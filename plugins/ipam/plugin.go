// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"fmt"
	"net"
	"strings"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"

	"github.com/mdlayher/netx/eui64"
)

var log = logger.GetLogger("plugins/ipam")

var Plugin = plugins.Plugin{
	Name:   "ipam",
	Setup6: setup6,
}

var (
	k8sClient K8sClient
)

func parseArgs(args ...string) (string, []string, error) {
	if len(args) < 2 {
		return "", []string{""}, fmt.Errorf("At least two arguments must be passed to ipam plugin, a namespace and a comma-separated subnet names list, got %d", len(args))
	}

	namespace := args[0]
	subnetNames := strings.Split(args[1], ",")
	return namespace, subnetNames, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	namespace, subnetNames, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	k8sClient = NewK8sClient(namespace, subnetNames)
	log.Printf("loaded ipam plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("received DHCPv6 packet: %s", req.Summary())

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	// Retrieve IPv6 prefix and MAC address from IPv6 address
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("Could not parse peer address: %s", err)
		return nil, true
	}

	ipaddr := make(net.IP, len(relayMsg.LinkAddr))
	copy(ipaddr, relayMsg.LinkAddr)
	ipaddr[len(ipaddr)-1] += 1

	log.Infof("Generated IP address %s for mac %s", ipaddr.String(), mac.String())
	err = k8sClient.createIpamIP(ipaddr, mac)
	if err != nil {
		log.Errorf("Could not create IPAM IP: %s", err)
		return nil, true
	}

	return resp, false
}
