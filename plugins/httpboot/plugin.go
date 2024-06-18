// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package httpboot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var bootFile4 string
var bootFile6 string
var useBootService bool

var log = logger.GetLogger("plugins/httpboot")

var Plugin = plugins.Plugin{
	Name:   "httpboot",
	Setup6: setup6,
	Setup4: setup4,
}

func parseArgs(args ...string) (*url.URL, bool, error) {
	if len(args) != 1 {
		return nil, false, fmt.Errorf("exactly one argument must be passed to the httpboot plugin, got %d", len(args))
	}
	arg := args[0]
	useBootService := strings.HasPrefix(arg, "bootservice:")
	if useBootService {
		arg = strings.TrimPrefix(arg, "bootservice:")
	}
	parsedURL, err := url.Parse(arg)
	if err != nil {
		return nil, false, fmt.Errorf("invalid URL: %v", err)
	}
	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.Path == "" {
		return nil, false, fmt.Errorf("malformed iPXE parameter, should be a valid URL")
	}
	return parsedURL, useBootService, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	u, ubs, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}
	bootFile6 = u.String()
	useBootService = ubs
	log.Printf("Configured httpboot plugin with URL: %s, useBootService: %t", bootFile6, useBootService)
	return handler6, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	u, _, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	bootFile4 = u.String()
	log.Printf("loaded httpboot plugin for DHCPv4.")
	return handler4, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

	var ukiURL string
	if !useBootService {
		ukiURL = bootFile6
	} else {
		clientIPs, err := extractClientIP6(req)
		if err != nil {
			log.Errorf("failed to extract ClientIP, Error: %v Request: %v ", err, req)
			return resp, false
		}
		ukiURL, err = fetchUKIURL(bootFile6, clientIPs)
		if err != nil {
			log.Errorf("failed to fetch UKI URL: %v", err)
			return resp, false
		}
	}

	decap, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("could not decapsulate request: %v", err)
		return nil, true
	}

	if decap.GetOneOption(dhcpv6.OptionVendorClass) != nil {
		optVendorClass := decap.GetOneOption(dhcpv6.OptionVendorClass)
		log.Debugf("VendorClass: %s", optVendorClass.String())
		vcc := optVendorClass.ToBytes()
		if len(vcc) >= 16 && binary.BigEndian.Uint16(vcc[4:6]) >= 10 && string(vcc[6:16]) == "HTTPClient" {
			bf := dhcpv6.OptBootFileURL(ukiURL)
			resp.AddOption(bf)
			log.Infof("Added option BootFileURL(%d): (%s)", dhcpv6.OptionBootfileURL, ukiURL)

			buf := []byte("HTTPClient")
			vc := &dhcpv6.OptVendorClass{
				EnterpriseNumber: 0,
				Data:             [][]byte{buf},
			}
			resp.AddOption(vc)
			log.Infof("Added option VendorClass %s", vc.String())
		} else {
			log.Errorf("non HTTPClient VendorClass %s", optVendorClass.String())
			return resp, false
		}
	}

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())
	return resp, false
}

func handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	log.Debugf("Received DHCPv4 request: %s", req.Summary())

	ukiURL, err := fetchUKIURL(bootFile4, []string{req.ClientIPAddr.String()})
	if err != nil {
		log.Errorf("failed to fetch UKI URL: %v", err)
		return resp, false
	}

	if req.GetOneOption(dhcpv4.OptionClassIdentifier) != nil {
		ci := req.GetOneOption(dhcpv4.OptionClassIdentifier)
		log.Debugf("ClassIdentifier: %s (%s)", string(ci), ci)
		if strings.Contains(string(ci), "HTTPClient") {
			bf := &dhcpv4.Option{
				Code:  dhcpv4.OptionBootfileName,
				Value: dhcpv4.String(ukiURL),
			}
			resp.Options.Update(*bf)
			vid := &dhcpv4.Option{
				Code:  dhcpv4.OptionClassIdentifier,
				Value: dhcpv4.String("HTTPClient"),
			}
			resp.Options.Update(*vid)
		}
	}
	return resp, false
}

func extractClientIP6(req dhcpv6.DHCPv6) ([]string, error) {
	if req.IsRelay() {
		relayMsg, ok := req.(*dhcpv6.RelayMessage)
		if !ok {
			return nil, fmt.Errorf("failed to cast the DHCPv6 request to a RelayMessage")
		}

		var addresses []string
		if relayMsg.LinkAddr != nil {
			addresses = append(addresses, relayMsg.LinkAddr.String())
		}

		if _, linkLayerAddress := relayMsg.Options.ClientLinkLayerAddress(); linkLayerAddress != nil {
			addresses = append(addresses, linkLayerAddress.String())
		}

		if len(addresses) == 0 {
			return nil, fmt.Errorf("no client IP or link-layer address found in the relay message")
		}

		return addresses, nil
	}
	return nil, fmt.Errorf("received non-relay DHCPv6 request, client IP cannot be extracted from non-relayed messages")
}

func fetchUKIURL(url string, clientIPs []string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	xForwardedFor := strings.Join(clientIPs, ", ")
	req.Header.Set("X-Forwarded-For", xForwardedFor)

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("HTTP request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var data struct {
		UKIURL string `json:"UKIURL"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}

	if data.UKIURL == "" {
		return "", fmt.Errorf("received empty UKI URL")
	}

	return data.UKIURL, nil
}
