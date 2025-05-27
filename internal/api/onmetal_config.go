// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type PrefixDelegation struct {
	Length int `yaml:"length"`
}

type OnMetalConfig struct {
	PrefixDelegation PrefixDelegation `yaml:"prefixDelegation"`
}
