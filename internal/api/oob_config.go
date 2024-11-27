// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type OOBConfig struct {
	Namespace   string `yaml:"namespace"`
	SubnetLabel string `yaml:"subnetLabel"`
}
