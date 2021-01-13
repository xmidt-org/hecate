package main

import (
	"github.com/gorilla/mux"
	"github.com/xmidt-org/themis/xhealth"
	"github.com/xmidt-org/themis/xhttp/xhttpserver/pprof"
	"github.com/xmidt-org/themis/xmetrics/xmetricshttp"
	"go.uber.org/fx"
)

type metricsRoutesIn struct {
	fx.In
	Router  *mux.Router `name:"servers.metrics"`
	Handler xmetricshttp.Handler
}

func buildMetricsRoutes(in metricsRoutesIn) {
	if in.Router != nil && in.Handler != nil {
		in.Router.Handle("/metrics", in.Handler).Methods("GET")
	}
}

type healthRoutesIn struct {
	fx.In
	Router  *mux.Router `name:"servers.health"`
	Handler xhealth.Handler
}

func buildHealthRoutes(in healthRoutesIn) {
	if in.Router != nil && in.Handler != nil {
		in.Router.Handle("/health", in.Handler).Methods("GET")
	}
}

type pprofRoutesIn struct {
	fx.In
	Router *mux.Router `name:"servers.pprof"`
}

func buildPprofRoutes(in pprofRoutesIn) {
	if in.Router != nil {
		pprof.BuildRoutes(in.Router)
	}
}
