// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// pluginName labels all metrics emitted by this plugin.
	pluginName = "stateless"

	// hostBits is the size of the host region of a /80 relay prefix
	// (bytes 10-15 = 48 bits). It determines how many addresses a pool can
	// theoretically hand out.
	hostBits = (net.IPv6len - macOffset) * 8
)

var (
	poolIPsTotalDesc = prometheus.NewDesc(
		metrics.Namespace+"_pool_ips_total",
		"Total number of IP addresses available in a pool.",
		[]string{"plugin", "prefix"}, nil,
	)
	poolIPsLeasedDesc = prometheus.NewDesc(
		metrics.Namespace+"_pool_ips_leased",
		"Number of IP addresses with an active (unexpired) lease in a pool.",
		[]string{"plugin", "prefix"}, nil,
	)
)

// poolTracker records, per relay /80 prefix, the active leases and their
// expiry. It implements prometheus.Collector: expired leases are pruned at
// scrape time, so the gauges reflect only currently active leases and
// active-lease state stays bounded by the number of active leases.
//
// The DHCP response itself remains a pure function of the request; this
// bookkeeping exists solely for observability.
type poolTracker struct {
	mu    sync.Mutex
	pools map[string]map[string]time.Time // prefix -> ip -> lease expiry
	now   func() time.Time
}

var tracker = newPoolTracker()

func newPoolTracker() *poolTracker {
	return &poolTracker{
		pools: make(map[string]map[string]time.Time),
		now:   time.Now,
	}
}

func init() {
	prometheus.MustRegister(tracker)
}

// prefixString renders the /80 pool identifier of a relay LinkAddr.
func prefixString(linkAddr net.IP) string {
	p := make(net.IP, net.IPv6len)
	copy(p, linkAddr.To16())
	for i := macOffset; i < net.IPv6len; i++ {
		p[i] = 0
	}
	return fmt.Sprintf("%s/%d", p.String(), macOffset*8)
}

// record registers (or refreshes) a lease for addr out of linkAddr's pool,
// expiring validLifetime from now. Re-requesting an address whose lease is
// still active refreshes its expiry rather than adding another active lease.
func (t *poolTracker) record(linkAddr, addr net.IP, validLifetime time.Duration) {
	prefix := prefixString(linkAddr)
	ip := addr.String()

	t.mu.Lock()
	defer t.mu.Unlock()

	ips, ok := t.pools[prefix]
	if !ok {
		ips = make(map[string]time.Time)
		t.pools[prefix] = ips
	}
	ips[ip] = t.now().Add(validLifetime)
}

// pruneLocked removes expired leases and drops emptied pools. Callers must hold
// t.mu.
func (t *poolTracker) pruneLocked() {
	now := t.now()
	for prefix, ips := range t.pools {
		for ip, expiry := range ips {
			if !expiry.After(now) {
				delete(ips, ip)
			}
		}
		if len(ips) == 0 {
			delete(t.pools, prefix)
		}
	}
}

// Describe implements prometheus.Collector.
func (t *poolTracker) Describe(ch chan<- *prometheus.Desc) {
	ch <- poolIPsTotalDesc
	ch <- poolIPsLeasedDesc
}

// Collect implements prometheus.Collector. It prunes expired leases first so
// the reported gauge values reflect only active leases.
func (t *poolTracker) Collect(ch chan<- prometheus.Metric) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pruneLocked()

	// A /80 prefix has 2^48 assignable host addresses.
	total := math.Pow(2, hostBits)
	for prefix, ips := range t.pools {
		ch <- prometheus.MustNewConstMetric(poolIPsTotalDesc, prometheus.GaugeValue, total, pluginName, prefix)
		ch <- prometheus.MustNewConstMetric(poolIPsLeasedDesc, prometheus.GaugeValue, float64(len(ips)), pluginName, prefix)
	}
}
