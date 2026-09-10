// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package onie

import (
	"fmt"
	"os"

	"github.com/ironcore-dev/fedhcp/internal/printer"

	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v2"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/onie")

// Plugin wraps plugin registration information
var Plugin = plugins.Plugin{
	Name:   "onie",
	Setup6: setup6,
}

// onieImagesAddress is the base URL for ONIE installer images
var onieImagesAddress string

const (
	onieUserClass = "onie_dhcp_user_class"
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.ONIEConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.ONIEConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	onieConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}

	if onieConfig.OnieImagesAddress == "" {
		return nil, fmt.Errorf("onieImagesAddress must be set in the ONIE config")
	}

	onieImagesAddress = onieConfig.OnieImagesAddress
	log.Printf("loaded ONIE plugin for DHCPv6 with images address: %s", onieImagesAddress)
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

	m, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("could not decapsulate: %v", err)
		return nil, true
	}

	if !isONIERequest(m) {
		return resp, false
	}

	bf := dhcpv6.OptBootFileURL(onieImagesAddress)
	resp.AddOption(bf)
	log.Infof("Added ONIE BootFileURL option: %s", onieImagesAddress)

	return resp, false
}

// isONIERequest checks whether the DHCPv6 message is an ONIE discovery request
// by verifying that Boot File URL (option 59) is requested and UserClass contains
// "onie_dhcp_user_class".
func isONIERequest(m *dhcpv6.Message) bool {
	if !m.IsOptionRequested(dhcpv6.OptionBootfileURL) {
		return false
	}
	userClasses := m.Options.UserClasses()
	for _, uc := range userClasses {
		if string(uc) == onieUserClass {
			return true
		}
	}
	return false
}
