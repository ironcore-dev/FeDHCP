// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package macfilter

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/mdlayher/netx/eui64"
	"gopkg.in/yaml.v2"
)

var log = logger.GetLogger("plugins/macfilter")

var Plugin = plugins.Plugin{
	Name:   "macfilter",
	Setup6: setup,
}

var (
	macFilterConfig *api.MACFilterConfig
	err             error
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.MACFilterConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.MACFilterConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup(args ...string) (handler.Handler6, error) {
	macFilterConfig, err = loadConfig(args...)
	if err != nil {
		return nil, err
	}
	return handleDHCPv6, nil
}

func handleDHCPv6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())
	var mac net.HardwareAddr

	if !req.IsRelay() {
		log.Info("Received non-relay DHCPv6 request.")
		opt := req.GetOneOption(dhcpv6.OptionClientID)
		if opt == nil {
			log.Infof("OptionClientID is nil: %s", req.Summary())
			return nil, true
		}

		duid, err := dhcpv6.DUIDFromBytes(opt.ToBytes())
		if err != nil {
			log.Infof("Error occurred while getting DUID from Options: %s", req.Summary())
			return nil, true
		}

		switch d := duid.(type) {
		case *dhcpv6.DUIDLLT:
			mac = d.LinkLayerAddr
		case *dhcpv6.DUIDLL:
			mac = d.LinkLayerAddr
		default:
			return nil, true
		}

		if len(mac) == 0 {
			log.Infof("Client did not sent MAC address: %s", req.Summary())
			return nil, true
		}
	} else {
		log.Info("Received relay DHCPv6 request.")
		relayMsg := req.(*dhcpv6.RelayMessage)
		_, mac, err = eui64.ParseIP(relayMsg.PeerAddr)
		if err != nil {
			log.Errorf("Could not parse peer address %s: %s", relayMsg.PeerAddr.String(), err)
			return nil, true
		}
	}

	if (len(macFilterConfig.WhiteList) != 0 && !hasMacPrefix(macFilterConfig.WhiteList, mac.String())) || hasMacPrefix(macFilterConfig.BlackList, mac.String()) {
		return nil, true
	}
	return resp, false
}

func hasMacPrefix(macPrefix []string, mac string) bool {
	for _, prefix := range macPrefix {
		if strings.HasPrefix(strings.ToLower(mac), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
