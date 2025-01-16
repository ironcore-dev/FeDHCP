// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type HttpBootConfig struct {
	BootFile       string `yaml:"bootFile"`
	ClientSpecific bool   `yaml:"clientSpecific"`
}
