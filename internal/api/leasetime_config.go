// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"time"
)

// DefaultLeaseTime is the default preferred and valid lifetime for DHCP leases.
const DefaultLeaseTime = 24 * time.Hour

// LeaseTimes holds the DHCPv6 preferred and valid lifetimes. Both are optional;
// when unset they default to DefaultLeaseTime (24h).
type LeaseTimes struct {
	// PreferredLifetime is the time.Duration an address remains preferred.
	// +optional
	PreferredLifetime time.Duration `yaml:"preferredLifetime"`
	// ValidLifetime is the time.Duration an address remains valid.
	// +optional
	ValidLifetime time.Duration `yaml:"validLifetime"`
}

// Resolve returns the preferred and valid lifetimes, defaulting any zero value
// to DefaultLeaseTime.
func (l LeaseTimes) Resolve() (preferred, valid time.Duration) {
	preferred = l.PreferredLifetime
	if preferred == 0 {
		preferred = DefaultLeaseTime
	}
	valid = l.ValidLifetime
	if valid == 0 {
		valid = DefaultLeaseTime
	}
	return preferred, valid
}

// Validate checks the resolved lifetimes. Both must be positive, and per
// RFC 8415 the preferred lifetime must not exceed the valid lifetime.
func (l LeaseTimes) Validate() error {
	preferred, valid := l.Resolve()
	switch {
	case preferred <= 0:
		return fmt.Errorf("preferredLifetime must be positive, got %s", preferred)
	case valid <= 0:
		return fmt.Errorf("validLifetime must be positive, got %s", valid)
	case preferred > valid:
		return fmt.Errorf("preferredLifetime (%s) must not exceed validLifetime (%s)", preferred, valid)
	}
	return nil
}
