// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package onmetal

import (
	"fmt"
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

var mask80 = net.CIDRMask(prefixLength, 128)

const (
	preferredLifeTime = 24 * time.Hour
	validLifeTime     = 24 * time.Hour
	prefixLength      = 80
)

func setup6(args ...string) (handler.Handler6, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("no arguments expected, got %d", len(args))
	}
	log.Printf("loaded onmetal plugin for DHCPv6.")

	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	ipaddr := make(net.IP, len(relayMsg.LinkAddr))
	copy(ipaddr, relayMsg.LinkAddr)
	ipaddr[len(ipaddr)-1] += 1

	m, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("BUG: could not decapsulate: %v", err)
		return nil, true
	}

	if m.Options.OneIANA() == nil {
		log.Debug("No address requested")
		return resp, false
	}

	iana := &dhcpv6.OptIANA{
		IaId: m.Options.OneIANA().IaId,
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          ipaddr,
				PreferredLifetime: preferredLifeTime,
				ValidLifetime:     validLifeTime,
			},
		}},
	}
	resp.AddOption(iana)
	log.Infof("Added option IA prefix %s", iana.String())

	optIAPD := m.Options.OneIAPD()
	T1 := preferredLifeTime
	T2 := validLifeTime

	if optIAPD != nil {
		if optIAPD.T1 != 0 {
			T1 = optIAPD.T1
		}
		if optIAPD.T2 != 0 {
			T2 = optIAPD.T2
		}
		iapd := &dhcpv6.OptIAPD{
			IaId: optIAPD.IaId,
			T1:   T1,
			T2:   T2,
			Options: dhcpv6.PDOptions{Options: dhcpv6.Options{&dhcpv6.OptIAPrefix{
				PreferredLifetime: preferredLifeTime,
				ValidLifetime:     validLifeTime,
				Prefix: &net.IPNet{
					Mask: mask80,
					IP:   ipaddr.Mask(mask80),
				},
				Options: dhcpv6.PrefixOptions{Options: dhcpv6.Options{}},
			}}},
		}
		resp.UpdateOption(iapd)
		log.Infof("Added option IA prefix %s", iapd.String())
	}

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())

	return resp, false
}
