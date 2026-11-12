// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"fmt"
	"math"
	"net"
	"sync"

	"github.com/ironcore-dev/fedhcp/internal/metrics"
)

const (
	// pluginName labels all metrics emitted by this plugin.
	pluginName = "stateless"

	// hostBits is the size of the host region of a /80 relay prefix
	// (bytes 10-15 = 48 bits). It determines how many addresses a pool can
	// theoretically hand out.
	hostBits = (net.IPv6len - macOffset) * 8
)

// poolTracker records, per relay /80 prefix, which addresses have been handed
// out and how often. It keeps the Prometheus gauges in sync. The stateless
// plugin never persists leases, so "leased" here means "observed since the
// process started".
type poolTracker struct {
	mu    sync.Mutex
	pools map[string]map[string]float64 // prefix -> ip -> lease count
}

var tracker = &poolTracker{
	pools: make(map[string]map[string]float64),
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

// record accounts for a single address assignment and updates the metrics.
func (t *poolTracker) record(linkAddr, addr net.IP) {
	prefix := prefixString(linkAddr)
	ip := addr.String()

	t.mu.Lock()
	defer t.mu.Unlock()

	ips, ok := t.pools[prefix]
	if !ok {
		ips = make(map[string]float64)
		t.pools[prefix] = ips
		// A /80 prefix has 2^48 assignable host addresses.
		metrics.PoolIPsTotal.WithLabelValues(pluginName, prefix).Set(math.Pow(2, hostBits))
	}

	ips[ip]++

	metrics.PoolIPsLeased.WithLabelValues(pluginName, prefix).Set(float64(len(ips)))
	metrics.LeasesPerIP.WithLabelValues(pluginName, prefix, ip).Set(ips[ip])
}
