// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

// Package metrics exposes FeDHCP runtime metrics in Prometheus format.
//
// It serves the default Prometheus registry on a dedicated HTTP listener (see
// Serve). Plugins register their own collectors on the default registry.
package metrics

import (
	"net/http"
	"time"

	"github.com/coredhcp/coredhcp/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var log = logger.GetLogger("metrics")

// Namespace is the common Prometheus namespace for all FeDHCP metrics.
const Namespace = "fedhcp"

// Serve starts an HTTP listener that exposes /metrics in Prometheus format.
// It runs in its own goroutine and logs (but does not crash on) listener
// errors, so a broken metrics endpoint never takes down the DHCP server.
func Serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Infof("Serving metrics on %s/metrics", addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("Metrics server stopped: %v", err)
		}
	}()
}
