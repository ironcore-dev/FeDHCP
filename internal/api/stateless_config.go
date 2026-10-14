// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

// StatelessConfig holds the configuration for the stateless plugin.
type StatelessConfig struct {
	LeaseTimes LeaseTimes `yaml:"leaseTimes"`
}
