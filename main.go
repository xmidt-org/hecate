package main

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"runtime"
	"time"

	"github.com/xmidt-org/ancla"
	"github.com/xmidt-org/arrange"

	"github.com/InVisionApp/go-health"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xmidt-org/argus/chrysom"
	"github.com/xmidt-org/themis/config"
	"github.com/xmidt-org/themis/xhealth"
	"github.com/xmidt-org/themis/xhttp/xhttpserver"
	"github.com/xmidt-org/themis/xlog"
	"github.com/xmidt-org/themis/xlog/xloghttp"
	"github.com/xmidt-org/themis/xmetrics"
	"github.com/xmidt-org/themis/xmetrics/xmetricshttp"
	"go.uber.org/fx"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/xmidt-org/webpa-common/logging"
)

const applicationName = "hecate"

// Variables with values provided at build time through ldflags (see Makefile for details)
var (
	GitCommit = "undefined"
	Version   = "undefined"
	BuildTime = "undefined"
)

type primaryRouter struct {
	fx.In
	Router *mux.Router `name:"servers.primary"`
}

type DownstreamIn struct {
	fx.In
	Logger log.Logger
	Config chrysom.BasicClientConfig `name:"downstream_argus_config"`
}

type UpstreamIn struct {
	fx.In
	LC              fx.Lifecycle
	GetLogger       func(context.Context) log.Logger
	Logger          log.Logger
	Config          ancla.Config `name:"upstream_argus_config"`
	DownstreamWatch ancla.WatchFunc
	Registry        xmetrics.Registry
}

type transitionConfig struct {
	Owner string
}

func setupFlagSet(fs *pflag.FlagSet) error {
	fs.StringP("file", "f", "", "the configuration file to use.  Overrides the search path.")
	fs.BoolP("debug", "d", false, "enables debug logging.  Overrides configuration.")
	fs.BoolP("version", "v", false, "print version and exit")

	return nil
}

func setupViper(v *viper.Viper, fs *pflag.FlagSet, name string) (err error) {
	if printVersion, _ := fs.GetBool("version"); printVersion {
		printVersionInfo()
	}

	if file, _ := fs.GetString("file"); len(file) > 0 {
		v.SetConfigFile(file)
		err = v.ReadInConfig()
	} else {
		v.SetConfigName(name)
		v.AddConfigPath(fmt.Sprintf("/etc/%s", name))
		v.AddConfigPath(fmt.Sprintf("$HOME/.%s", name))
		v.AddConfigPath(".")
		err = v.ReadInConfig()
	}

	if err != nil {
		return
	}

	if debug, _ := fs.GetBool("debug"); debug {
		v.Set("log.level", "DEBUG")
	}

	return nil
}

