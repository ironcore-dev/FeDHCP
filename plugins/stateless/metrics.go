// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// pluginName labels all metrics emitted by this plugin.
const pluginName = "stateless"

var (
	poolIPsLeasedDesc = prometheus.NewDesc(
		metrics.Namespace+"_pool_ips_leased",
		"Number of IP addresses with an active (unexpired) lease in a pool.",
		[]string{"plugin", "prefix"}, nil,
	)
	leasesTotalDesc = prometheus.NewDesc(
		metrics.Namespace+"_leases_total",
		"Total number of lease assignments handed out (new leases plus renewals).",
		[]string{"plugin"}, nil,
	)
	renewalsTotalDesc = prometheus.NewDesc(
		metrics.Namespace+"_renewals_total",
		"Total number of lease renewals (re-requests of an already active address).",
		[]string{"plugin"}, nil,
	)
)

// poolTracker records, per relay /80 prefix, the active leases and their
// expiry, plus instance-wide assignment and renewal counts. It implements
// prometheus.Collector: expired leases are pruned at scrape time, so the
// pool gauge reflects only currently active leases and active-lease state
// stays bounded by the number of active leases.
//
// Per-prefix counts are kept to the ips_leased gauge only. The leases/renewals
// counters are aggregated per instance (bounded cardinality, one series each)
// instead of per prefix, since topologies with one relay prefix per switch
// port would otherwise create an unbounded number of counter series. They are
// cumulative for the process lifetime.
//
// The DHCP response itself remains a pure function of the request; this
// bookkeeping exists solely for observability.
type poolTracker struct {
	mu       sync.Mutex
	pools    map[string]map[string]time.Time // prefix -> ip -> lease expiry
	leases   float64                         // cumulative assignments, all pools
	renewals float64                         // cumulative renewals, all pools
	now      func() time.Time
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
// still active counts as a renewal and refreshes its expiry rather than adding
// another active lease; re-requesting after expiry counts as a new lease.
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

	if expiry, active := ips[ip]; active && expiry.After(t.now()) {
		t.renewals++
	}
	t.leases++
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
	ch <- poolIPsLeasedDesc
	ch <- leasesTotalDesc
	ch <- renewalsTotalDesc
}

// Collect implements prometheus.Collector. It prunes expired leases first so
// the pool gauge reflects only active leases. The leases/renewals counters are
// instance-wide and monotonic.
func (t *poolTracker) Collect(ch chan<- prometheus.Metric) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pruneLocked()

	for prefix, ips := range t.pools {
		ch <- prometheus.MustNewConstMetric(poolIPsLeasedDesc, prometheus.GaugeValue, float64(len(ips)), pluginName, prefix)
	}
	ch <- prometheus.MustNewConstMetric(leasesTotalDesc, prometheus.CounterValue, t.leases, pluginName)
	ch <- prometheus.MustNewConstMetric(renewalsTotalDesc, prometheus.CounterValue, t.renewals, pluginName)
}
