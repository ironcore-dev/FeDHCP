// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type SubnetLabel struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type OOBConfig struct {
	Namespace    string        `yaml:"namespace"`
	SubnetLabels []SubnetLabel `yaml:"subnetLabels"`
}
