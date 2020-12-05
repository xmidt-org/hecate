package main

import (
	"encoding/json"
	"fmt"
	"io"
	_ "net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/goph/emperror"
	"github.com/gorilla/mux"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/xmidt-org/webpa-common/concurrent"
	"github.com/xmidt-org/webpa-common/logging"
	"github.com/xmidt-org/webpa-common/server"
	"github.com/xmidt-org/webpa-common/webhook"
	"github.com/xmidt-org/webpa-common/webhook/aws"
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

func hecate(arguments []string) int {
	start := time.Now()

	var (
		f, v                                = pflag.NewFlagSet(applicationName, pflag.ContinueOnError), viper.New()
		logger, metricsRegistry, codex, err = server.Initialize(applicationName, arguments, f, v, webhook.Metrics, aws.Metrics, xwebhook.Metrics)
	)

	if parseErr, done := printVersion(f, arguments); done {
		// if we're done, we're exiting no matter what
		exitIfError(logger, emperror.Wrap(parseErr, "failed to parse arguments"))
		os.Exit(0)
	}

	// set everything up
	config := new(Config)
	err = v.Unmarshal(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating new webhook factory: %s\n", err)
		return 1
	}
	exitIfError(logger, emperror.Wrap(err, "unable to initialize viper"))

	webhookFactory, err := webhook.NewFactory(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating new webhook factory: %s\n", err)
		return 1
	}
	webhookRegistry, webhookHandler := webhookFactory.NewRegistryAndHandler(metricsRegistry)

	scheme := v.GetString("scheme")
	if len(scheme) < 1 {
		scheme = "https"
	}

	selfURL := &url.URL{
		Scheme: scheme,
		Host:   v.GetString("fqdn") + v.GetString("primary.address"),
	}

	rootRouter := mux.NewRouter()
	webhookFactory.Initialize(rootRouter, selfURL, v.GetString("soa.provider"), webhookHandler, logger, metricsRegistry, time.Now)

	config.Webhook.Argus.MetricsProvider = metricsRegistry
	config.Webhook.Argus.Logger = logger
	svc, stopWatches, err := xwebhook.Initialize(&config.Webhook)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing xwebhook %s\n", err)
		return 1
	}
	defer stopWatches()

	logging.Info(logger).Log(logging.MessageKey(), fmt.Sprintf("%s is up and running!", applicationName), "elapsedTime", time.Since(start))

	// The actual logic
	_, runnable, done := codex.Prepare(logger, nil, metricsRegistry, rootRouter)

	waitGroup, shutdown, err := concurrent.Execute(runnable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to start device manager: %s\n", err)
		return 1
	}
	webhookFactory.SetExternalUpdate(createArgusSynchronizer(svc, logger))

	// wait for DNS to propagate before subscribing to SNS
	if err = webhookFactory.DnsReady(); err == nil {
		logging.Info(logger).Log(logging.MessageKey(), "server is ready to take on subscription confirmations")
		webhookFactory.PrepareAndStart()
	} else {
		logging.Error(logger).Log(logging.MessageKey(), "Server was not ready within a time constraint. SNS confirmation could not happen",
			logging.ErrorKey(), err)
	}

	signals := make(chan os.Signal, 10)
	signal.Notify(signals)
	for exit := false; !exit; {
		select {
		case s := <-signals:
			if s != os.Kill && s != os.Interrupt {
			} else {
				logging.Error(logger).Log(logging.MessageKey(), "exiting due to signal", "signal", s)
				exit = true
			}
		case <-done:
			exit = true
		}
	}
	close(shutdown)
	close(webhookRegistry.Changes)
	waitGroup.Wait()

	return 0
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

func printVersion(f *pflag.FlagSet, arguments []string) (error, bool) {
	printVer := f.BoolP("version", "v", false, "displays the version number")
	if err := f.Parse(arguments); err != nil {
		return err, true
	}

	if *printVer {
		printVersionInfo(os.Stdout)
		return nil, true
	}
	return nil, false
}

func printVersionInfo(writer io.Writer) {
	fmt.Fprintf(writer, "%s:\n", applicationName)
	fmt.Fprintf(writer, "  version: \t%s\n", Version)
	fmt.Fprintf(writer, "  go version: \t%s\n", runtime.Version())
	fmt.Fprintf(writer, "  built time: \t%s\n", BuildTime)
	fmt.Fprintf(writer, "  git commit: \t%s\n", GitCommit)
	fmt.Fprintf(writer, "  os/arch: \t%s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func exitIfError(logger log.Logger, err error) {
	if err != nil {
		if logger != nil {
			logging.Error(logger, emperror.Context(err)...).Log(logging.ErrorKey(), err.Error())
		}
		fmt.Fprintf(os.Stderr, "Error: %#v\n", err.Error())
		os.Exit(1)
	}
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

func main() {
	os.Exit(hecate(os.Args))
}
