// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

// Package metrics exposes FeDHCP runtime metrics in Prometheus format.
//
// The metrics are registered on the default Prometheus registry and served on
// a dedicated HTTP listener (see Serve). Plugins update the metrics via the
// exported collectors below.
package metrics

import (
	"net/http"
	"time"

	"github.com/coredhcp/coredhcp/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var log = logger.GetLogger("metrics")

const namespace = "fedhcp"

var (
	// PoolIPsTotal is the total number of IP addresses a pool can hand out.
	// For the stateless plugin a pool is a relay /80 prefix and the total is
	// the theoretical size of its 48-bit host region.
	PoolIPsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "pool",
		Name:      "ips_total",
		Help:      "Total number of IP addresses available in a pool.",
	}, []string{"plugin", "prefix"})

	// PoolIPsLeased is the number of distinct IP addresses currently handed
	// out from a pool.
	PoolIPsLeased = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "pool",
		Name:      "ips_leased",
		Help:      "Number of distinct IP addresses currently leased from a pool.",
	}, []string{"plugin", "prefix"})

	// LeasesPerIP is the number of leases that have been handed out for a
	// single IP address.
	//
	// Note: the ip label is high cardinality. See the metrics task for the
	// discussion on bucketing / aggregating this in a follow-up.
	LeasesPerIP = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "pool",
		Name:      "leases_per_ip",
		Help:      "Number of leases handed out per IP address.",
	}, []string{"plugin", "prefix", "ip"})
)

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
