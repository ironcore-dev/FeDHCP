// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"fmt"
	"net/url"
	"os"

	"github.com/ironcore-dev/fedhcp/internal/api"
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

var (
	provisioningScript string
)

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

func parseConfig(args ...string) (*url.URL, error) {
	ztpConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}
	scriptURL, err := url.Parse(ztpConfig.ProvisioningScriptAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid ztp script scriptURL: %v", err)
	}

	if (scriptURL.Scheme != "http" && scriptURL.Scheme != "https") || scriptURL.Host == "" || scriptURL.Path == "" {
		return nil, fmt.Errorf("malformed ZTP script parameter, should be a valid URL")
	}

	return scriptURL, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	scriptURL, err := parseConfig(args...)
	if err != nil {
		return nil, err
	}

	provisioningScript = scriptURL.String()

	log.Printf("loaded ZTP plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

	if provisioningScript == "" {
		// nothing to do
		return resp, false
	}

	//var opt dhcpv6.Option

	// TODO: ZTP check?
	buf := []byte(provisioningScript)
	opt := &dhcpv6.OptionGeneric{
		OptionCode: optionZTPCode,
		OptionData: buf,
	}

	//if opt != nil {
	resp.AddOption(opt)
	log.Debugf("Added option %s", opt)
	//}

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())
	return resp, false
}
