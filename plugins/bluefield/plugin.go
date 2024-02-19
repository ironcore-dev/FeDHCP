package bluefield

import (
	"fmt"
	"net"
	"time"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
)

var log = logger.GetLogger("plugins/bluefield")

var Plugin = plugins.Plugin{
	Name:   "bluefield",
	Setup6: setupPlugin,
}
var ipaddr net.IP

// setupPlugin initializes the plugin with given arguments.
func setupPlugin(args ...string) (handler.Handler6, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("plugin bluefield requires at least one argument (static IPv6 address)")
	}
	ipaddr = net.ParseIP(args[0])
	if ipaddr == nil {
		return nil, fmt.Errorf("invalid IPv6 address: %s", args[0])
	}
	log.Infof("Parsed IP %s", ipaddr)
	return handleDHCPv6, nil
}

func handleDHCPv6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) { //nolint:staticcheck
	m, err := req.GetInnerMessage()
	if err != nil {
		return nil, true
	}

	hwaddr, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		return nil, true
	}

	v6ServerID := &dhcpv6.DUIDLL{
		HWType:        iana.HWTypeEthernet,
		LinkLayerAddr: hwaddr,
	}

	switch m.Type() {
	case dhcpv6.MessageTypeSolicit:
		resp, err := dhcpv6.NewAdvertiseFromSolicit(m)
		if err != nil {
			log.Errorf("Failed to create DHCPv6 advertise: %v", err)
			return nil, true
		}

		log.Infof("IP: %s", ipaddr)

		resp.AddOption(&dhcpv6.OptIANA{
			IaId: m.Options.OneIANA().IaId,
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

		dhcpv6.WithServerID(v6ServerID)(resp)
		return resp, false

	case dhcpv6.MessageTypeRequest:
		resp, err = dhcpv6.NewReplyFromMessage(m) //nolint:staticcheck
		if err != nil {
			log.Errorf("Failed to create DHCPv6 reply: %v", err)
			return nil, false
		}

		resp.AddOption(&dhcpv6.OptIANA{
			IaId: m.Options.OneIANA().IaId,
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

		dhcpv6.WithServerID(v6ServerID)(resp)
		return resp, true
	}
	return nil, false
}
