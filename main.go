package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/InVisionApp/go-health"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	"github.com/go-kit/kit/metrics/provider"
	"github.com/gorilla/mux"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/xmidt-org/webpa-common/logging"
	"github.com/xmidt-org/webpa-common/webhook"
	"github.com/xmidt-org/webpa-common/xwebhook"
)

const (
	applicationName, apiBase = "hecate", "/api/v1"
	DEFAULT_KEY_ID           = "current"
)

var (
	GitCommit = "undefined"
	Version   = "undefined"
	BuildTime = "undefined"
)

type Config struct {
	Webhook xwebhook.Config
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
		webhook.ProvideMetrics(),
		aws.ProvideMetrics(),
		fx.Provide(
			ProvideUnmarshaller,
			xlog.Unmarshal("log"),
			xloghttp.ProvideStandardBuilders,
			xmetrics.NewRegistry,
			xmetricshttp.Unmarshal("prometheus", promhttp.HandlerOpts{}),
			xhealth.Unmarshal("health"),
			func(v *viper.Viper, logger log.Logger) (*Config, error) {
				config := new(Config)
				err := v.Unmarshal(config)
				// TODO: What to do? This is a discard provider because we don't create providers in uber/fx style
				config.Webhook.Argus.MetricsProvider = provider.NewDiscardProvider()
				config.Webhook.Argus.Logger = logger
				return config, err
			},
			webhook.NewFactory,
			func(lc fx.Lifecycle, factory *webhook.Factory, metrics webhook.WebhookMetrics) http.Handler {
				webhookRegistry, webhookHandler := factory.NewRegistryAndHandler(&metrics)
				lc.Append(fx.Hook{
					OnStop: func(ctx context.Context) error {
						close(webhookRegistry.Changes)
						return nil
					},
				})

				return webhookHandler
			},
			func(lc fx.Lifecycle, config *Config) (xwebhook.Service, error) {
				svc, stopWatches, err := xwebhook.Initialize(&config.Webhook)

				lc.Append(fx.Hook{
					OnStop: func(ctx context.Context) error {
						stopWatches()
						return nil
					},
				})

				return svc, err
			},
			xhttpserver.Unmarshal{Key: "primary", Optional: true}.Annotated(),
			xhttpserver.Unmarshal{Key: "metric", Optional: true}.Annotated(),
			xhttpserver.Unmarshal{Key: "health", Optional: true}.Annotated(),
			xhttpserver.Unmarshal{Key: "pprof", Optional: true}.Annotated(),
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
			func(factory *webhook.Factory, webhookHandler http.Handler, v *viper.Viper, logger log.Logger, awsMetrics aws.AWSMetrics) {
				scheme := v.GetString("scheme")
				if len(scheme) < 1 {
					scheme = "https"
				}

				selfURL := &url.URL{
					Scheme: scheme,
					Host:   v.GetString("fqdn") + v.GetString("primary.address"),
				}

				rootRouter := mux.NewRouter()
				factory.Initialize(rootRouter, selfURL, v.GetString("soa.provider"), webhookHandler, logger, &awsMetrics, time.Now)
			},
			func(webhookFactory *webhook.Factory, svc xwebhook.Service, logger log.Logger) {
				webhookFactory.SetExternalUpdate(createArgusSynchronizer(svc, logger))

				// wait for DNS to propagate before subscribing to SNS
				if err = webhookFactory.DnsReady(); err == nil {
					logging.Info(logger).Log(logging.MessageKey(), "server is ready to take on subscription confirmations")
					webhookFactory.PrepareAndStart()
				} else {
					logging.Error(logger).Log(logging.MessageKey(), "Server was not ready within a time constraint. SNS confirmation could not happen",
						logging.ErrorKey(), err)
				}
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

func createArgusSynchronizer(svc xwebhook.Service, logger log.Logger) func([]webhook.W) {
	return func(webhooks []webhook.W) {
		for _, w := range webhooks {
			logging.Info(logger).Log("msg", "Pushing webhook update from SNS into Argus")

			webhook, err := toNewWebhook(&w)
			if err != nil {
				logging.Error(logger).Log(logging.MessageKey(), "could not convert to new webhook struct", "err", err)
				continue
			}
			err = svc.Add("Argus", webhook)
			if err != nil {
				logging.Error(logger).Log(logging.MessageKey(), "could not add webhook to argus", "err", err)
			}

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

func toNewWebhook(w *webhook.W) (*xwebhook.Webhook, error) {
	data, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}
	xw := new(xwebhook.Webhook)
	err = json.Unmarshal(data, xw)
	if err != nil {
		return nil, err
	}
	return xw, nil
}

func ApplyMetricsData(awsMetrics aws.AWSMetrics, webhookMetrics webhook.WebhookMetrics) {
	webhookMetrics.ListSize.Add(0.0)
	webhookMetrics.NotificationUnmarshallFailed.Add(0.0)

	awsMetrics.DnsReadyQueryCount.Add(0.0)
	awsMetrics.DnsReady.Add(0.0)
	awsMetrics.SNSNotificationSent.Add(0.0)
	awsMetrics.SNSSubscribed.Add(0.0)
}

// TODO: once we get rid of any packages that need an unmarshaller, remove this.
type UnmarshallerOut struct {
	fx.Out
	Unmarshaller config.Unmarshaller
}

func ProvideUnmarshaller(v *viper.Viper) UnmarshallerOut {
	return UnmarshallerOut{
		Unmarshaller: config.ViperUnmarshaller{Viper: v, Options: []viper.DecoderConfigOption{}},
	}
}
