// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ntp

import (
	"fmt"
	"os"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"gopkg.in/yaml.v3"

	"github.com/ironcore-dev/fedhcp/internal/api"
)

var log = logger.GetLogger("plugins/ntp")

var Plugin = plugins.Plugin{
	Name:   "ntp",
	Setup4: setup4,
	Setup6: setup6,
}

var ntpConfig *api.NTPConfig

func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.NTPConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.NTPConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	var err error
	ntpConfig, err = loadConfig(args...)
	if err != nil {
		return nil, err
	}

	log.Print("Loaded NTP plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if len(ntpConfig.ServersV6) > 0 {
		opt := &dhcpv6.OptNTPServer{}
		for _, server := range ntpConfig.ServersV6 {
			so := dhcpv6.NTPSuboptionSrvAddr(server)
			opt.Suboptions.Add(&so)
		}
		resp.AddOption(opt)
	}

	return resp, false
}

func setup4(args ...string) (handler.Handler4, error) {
	var err error
	ntpConfig, err = loadConfig(args...)
	if err != nil {
		return nil, err
	}

	log.Print("Loaded NTP plugin for DHCPv6.")
	return handler4, nil
}

func handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	if len(ntpConfig.Servers) > 0 {
		resp.Options.Update(dhcpv4.OptNTPServers(ntpConfig.Servers...))
	}
	return resp, false
}