func main() {
	// setup command line options and configuration from file
	f := pflag.NewFlagSet(applicationName, pflag.ContinueOnError)
	setupFlagSet(f)
	v := viper.New()
	err := setupViper(v, f, applicationName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	app := fx.New(
		xlog.Logger(),
		fx.Supply(v),
		arrange.ForViper(v),
		fx.Provide(
			func() func(context.Context) log.Logger {
				return func(ctx context.Context) log.Logger {
					logger := log.With(logging.GetLogger(ctx), "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)
					return logger
				}
			},
			provideUnmarshaller,
			xlog.Unmarshal("log"),
			xloghttp.ProvideStandardBuilders,
			xmetricshttp.Unmarshal("prometheus", promhttp.HandlerOpts{}),
			xhealth.Unmarshal("health"),
			arrange.UnmarshalKey("downstream", transitionConfig{}),
			fx.Annotated{
				Name:   "downstream_argus_config",
				Target: arrange.UnmarshalKey("downstream.argus", chrysom.BasicClientConfig{}),
			},
			// build downstream client
			func(in DownstreamIn) (*chrysom.BasicClient, error) {
				if in.Config.Bucket == "" {
					in.Config.Bucket = "webhooks"
				}
				in.Config.Logger = in.Logger
				level.Info(in.Logger).Log(logging.MessageKey(), fmt.Sprintf("argus address: %s", in.Config.Address))
				return chrysom.NewBasicClient(in.Config, nil)
			},
			createArgusSynchronizer,
			fx.Annotated{
				Name:   "upstream_argus_config",
				Target: arrange.UnmarshalKey("upstream", ancla.Config{}),
			},
			xhttpserver.Unmarshal{Key: "servers.primary", Optional: true}.Annotated(),
			xhttpserver.Unmarshal{Key: "servers.metrics", Optional: true}.Annotated(),
			xhttpserver.Unmarshal{Key: "servers.health", Optional: true}.Annotated(),
			xhttpserver.Unmarshal{Key: "servers.pprof", Optional: true}.Annotated(),
		),
		fx.Invoke(
			xhealth.ApplyChecks(
				&health.Config{
					Name:     applicationName,
					Interval: 24 * time.Hour,
					Checker: xhealth.NopCheckable{
						Details: map[string]interface{}{
							"StartTime": time.Now().UTC().Format(time.RFC3339),
						},
					},
				},
			),
			buildHealthRoutes,
			buildMetricsRoutes,
			buildPprofRoutes,
			func(in UpstreamIn) error {
				in.Config.Logger = in.Logger

				// TODO: move to touchstone and get rid of all of this special stuff.
				oldMetrics := ancla.Metrics()
				gauge, err := in.Registry.NewGauge(prometheus.GaugeOpts{
					Name: oldMetrics[0].Name,
					Help: oldMetrics[0].Help,
				}, oldMetrics[0].LabelNames)
				if err != nil {
					return fmt.Errorf("failed to create ancla metric '%s': %v", oldMetrics[0].Name, err)
				}
				counter, err := in.Registry.NewCounterVec(prometheus.CounterOpts{
					Name: oldMetrics[1].Name,
					Help: oldMetrics[1].Help,
				}, oldMetrics[1].LabelNames)
				if err != nil {
					return fmt.Errorf("failed to create ancla metric '%s': %v", oldMetrics[1].Name, err)
				}
				in.Config.Measures = ancla.Measures{
					WebhookListSizeGauge:     gauge,
					ChrysomPollsTotalCounter: counter,
				}

				_, stopWatches, err := ancla.Initialize(in.Config, in.GetLogger, logging.WithLogger, in.DownstreamWatch)
				if err != nil {
					return fmt.Errorf("failed to initialize upstream argus client: %v", err)
				}

				in.LC.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						return nil
					},
					OnStop: func(ctx context.Context) error {
						// TODO: currently, uber fx cannot restart the
						// application. once ancla is stopped, it cannot be
						// started again.
						stopWatches()
						return nil
					},
				})
				return nil
			},
		),
	)

	switch err := app.Err(); err {
	case pflag.ErrHelp:
		return
	case nil:
		app.Run()
	default:
		fmt.Println(err)
		os.Exit(2)
	}
}

func createArgusSynchronizer(client *chrysom.BasicClient, config transitionConfig, logger log.Logger) ancla.WatchFunc {
	return func(webhooks []ancla.InternalWebhook) {
		for _, w := range webhooks {
			logging.Info(logger).Log("msg", "Pushing webhook update from SNS into Argus")

			item, err := ancla.InternalWebhookToItem(time.Now, w)
			if err != nil {
				logging.Error(logger).Log(logging.MessageKey(), "failed to convert webhook to item", "err", err)
				continue
			}

			result, err := client.PushItem(context.Background(), config.Owner, item)
			if err != nil {
				logging.Error(logger).Log(logging.MessageKey(), "failed to push item to Argus", "err", err)
				continue
			}

			if result != chrysom.CreatedPushResult && result != chrysom.UpdatedPushResult {
				logging.Error(logger).Log(logging.MessageKey(), "Unsuccessful item push response from Argus", "err", err)
			}
			logging.Debug(logger).Log(logging.MessageKey(), "Successfully pushed an webhook item from SNS to Argus")
		}
	}
}

func printVersionInfo() {
	fmt.Fprintf(os.Stdout, "%s:\n", applicationName)
	fmt.Fprintf(os.Stdout, "  version: \t%s\n", Version)
	fmt.Fprintf(os.Stdout, "  go version: \t%s\n", runtime.Version())
	fmt.Fprintf(os.Stdout, "  built time: \t%s\n", BuildTime)
	fmt.Fprintf(os.Stdout, "  git commit: \t%s\n", GitCommit)
	fmt.Fprintf(os.Stdout, "  os/arch: \t%s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// TODO: once we get rid of any packages that need an unmarshaller, remove this.
type unmarshallerOut struct {
	fx.Out
	Unmarshaller config.Unmarshaller
}

func provideUnmarshaller(v *viper.Viper) unmarshallerOut {
	return unmarshallerOut{
		Unmarshaller: config.ViperUnmarshaller{Viper: v, Options: []viper.DecoderConfigOption{}},
	}
}
