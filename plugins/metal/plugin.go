// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package metal

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/ironcore-dev/fedhcp/internal/printer"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	"github.com/mdlayher/netx/eui64"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var log = logger.GetLogger("plugins/metal")

var Plugin = plugins.Plugin{
	Name:   "metal",
	Setup6: setup6,
	Setup4: setup4,
}

// map MAC address to inventory name
var inventory *Inventory

type Inventory struct {
	Entries  map[string]string
	Strategy OnBoardingStrategy
}

// default inventory name prefix
const defaultNamePrefix = "compute-"

type OnBoardingStrategy string

const (
	OnBoardingStrategyStatic  OnBoardingStrategy = "Static"
	OnboardingStrategyDynamic OnBoardingStrategy = "Dynamic"
)

// args[0] = path to inventory file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func setup6(args ...string) (handler.Handler6, error) {
	var err error
	inventory, err = loadConfig(args...)
	if err != nil {
		return nil, err
	}
	if inventory == nil || len(inventory.Entries) == 0 {
		return nil, nil
	}

	return handler6, nil
}

func loadConfig(args ...string) (*Inventory, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var config api.MetalConfig
	if err = yaml.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	inv := &Inventory{}
	entries := make(map[string]string)
	switch {
	// static inventory list has precedence, always
	case len(config.Inventories) > 0:
		inv.Strategy = OnBoardingStrategyStatic
		log.Debug("Using static list onboarding")
		for _, i := range config.Inventories {
			if i.MacAddress != "" && i.Name != "" {
				entries[strings.ToLower(i.MacAddress)] = i.Name
			}
		}
	case len(config.Filter.MacPrefix) > 0:
		inv.Strategy = OnboardingStrategyDynamic
		namePrefix := defaultNamePrefix
		if config.NamePrefix != "" {
			namePrefix = config.NamePrefix
		}
		log.Debugf("Using MAC address prefix filter onboarding with name prefix '%s'", namePrefix)
		for _, i := range config.Filter.MacPrefix {
			entries[strings.ToLower(i)] = namePrefix
		}
	default:
		log.Infof("No inventories loaded")
		return nil, nil
	}

	inv.Entries = entries

	log.Infof("Loaded config with %d inventories", len(entries))
	return inv, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	var err error
	inventory, err = loadConfig(args...)
	if err != nil {
		return nil, err
	}
	if inventory == nil || len(inventory.Entries) == 0 {
		return nil, nil
	}

	return handler4, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if req == nil {
		log.Error("Received nil IPv6 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv6)
	defer printer.VerboseResponse(req, resp, log, printer.IPv6)

	if !req.IsRelay() {
		log.Info("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("Could not parse peer address %s: %s", relayMsg.PeerAddr.String(), err)
		return nil, true
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ApplyEndpointForMACAddress(ctx, mac, ipamv1alpha1.IPv6SubnetType); err != nil {
		log.Errorf("Could not apply endpoint for mac %s: %s", mac.String(), err)
	}

	return resp, false
}

func handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	if req == nil {
		log.Error("Received nil IPv4 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv4)
	defer printer.VerboseResponse(req, resp, log, printer.IPv4)

	mac := req.ClientHWAddr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ApplyEndpointForMACAddress(ctx, mac, ipamv1alpha1.IPv4SubnetType); err != nil {
		log.Errorf("Could not apply endpoint for mac %s: %s", mac.String(), err)
	}

	return resp, false
}

func ApplyEndpointForMACAddress(ctx context.Context, mac net.HardwareAddr, subnetFamily ipamv1alpha1.SubnetAddressType) error {
	inventoryName := GetInventoryEntryMatchingMACAddress(mac)
	if inventoryName == "" {
		log.Print("Unknown inventory, not processing")
		return nil
	}

	ip, err := GetIPAMIPAddressForMACAddress(mac, subnetFamily)
	if err != nil {
		return fmt.Errorf("could not get IPAM IP for MAC address %s: %s", mac.String(), err)
	}

	if ip != nil {
		if err := ApplyEndpointForInventory(ctx, inventoryName, mac, ip); err != nil {
			if errors.IsAlreadyExists(err) {
				log.Debugf("Endpoint %s (%s) exists, nothing to do", mac.String(), ip.String())
			} else {
				return fmt.Errorf("could not apply endpoint for inventory: %s", err)
			}
		} else {
			log.Infof("Successfully applied endpoint for inventory %s (%s)", inventoryName, mac.String())
		}
	} else {
		log.Infof("Could not find IPAM IP for MAC address %s", mac.String())
	}

	return nil
}

func ApplyEndpointForInventory(ctx context.Context, name string, mac net.HardwareAddr, ip *netip.Addr) error {
	if ip == nil {
		log.Info("No IP address specified. Skipping.")
		return nil
	}

	cl := kubernetes.GetClient()
	if cl == nil {
		return fmt.Errorf("kubernetes client not initialized")
	}

	switch inventory.Strategy {
	case OnBoardingStrategyStatic:
		// we do know the real name, so CreateOrPatch is fine
		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: metalv1alpha1.EndpointSpec{
				MACAddress: mac.String(),
				IP:         metalv1alpha1.MustParseIP(ip.String()),
			},
		}
		if _, err := controllerutil.CreateOrPatch(ctx, cl, endpoint, nil); err != nil {
			return fmt.Errorf("failed to apply endpoint: %v", err)
		}
	case OnboardingStrategyDynamic:
		// the (generated) name is unknown, so go for filtering
		if existingEndpoint, _ := GetEndpointForMACAddress(mac); existingEndpoint != nil {
			if existingEndpoint.Spec.IP.String() != ip.String() {
				log.Debugf("Endpoint exists with different IP address, updating IP address %s to %s",
					existingEndpoint.Spec.IP.String(), ip.String())

				existingEndpointBase := existingEndpoint.DeepCopy()
				existingEndpoint.Spec.IP = metalv1alpha1.MustParseIP(ip.String())

				if err := cl.Patch(ctx, existingEndpoint, client.MergeFrom(existingEndpointBase)); err != nil {
					return fmt.Errorf("failed to patch endpoint: %v", err)
				}
			} else {
				return errors.NewAlreadyExists(
					schema.GroupResource{Group: metalv1alpha1.GroupVersion.Group, Resource: "Endpoints"},
					existingEndpoint.Name,
				)
			}
		} else {
			log.Debugf("Endpoint %s (%s) does not exist, creating", mac.String(), ip.String())
			endpoint := &metalv1alpha1.Endpoint{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: name,
				},
				Spec: metalv1alpha1.EndpointSpec{
					MACAddress: mac.String(),
					IP:         metalv1alpha1.MustParseIP(ip.String()),
				},
			}
			if err := cl.Create(ctx, endpoint); err != nil {
				return fmt.Errorf("failed to create endpoint: %v", err)
			}
		}
	default:
		return fmt.Errorf("unknown OnboardingStrategy %s", inventory.Strategy)
	}

	return nil
}

func GetEndpointForMACAddress(mac net.HardwareAddr) (*metalv1alpha1.Endpoint, error) {
	cl := kubernetes.GetClient()
	if cl == nil {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	epList := &metalv1alpha1.EndpointList{}
	if err := cl.List(ctx, epList); err != nil {
		return nil, fmt.Errorf("failed to list Endpoints: %v", err)
	}

	for _, ep := range epList.Items {
		if ep.Spec.MACAddress == mac.String() {
			return &ep, nil
		}
	}
	return nil, nil
}

func GetInventoryEntryMatchingMACAddress(mac net.HardwareAddr) string {
	switch inventory.Strategy {
	case OnBoardingStrategyStatic:
		inventoryName, ok := inventory.Entries[strings.ToLower(mac.String())]
		if !ok {
			log.Debugf("Unknown inventory MAC address: %s", mac.String())
		} else {
			return inventoryName
		}
	case OnboardingStrategyDynamic:
		for i := range inventory.Entries {
			if strings.HasPrefix(strings.ToLower(mac.String()), strings.ToLower(i)) {
				return inventory.Entries[i]
			}
		}
		// we don't onboard by default yet, might change in the future
		log.Debugf("Inventory MAC address %s does not match any inventory MAC prefix", mac.String())
	default:
		log.Debugf("Unknown Onboarding strategy %s", inventory.Strategy)
	}

	return ""
}

func GetIPAMIPAddressForMACAddress(mac net.HardwareAddr, subnetFamily ipamv1alpha1.SubnetAddressType) (*netip.Addr, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cl := kubernetes.GetClient()
	if cl == nil {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}

	ips := &ipamv1alpha1.IPList{}
	if err := cl.List(ctx, ips); err != nil {
		return nil, fmt.Errorf("failed to list IPs: %v", err)
	}

	sanitizedMAC := strings.ReplaceAll(strings.ToLower(mac.String()), ":", "")
	for _, ip := range ips.Items {
		if ip.Labels["mac"] == sanitizedMAC && ipFamilyMatches(ip, subnetFamily) {
			return &ip.Status.Reserved.Net, nil
		}
	}

	return nil, nil
}

func ipFamilyMatches(ip ipamv1alpha1.IP, subnetFamily ipamv1alpha1.SubnetAddressType) bool {
	ipAddr := ip.Status.Reserved.String()

	return strings.Contains(ipAddr, ":") && subnetFamily == "IPv6" ||
		strings.Contains(ipAddr, ".") && subnetFamily == "IPv4"
}
