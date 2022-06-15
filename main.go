package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/xmidt-org/ancla"
	"github.com/xmidt-org/arrange"

	"github.com/InVisionApp/go-health"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xmidt-org/argus/chrysom"
	"github.com/xmidt-org/themis/config"
	"github.com/xmidt-org/themis/xhealth"
	"github.com/xmidt-org/themis/xhttp/xhttpserver"
	"github.com/xmidt-org/themis/xlog"
	"github.com/xmidt-org/themis/xlog/xloghttp"
	"github.com/xmidt-org/themis/xmetrics/xmetricshttp"
	"github.com/xmidt-org/webpa-common/webhook/aws"
	"github.com/xmidt-org/webpa-common/xmetrics"
	"go.uber.org/fx"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/xmidt-org/webpa-common/logging"
	"github.com/xmidt-org/webpa-common/webhook"
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

// config.Logger = logger
// level.Info(logger).Log(logging.MessageKey(), fmt.Sprintf("argus address: %s", config.Address))
// return config, err

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
		webhook.ProvideMetrics(),
		aws.ProvideMetrics(),
		arrange.ForViper(v),
		fx.Provide(
			provideUnmarshaller,
			xlog.Unmarshal("log"),
			xloghttp.ProvideStandardBuilders,
			xmetrics.NewRegistry,
			xmetricshttp.Unmarshal("prometheus", promhttp.HandlerOpts{}),
			xhealth.Unmarshal("health"),
			arrange.UnmarshalKey("downstream", transitionConfig{}),
			fx.Annotated{
				Name:   "upstream_argus_config",
				Target: arrange.UnmarshalKey("upstream.argus", chrysom.BasicClientConfig{}),
			},
			fx.Annotated{
				Name:   "downstream_argus_config",
				Target: arrange.UnmarshalKey("downstream.argus", chrysom.BasicClientConfig{}),
			},
			// TODO: add setup for ancla client, only set up aws webhooks if the upstream argus isn't set.
			webhook.NewFactory,
			func(lc fx.Lifecycle, factory *webhook.Factory, metrics webhook.WebhookMetrics) http.Handler {
				_, webhookHandler := factory.NewRegistryAndHandler(metrics)
				return webhookHandler
			},
			fx.Annotated{
				Name: "downstream_client",
				Target: func(in DownstreamIn) (*chrysom.BasicClient, error) {
					if in.Config.Bucket == "" {
						in.Config.Bucket = "webhooks"
					}
					in.Config.Logger = in.Logger
					level.Info(in.Logger).Log(logging.MessageKey(), fmt.Sprintf("argus address: %s", in.Config.Address))
					return chrysom.NewBasicClient(in.Config, nil)
				},
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
			func(lc fx.Lifecycle, factory *webhook.Factory, webhookHandler http.Handler, primaryRouter primaryRouter, v *viper.Viper, logger log.Logger, awsMetrics aws.AWSMetrics) {
				var scheme = "https"
				if v.GetBool("disableSnsTls") {
					scheme = "http"
				}

				selfURL := &url.URL{
					Scheme: scheme,
					Host:   v.GetString("fqdn") + v.GetString("servers.primary.address"),
				}

				factory.Initialize(primaryRouter.Router, selfURL, v.GetString("soa.provider"), webhookHandler, logger, awsMetrics, time.Now)
				logging.Info(logger).Log(logging.MessageKey(), fmt.Sprintf("%s is up and running!", applicationName))
			},
			startWebhookFactory,
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

type WebhookStartIn struct {
	fx.In
	LC              fx.Lifecycle
	WebhookFactory  *webhook.Factory
	Client          *chrysom.BasicClient `name:"downstream_client"`
	MigrationConfig transitionConfig
	Logger          log.Logger
}

func startWebhookFactory(in WebhookStartIn) {
	in.WebhookFactory.SetExternalUpdate(createArgusSynchronizer(in.Client, in.MigrationConfig, in.Logger))
	in.LC.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if err := in.WebhookFactory.DnsReady(); err != nil {
				logging.Error(in.Logger).Log(logging.MessageKey(), "Server was not ready within a time constraint. SNS confirmation could not happen",
					logging.ErrorKey(), err)
				return err
			}
			logging.Info(in.Logger).Log(logging.MessageKey(), "server is ready to take on subscription confirmations")
			in.WebhookFactory.PrepareAndStart()
			return nil
		},
	})
}

func createArgusSynchronizer(client *chrysom.BasicClient, config transitionConfig, logger log.Logger) func([]webhook.W) {
	return func(webhooks []webhook.W) {
		for _, w := range webhooks {
			logging.Info(logger).Log("msg", "Pushing webhook update from SNS into Argus")

			internalW := ancla.InternalWebhook{
				PartnerIDs: []string{"comcast"},
				Webhook: ancla.Webhook{
					Address:    w.Address,
					Config:     w.Config,
					FailureURL: w.FailureURL,
					Events:     w.Events,
					Matcher: ancla.MetadataMatcherConfig{
						DeviceID: w.Matcher.DeviceId,
					},
					Duration: w.Duration,
					Until:    w.Until,
				},
			}
			item, err := ancla.InternalWebhookToItem(time.Now, internalW)
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
