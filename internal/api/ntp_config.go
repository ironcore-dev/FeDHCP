// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

import "net"

type NTPConfig struct {
	Servers   []net.IP `yaml:"servers"`
	ServersV6 []net.IP `yaml:"servers_v6"`
}
