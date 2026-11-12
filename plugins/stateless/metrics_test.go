// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"net"
	"testing"

	"github.com/ironcore-dev/fedhcp/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrefixString(t *testing.T) {
	got := prefixString(net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff"))
	want := "2001:db8:1111:2222:3333::/80"
	if got != want {
		t.Errorf("got=%s want=%s", got, want)
	}
}

func TestPoolTrackerRecord(t *testing.T) {
	// isolated tracker and reset metrics so the test is independent of order
	tr := &poolTracker{pools: make(map[string]map[string]float64)}
	metrics.PoolIPsTotal.Reset()
	metrics.PoolIPsLeased.Reset()
	metrics.LeasesPerIP.Reset()

	prefix := "2001:db8:1111:2222:3333::/80"
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")
	ipA := net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff")
	ipB := net.ParseIP("2001:db8:1111:2222:3333:1122:3344:5566")

	// two distinct IPs, one of them leased twice
	tr.record(linkAddr, ipA)
	tr.record(linkAddr, ipB)
	tr.record(linkAddr, ipA)

	if got := testutil.ToFloat64(metrics.PoolIPsTotal.WithLabelValues(pluginName, prefix)); got != 281474976710656 {
		t.Errorf("pool total: got=%v want=2^48", got)
	}
	if got := testutil.ToFloat64(metrics.PoolIPsLeased.WithLabelValues(pluginName, prefix)); got != 2 {
		t.Errorf("pool leased: got=%v want=2", got)
	}
	if got := testutil.ToFloat64(metrics.LeasesPerIP.WithLabelValues(pluginName, prefix, ipA.String())); got != 2 {
		t.Errorf("leases per ip A: got=%v want=2", got)
	}
	if got := testutil.ToFloat64(metrics.LeasesPerIP.WithLabelValues(pluginName, prefix, ipB.String())); got != 1 {
		t.Errorf("leases per ip B: got=%v want=1", got)
	}
}
