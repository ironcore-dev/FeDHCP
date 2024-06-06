// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package httpboot

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
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

var log = logger.GetLogger("plugins/httpboot")

var Plugin = plugins.Plugin{
	Name:   "httpboot",
	Setup6: setup6,
	Setup4: setup4,
}

func parseArgs(args ...string) (*url.URL, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("exactly one argument must be passed to the httpboot plugin, got %d", len(args))
	}
	return url.Parse(args[0])
}

func setup6(args ...string) (handler.Handler6, error) {
	u, err := parseArgs(args...)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL provided: %v", err)
	}
	bootFile6 = u.String()
	log.Printf("Configured httpboot plugin with URL: %s", bootFile6)
	return Handler6, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	u, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	bootFile4 = u.String()
	log.Printf("loaded httpboot plugin for DHCPv4.")
	return Handler4, nil
}

func Handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

	clientIP, err := extractClientIP6(req)
	if err != nil {
		log.Errorf("failed to extract the ClientIP, Error: %v Request: %v ", err, req)
		return resp, true
	}

	ukiURL, err := fetchUKIURL(bootFile6, clientIP)
	if err != nil {
		log.Errorf("failed to fetch UKI URL: %v", err)
		return resp, true
	}

	decap, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("could not decapsulate request: %v", err)
		return nil, true
	}

	if decap.GetOneOption(dhcpv6.OptionVendorClass) != nil {
		vc := decap.GetOneOption(dhcpv6.OptionVendorClass).String()
		if strings.Contains(vc, "HTTPClient") {
			bf := &dhcpv6.OptionGeneric{
				OptionCode: dhcpv6.OptionBootfileURL,
				OptionData: []byte(ukiURL),
			}
			resp.AddOption(bf)
			vid := &dhcpv6.OptionGeneric{
				OptionCode: dhcpv6.OptionVendorClass,
				// 0000 (4 bytes) Enterprise Identifier
				// 0a (2 bytes) length of "HTTPClient"
				// - rest with HTTPClient
				OptionData: []byte("00000aHTTPClient"),
			}
			resp.AddOption(vid)
		}
	}

	return resp, false
}

func Handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	log.Debugf("Received DHCPv4 request: %s", req.Summary())

	ukiURL, err := fetchUKIURL(bootFile4, req.ClientIPAddr)
	if err != nil {
		log.Errorf("failed to fetch UKI URL: %v", err)
		return resp, true
	}

	if req.GetOneOption(dhcpv4.OptionClassIdentifier) != nil {
		vc := req.GetOneOption(dhcpv4.OptionClassIdentifier)
		if strings.Contains(string(vc), "HTTPClient") {
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

func extractClientIP6(req dhcpv6.DHCPv6) (net.IP, error) {
	if req.IsRelay() {
		relayMsg, ok := req.(*dhcpv6.RelayMessage)
		if !ok {
			return nil, fmt.Errorf("failed to cast the DHCPv6 request to a RelayMessage")
		}
		return relayMsg.LinkAddr, nil
	}
	return nil, fmt.Errorf("received non-relay DHCPv6 request, client IP cannot be extracted from non-relayed messages")
}

func fetchUKIURL(url string, clientIP net.IP) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Forwarded-For", clientIP.String())

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
