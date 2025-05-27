// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type MACFilterConfig struct {
	AllowList []string `yaml:"allowList"`
	DenyList  []string `yaml:"denyList"`
}
