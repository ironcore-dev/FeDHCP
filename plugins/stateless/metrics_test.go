// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrefixString(t *testing.T) {
	got := prefixString(net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff"))
	want := "2001:db8:1111:2222:3333::/80"
	if got != want {
		t.Errorf("got=%s want=%s", got, want)
	}
}

func TestPoolTrackerActiveLeases(t *testing.T) {
	now := time.Unix(0, 0)
	tr := newPoolTracker()
	tr.now = func() time.Time { return now }

	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")
	ipA := net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff")
	ipB := net.ParseIP("2001:db8:1111:2222:3333:1122:3344:5566")

	// two distinct active leases; re-requesting ipA must only refresh it
	tr.record(linkAddr, ipA, time.Hour)
	tr.record(linkAddr, ipB, time.Hour)
	tr.record(linkAddr, ipA, time.Hour)

	expect := func(leased int) {
		t.Helper()
		want := `
# HELP fedhcp_pool_ips_leased Number of IP addresses with an active (unexpired) lease in a pool.
# TYPE fedhcp_pool_ips_leased gauge
fedhcp_pool_ips_leased{plugin="stateless",prefix="2001:db8:1111:2222:3333::/80"} ` + strconv.Itoa(leased) + "\n"
		if leased == 0 {
			want = "" // emptied pool emits no series
		}
		if err := testutil.CollectAndCompare(tr, strings.NewReader(want), "fedhcp_pool_ips_leased"); err != nil {
			t.Error(err)
		}
	}

	expect(2)

	// advance past ipB's expiry but keep ipA alive by refreshing it
	now = now.Add(90 * time.Minute)
	tr.record(linkAddr, ipA, time.Hour)
	expect(1)

	// advance past ipA's refreshed expiry: pool empties and disappears
	now = now.Add(2 * time.Hour)
	expect(0)
}

func TestPoolCounters(t *testing.T) {
	now := time.Unix(0, 0)
	tr := newPoolTracker()
	tr.now = func() time.Time { return now }

	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")
	ipA := net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff")
	ipB := net.ParseIP("2001:db8:1111:2222:3333:1122:3344:5566")

	tr.record(linkAddr, ipA, time.Hour) // new
	tr.record(linkAddr, ipB, time.Hour) // new
	tr.record(linkAddr, ipA, time.Hour) // renewal (still active)

	// let ipA's lease lapse, then re-request: counts as a new lease, not a renewal
	now = now.Add(2 * time.Hour)
	tr.record(linkAddr, ipA, time.Hour) // new (was expired)

	wantLeases := `
# HELP fedhcp_pool_leases_total Total number of lease assignments handed out from a pool (new leases plus renewals).
# TYPE fedhcp_pool_leases_total counter
fedhcp_pool_leases_total{plugin="stateless",prefix="2001:db8:1111:2222:3333::/80"} 4
`
	if err := testutil.CollectAndCompare(tr, strings.NewReader(wantLeases), "fedhcp_pool_leases_total"); err != nil {
		t.Error(err)
	}

	wantRenewals := `
# HELP fedhcp_pool_renewals_total Total number of lease renewals (re-requests of an already active address) in a pool.
# TYPE fedhcp_pool_renewals_total counter
fedhcp_pool_renewals_total{plugin="stateless",prefix="2001:db8:1111:2222:3333::/80"} 1
`
	if err := testutil.CollectAndCompare(tr, strings.NewReader(wantRenewals), "fedhcp_pool_renewals_total"); err != nil {
		t.Error(err)
	}
}
