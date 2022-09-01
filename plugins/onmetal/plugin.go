// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package onmetal

import (
	"net"
	"time"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/onmetal")

var Plugin = plugins.Plugin{
	Name:   "onmetal",
	Setup6: setup6,
}

func setup6(args ...string) (handler.Handler6, error) {
	log.Printf("loaded plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	//log.Printf("received DHCPv6 packet: %s", req.Summary())

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	ipaddr := make(net.IP, len(relayMsg.LinkAddr))
	copy(ipaddr, relayMsg.LinkAddr)
	ipaddr[len(ipaddr)-1] += 1

	//	ipaddr := net.ParseIP("2a10:afc0:e013:1004::ffff:5:1")

	log.Infof("generated IP address %s", ipaddr)

	m, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("BUG: could not decapsulate: %v", err)
		return nil, true
	}

	if m.Options.OneIANA() == nil {
		log.Debug("No address requested")
		return resp, false
	}

	resp.AddOption(&dhcpv6.OptIANA{
		IaId: m.Options.OneIANA().IaId,
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          ipaddr,
				PreferredLifetime: 30 * time.Second,
				ValidLifetime:     30 * time.Second,
			},
		}},
	})

	//log.Printf("responding with this packet:\n%s", resp.Summary())

	return resp, false
}
