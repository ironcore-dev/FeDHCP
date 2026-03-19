// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"encoding/binary"
	"fmt"
	"net/url"
	"os"

	"github.com/ironcore-dev/fedhcp/internal/helper"
	"github.com/ironcore-dev/fedhcp/internal/printer"

	"github.com/mdlayher/netx/eui64"

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

// map MAC address to inventory name
var inventory SwitchInventory

// globalProvisioningScriptAddress is the default ZTP script URL for all switches
var globalProvisioningScriptAddress string

// onieInstallers maps vendor class data strings to installer URLs
var onieInstallers map[string]string

type SwitchInventory []api.Switch

const (
	optionZTPCode = 239
	onieUserClass = "onie_dhcp_user_class"
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

func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %v", rawURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path == "" {
		return fmt.Errorf("malformed URL %q, should be a valid http(s) URL", rawURL)
	}
	return nil
}

func parseConfig(args ...string) error {
	ztpConfig, err := loadConfig(args...)
	if err != nil {
		return err
	}

	// Validate global provisioning script address if set
	if ztpConfig.ProvisioningScriptAddress != "" {
		if err := validateURL(ztpConfig.ProvisioningScriptAddress); err != nil {
			return fmt.Errorf("invalid global provisioning script address: %v", err)
		}
		globalProvisioningScriptAddress = ztpConfig.ProvisioningScriptAddress
	}

	for _, switchEntry := range ztpConfig.Switches {
		// Resolve provisioning script address: per-switch override or global default
		scriptAddr := switchEntry.ProvisioningScriptAddress
		if scriptAddr == "" {
			scriptAddr = globalProvisioningScriptAddress
		}

		if scriptAddr != "" {
			if err := validateURL(scriptAddr); err != nil {
				return fmt.Errorf("invalid ZTP script URL for switch %s: %v", switchEntry.Name, err)
			}
		}

		inventory = append(inventory, switchEntry)
	}

	// Parse ONIE installer mappings
	onieInstallers = make(map[string]string)
	for _, installer := range ztpConfig.ONIEInstallers {
		if installer.Vendor == "" {
			return fmt.Errorf("ONIE installer entry has empty vendor string")
		}
		if err := validateURL(installer.InstallerURL); err != nil {
			return fmt.Errorf("invalid ONIE installer URL for vendor %s: %v", installer.Vendor, err)
		}
		onieInstallers[installer.Vendor] = installer.InstallerURL
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

	relayMsg := req.(*dhcpv6.RelayMessage)

	// Handle ONIE installer request
	if isONIERequest(m) {
		handleONIE(relayMsg, m, resp)
	}

	// Hanlde ZTP provisioning script request
	if m.IsOptionRequested(optionZTPCode) {
		handleZTP(relayMsg, resp)
	}

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

// getVendorClassData extracts the vendor class data string from the DHCPv6 message.
// ONIE sends VendorClass with EnterpriseNumber=xyz (4-bytes) and data like "onie_vendor:x86_64-accton_as7726_32x-r0".
func getVendorClassData(m *dhcpv6.Message) string {
	opt := m.GetOneOption(dhcpv6.OptionVendorClass)
	if opt == nil {
		return ""
	}

	// Parse raw bytes: uint32 enterprise number + (uint16 length + data)*
	vcc := opt.ToBytes()
	if len(vcc) < 6 {
		return ""
	}

	entNum := binary.BigEndian.Uint32(vcc[0:4])
	if entNum != 0 {
		return ""
	}

	dataLen := int(binary.BigEndian.Uint16(vcc[4:6]))
	if len(vcc) < 6+dataLen {
		return ""
	}

	return string(vcc[6 : 6+dataLen])
}

func handleONIE(relayMsg *dhcpv6.RelayMessage, m *dhcpv6.Message, resp dhcpv6.DHCPv6) {
	if len(onieInstallers) == 0 {
		log.Debug("No ONIE installers configured")
		return
	}

	mac, err := helper.GetMAC(relayMsg, log)
	if err != nil {
		log.Errorf("could not get MAC address: %v", err)
		return
	}

	// Check MAC is in inventory
	macFound := false
	for _, switchEntry := range inventory {
		if switchEntry.MacAddress == mac.String() {
			macFound = true
			log.Infof("ONIE request from known switch %s (MAC %s)", switchEntry.Name, mac.String())
			break
		}
	}
	if !macFound {
		log.Infof("ONIE request from unknown MAC %s, ignoring", mac.String())
		return
	}

	vendorClass := getVendorClassData(m)
	if vendorClass == "" {
		log.Warning("ONIE request has no vendor class data")
		return
	}

	installerURL, ok := onieInstallers[vendorClass]
	if !ok {
		log.Warningf("No ONIE installer configured for vendor %q", vendorClass)
		return
	}

	bf := dhcpv6.OptBootFileURL(installerURL)
	resp.AddOption(bf)
	log.Infof("Added ONIE BootFileURL option: %s (vendor: %s)", installerURL, vendorClass)
}

func handleZTP(relayMsg *dhcpv6.RelayMessage, resp dhcpv6.DHCPv6) {
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("could not parse peer address %s: %s", relayMsg.PeerAddr.String(), err)
		return
	}

	switchMACFound := false
	for _, switchEntry := range inventory {
		if switchEntry.MacAddress == mac.String() {
			log.Infof("MAC address %s found in inventory, switch: %s", mac.String(), switchEntry.Name)
			switchMACFound = true

			// Use per-switch override if set, otherwise use global default
			scriptAddr := switchEntry.ProvisioningScriptAddress
			if scriptAddr == "" {
				scriptAddr = globalProvisioningScriptAddress
			}

			if scriptAddr == "" {
				log.Warningf("No provisioning script address configured for switch %s", switchEntry.Name)
				break
			}

			buf := []byte(scriptAddr)
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
}
