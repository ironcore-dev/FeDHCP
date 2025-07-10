// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"fmt"
	"net/url"
	"os"

	"github.com/mdlayher/netx/eui64"

	"github.com/ironcore-dev/fedhcp/internal/api"
	h "github.com/ironcore-dev/fedhcp/internal/helper"
	"gopkg.in/yaml.v2"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/ztp")

// Plugin wraps plugin registration information
var Plugin = plugins.Plugin{
	Name:   "ztp",
	Setup6: setup6,
}

// map MAC address to inventory name
var inventory SwitchInventory

type SwitchInventory []api.Switch

const (
	optionZTPCode = 239
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.ZTPConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.ZTPConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}

func parseConfig(args ...string) error {
	ztpConfig, err := loadConfig(args...)
	if err != nil {
		return err
	}

	for _, switchEntry := range ztpConfig.Switches {
		scriptURL, err := url.Parse(switchEntry.ProvisioningScriptAddress)
		if err != nil {
			return fmt.Errorf("invalid ztp script scriptURL: %v", err)
		}

		if (scriptURL.Scheme != "http" && scriptURL.Scheme != "https") || scriptURL.Host == "" || scriptURL.Path == "" {
			return fmt.Errorf("malformed ZTP script parameter, should be a valid URL")
		}

		inventory = append(inventory, switchEntry)
	}

	return nil
}

func setup6(args ...string) (handler.Handler6, error) {
	err := parseConfig(args...)
	if err != nil {
		return nil, err
	}

	if len(inventory) == 0 {
		log.Errorf("no switches found in inventory")
		return nil, nil
	}

	log.Printf("loaded ZTP plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	h.PrintRequest(req, log)

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	m, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("could not decapsulate: %v", err)
		return nil, true
	}

	if !m.IsOptionRequested(optionZTPCode) {
		log.Debug("No ZTP option requested")
		h.PrintResponse(req, resp, log)

		return resp, false
	}

	relayMsg := req.(*dhcpv6.RelayMessage)
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("could not parse peer address %s: %s", relayMsg.PeerAddr.String(), err)
		return nil, true
	}

	switchMACFound := false
	for _, switchEntry := range inventory {
		if switchEntry.MacAddress == mac.String() {
			log.Infof("MAC address %s found in inventory, switch: %s", mac.String(), switchEntry.Name)
			switchMACFound = true

			buf := []byte(switchEntry.ProvisioningScriptAddress)
			opt := &dhcpv6.OptionGeneric{
				OptionCode: optionZTPCode,
				OptionData: buf,
			}

			resp.AddOption(opt)
			log.Debugf("Added option %s", opt)
		}
	}

	if !switchMACFound {
		log.Infof("MAC address %s not found in inventory", mac.String())
	}

	h.PrintResponse(req, resp, log)

	return resp, false
}
