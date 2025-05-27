// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type IPAMConfig struct {
	Namespace string   `yaml:"namespace"`
	Subnets   []string `yaml:"subnets"`
}
