package metal

import (
	"context"
	"encoding/json"
	"fmt"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"net/netip"
	"os"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"strings"
)

var log = logger.GetLogger("plugins/metal")

var Plugin = plugins.Plugin{
	Name:   "metal",
	Setup6: setup6,
	Setup4: setup4,
}

var (
	config *api.Machines
	// map MAC address to machine name
	machineMap = make(map[string]string)
)

// args[0] = path to configuration file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the metal plugin, got %d", len(args))
	}
	return args[0], nil
}

func setup6(args ...string) (handler.Handler6, error) {
	if err := loadConfig(args...); err != nil {
		return nil, err
	}
	return handler6, nil
}

func loadConfig(args ...string) error {
	fmt.Print("loading metal config")
	path, err := parseArgs(args...)
	if err != nil {
		return fmt.Errorf("invalid configuration: %v", err)
	}

	configData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	config = &api.Machines{}
	if err := json.Unmarshal(configData, config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	for _, m := range config.MachineList {
		machineMap[m.MacAddress] = m.Name
	}
	return nil
}

func setup4(args ...string) (handler.Handler4, error) {
	if err := loadConfig(args...); err != nil {
		return nil, err
	}
	return handler4, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

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

	if err := applyEndpointForMACAddress(mac, ipamv1alpha1.CIPv6SubnetType); err != nil {
		log.Errorf("Could not apply endpoint for mac %s: %s", mac.String(), err)
		return resp, false
	}

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())
	return resp, false
}

func handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	log.Debugf("Received DHCPv4 request: %s", req.Summary())

	mac := req.ClientHWAddr

	if err := applyEndpointForMACAddress(mac, ipamv1alpha1.CIPv4SubnetType); err != nil {
		log.Errorf("Could not apply peer address: %s", err)
		return resp, false
	}

	log.Debugf("Sent DHCPv4 response: %s", resp.Summary())
	return resp, false
}

func applyEndpointForMACAddress(mac net.HardwareAddr, subnetFamily ipamv1alpha1.SubnetAddressType) error {
	machineName, ok := machineMap[mac.String()]
	if !ok {
		// done here, next plugin
		return fmt.Errorf("unknown machine MAC address: %s", mac.String())
	}

	ip, err := GetIPForMACAddress(mac, subnetFamily)
	if err != nil {
		return fmt.Errorf("could not get IP for MAC address %s: %s", mac.String(), err)
	}

	if ip != nil {
		if err := ApplyEndpointForMachine(machineName, mac, ip); err != nil {
			return fmt.Errorf("could not apply endpoint for machine: %s", err)
		}
	} else {
		log.Infof("Could not find IP for MAC address %s", mac.String())
	}

	return nil
}

func ApplyEndpointForMachine(name string, mac net.HardwareAddr, ip *netip.Addr) error {
	if ip == nil {
		log.Info("No IP address specified. Skipping.")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	endpoint := &metalv1alpha1.Endpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: metalv1alpha1.EndpointSpec{
			MACAddress: mac.String(),
			IP:         metalv1alpha1.MustParseIP(ip.String()),
		},
	}

	cl := kubernetes.GetClient()
	if cl == nil {
		return fmt.Errorf("kubernetes client not initialized")
	}

	if _, err := controllerruntime.CreateOrUpdate(ctx, cl, endpoint, func() error { return nil }); err != nil {
		return fmt.Errorf("failed to apply endpoint: %v", err)
	}

	return nil
}

func GetIPForMACAddress(mac net.HardwareAddr, subnetFamily ipamv1alpha1.SubnetAddressType) (*netip.Addr, error) {
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

	sanitizedMAC := strings.Replace(mac.String(), ":", "", -1)
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
