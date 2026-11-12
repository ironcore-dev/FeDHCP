// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package metrics

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestServe verifies that Serve brings up a listener that exposes the default
// Prometheus registry on /metrics.
func TestServe(t *testing.T) {
	// bind to an ephemeral port and hand the resolved address to Serve
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	Serve(addr)

	body := mustGet(t, "http://"+addr+"/metrics")

	// the default registry always exposes the Go collector; its presence
	// proves the endpoint is wired up and scrapeable
	if !strings.Contains(body, "go_goroutines") {
		t.Errorf("metrics output does not look like a Prometheus exposition:\n%s", body)
	}
}

// mustGet polls url until the (async) metrics server is ready and returns the
// response body, failing the test on timeout.
func mustGet(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec // test-only localhost URL
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d", url, resp.StatusCode)
			}
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics endpoint never became ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
