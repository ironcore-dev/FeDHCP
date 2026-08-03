// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type BluefieldConfig struct {
	BluefieldIP string     `yaml:"bluefieldIP"`
	LeaseTimes  LeaseTimes `yaml:"leaseTimes"`
}
