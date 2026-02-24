// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package leases

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/helper"
	"github.com/ironcore-dev/fedhcp/internal/printer"
	"gopkg.in/yaml.v3"
)

var log = logger.GetLogger("plugins/leases")

var Plugin = plugins.Plugin{
	Name:   "leases",
	Setup6: setup6,
}

var leaseClient *k8sClient

func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.LeasesConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.LeasesConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	leasesConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}

	leaseClient = newK8sClient(leasesConfig.Namespace)

	log.Printf("Loaded leases plugin for DHCPv6.")
	return handler6, nil
}

// ipToResourceName returns the expanded IPv6 address with dashes as separators,
// suitable for use as a Kubernetes resource name (e.g., "2001-0db8-0000-0000-0000-0000-0000-0001").
func ipToResourceName(ip net.IP) string {
	ip = ip.To16()
	if ip == nil {
		return ""
	}
	return fmt.Sprintf("%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x",
		ip[0], ip[1], ip[2], ip[3],
		ip[4], ip[5], ip[6], ip[7],
		ip[8], ip[9], ip[10], ip[11],
		ip[12], ip[13], ip[14], ip[15])
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

	mac, err := helper.GetMAC(relayMsg, log)
	if err != nil {
		log.Errorf("Could not determine client MAC: %v", err)
		return nil, true
	}

	respMsg, ok := resp.(*dhcpv6.Message)
	if !ok || respMsg.Options.OneIANA() == nil {
		log.Debug("No IANA in response, nothing to record")
		return resp, false
	}

	iana := respMsg.Options.OneIANA()
	addr := iana.Options.OneAddress()
	if addr == nil {
		log.Debug("No address in IANA response, nothing to record")
		return resp, false
	}

	ip := addr.IPv6Addr
	resourceName := ipToResourceName(ip)
	if resourceName == "" {
		log.Errorf("Could not convert IP %s to resource name", ip)
		return nil, true
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = leaseClient.applyLease(ctx, mac, ip, resourceName, addr.ValidLifetime)
	if err != nil {
		log.Errorf("Failed to apply lease: %v", err)
		return nil, true
	}

	return resp, false
}
