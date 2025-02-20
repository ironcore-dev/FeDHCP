// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type MACFilterConfig struct {
	WhiteList []string `yaml:"whiteList"`
	BlackList []string `yaml:"blackList"`
}
