// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"context"
	"fmt"

	"github.com/ironcore-dev/fedhcp/internal/printer"

	"net"
	"os"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v3"

	"github.com/mdlayher/netx/eui64"
)

var log = logger.GetLogger("plugins/ipam")

var Plugin = plugins.Plugin{
	Name:   "ipam",
	Setup6: setup6,
}

var (
	k8sClient *K8sClient
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.IPAMConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.IPAMConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	ipamConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}

	k8sClient, err = NewK8sClient(ipamConfig.Namespace, ipamConfig.Subnets)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Printf("Loaded ipam plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if req == nil {
		log.Error("Received nil IPv6 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv6)
	defer printer.VerboseResponse(req, resp, log, printer.IPv6)

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Debugf("Generated IP address %s for mac %s", ipaddr.String(), mac.String())
	err = k8sClient.createIpamIP(ctx, ipaddr, mac)
	if err != nil {
		log.Errorf("Could not create IPAM IP: %s", err)
		return nil, true
	}

	return resp, false
}
