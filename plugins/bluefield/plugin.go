// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package bluefield

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/printer"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v2"
)

var log = logger.GetLogger("plugins/bluefield")

var Plugin = plugins.Plugin{
	Name:   "bluefield",
	Setup6: setupPlugin,
}
var ipaddr net.IP

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.BluefieldConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.BluefieldConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

// setupPlugin initializes the plugin with given bluefield config.
func setupPlugin(args ...string) (handler.Handler6, error) {
	bluefieldIPConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}
	ipaddr = net.ParseIP(bluefieldIPConfig.BulefieldIP)
	if ipaddr == nil {
		return nil, fmt.Errorf("invalid IPv6 address: %s", args[0])
	}
	log.Infof("Parsed IP %s", ipaddr)
	return handleDHCPv6, nil
}

func handleDHCPv6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if req == nil {
		log.Error("Received nil IPv6 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv6)
	defer printer.VerboseResponse(req, resp, log, printer.IPv6)

	m, err := req.GetInnerMessage()
	if err != nil {
		return nil, true
	}

	if m.Options.OneIANA() != nil {
		switch m.Type() {
		case dhcpv6.MessageTypeSolicit:
			if resp == nil {
				resp, err = dhcpv6.NewAdvertiseFromSolicit(m)
				if err != nil {
					log.Errorf("Failed to create DHCPv6 advertise: %v", err)
					return nil, true
				}
			}
			addOptIANA(resp, m.Options.OneIANA().IaId)
		case dhcpv6.MessageTypeRequest:
			if resp == nil {
				resp, err = dhcpv6.NewReplyFromMessage(m)
				if err != nil {
					log.Errorf("Failed to create DHCPv6 reply: %v", err)
					return nil, true
				}
			}
			addOptIANA(resp, m.Options.OneIANA().IaId)
		}
	}

	return resp, false
}

func addOptIANA(resp dhcpv6.DHCPv6, iaId [4]byte) {
	resp.AddOption(&dhcpv6.OptIANA{
		IaId: iaId,
		T1:   1 * time.Hour,
		T2:   2 * time.Hour,
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          ipaddr,
				PreferredLifetime: 24 * time.Hour,
				ValidLifetime:     48 * time.Hour,
			},
		}},
	})
	log.Infof("Added option IANA, address %s", ipaddr)
}
