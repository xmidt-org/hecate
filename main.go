package main

import (
	"encoding/json"
	"fmt"
	"io"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/xmidt-org/argus/chrysom"
	"github.com/xmidt-org/argus/model"

	"github.com/go-kit/kit/log"
	"github.com/goph/emperror"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/prometheus/common/route"
	"github.com/xmidt-org/webpa-common/concurrent"
	"github.com/xmidt-org/webpa-common/logging"
	"github.com/xmidt-org/webpa-common/server"
	"github.com/xmidt-org/webpa-common/webhook"
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
	Argus chrysom.ClientConfig
}

func hecate(arguments []string) int {
	start := time.Now()

	var (
		f, v                                = pflag.NewFlagSet(applicationName, pflag.ContinueOnError), viper.New()
		logger, metricsRegistry, codex, err = server.Initialize(applicationName, arguments, f, v, webhook.Metrics)
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

	webhookFactory.Initialize(nil, nil, "", webhookHandler, logger, metricsRegistry, nil)

	hookStorage, err := chrysom.CreateClient(config.Argus, chrysom.WithLogger(logger))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating new argus store: %s\n", err)
		return 1
	}

	logging.Info(logger).Log(logging.MessageKey(), fmt.Sprintf("%s is up and running!", applicationName), "elapsedTime", time.Since(start))

	// The actual logic

	_, runnable, done := codex.Prepare(logger, nil, metricsRegistry, route.New())

	waitGroup, shutdown, err := concurrent.Execute(runnable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to start device manager: %s\n", err)
		return 1
	}
	waitGroup.Add(1)
	go func() {
		for change := range webhookRegistry.Changes {
			for _, hook := range change {
				webhook := map[string]interface{}{}
				data, err := json.Marshal(&hook)
				if err != nil {
					continue
				}
				err = json.Unmarshal(data, &webhook)
				if err != nil {
					continue
				}
				hookStorage.Push(model.Item{
					Identifier: hook.ID(),
					Data:       webhook,
					TTL:        config.Argus.DefaultTTL,
				}, "")
			}
		}
		waitGroup.Done()
	}()

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

func main() {
	os.Exit(hecate(os.Args))
}
