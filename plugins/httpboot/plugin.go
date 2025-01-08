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
	"os"
	"strings"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v2"
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

const httpClient = "HTTPClient"

func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the httpboot plugin, got %d", len(args))
	}
	return args[0], nil
}

func parseConfig(args ...string) (*url.URL, bool, error) {
	httpbootConfig, err := loadConfig(args...)
	if err != nil {
		return nil, false, fmt.Errorf("erorr loading httpboot plugin config: %v", err)
	}
	arg := httpbootConfig.BootFile
	parsedURL, err := url.Parse(arg)
	if err != nil {
		return nil, false, fmt.Errorf("invalid URL: %v", err)
	}
	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.Path == "" {
		return nil, false, fmt.Errorf("malformed httpboot parameter, should be a valid HTTP(s) URL")
	}
	return parsedURL, httpbootConfig.ClientSpecific, nil
}

func loadConfig(args ...string) (*api.HttpBootConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading httpboot config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}
	config := &api.HttpBootConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	u, ubs, err := parseConfig(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}
	bootFile6 = u.String()
	useBootService = ubs
	log.Printf("Configured httpboot plugin with URL: %s, useBootService: %t", bootFile6, useBootService)
	return handler6, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	u, ubs, err := parseConfig(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}
	bootFile4 = u.String()
	useBootService = ubs
	log.Printf("Configured httpboot plugin with URL: %s, useBootService: %t", bootFile4, useBootService)
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
		if len(vcc) >= 16 && binary.BigEndian.Uint16(vcc[4:6]) >= 10 && string(vcc[6:16]) == httpClient {
			bf := dhcpv6.OptBootFileURL(ukiURL)
			resp.AddOption(bf)
			log.Infof("Added option BootFileURL(%d): (%s)", dhcpv6.OptionBootfileURL, ukiURL)

			buf := []byte(httpClient)
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

	var ukiURL string
	var err error
	if !useBootService {
		ukiURL = bootFile4
	} else {
		ukiURL, err = fetchUKIURL(bootFile4, []string{req.ClientIPAddr.String()})
		if err != nil {
			log.Errorf("failed to fetch UKI URL: %v", err)
			return resp, false
		}
	}

	if req.GetOneOption(dhcpv4.OptionClassIdentifier) != nil {
		cic := req.GetOneOption(dhcpv4.OptionClassIdentifier)
		log.Debugf("ClassIdentifier: %s (%s)", string(cic), cic)
		if len(cic) >= 10 && string(cic[0:10]) == httpClient {
			bf := &dhcpv4.Option{
				Code:  dhcpv4.OptionBootfileName,
				Value: dhcpv4.String(ukiURL),
			}
			resp.Options.Update(*bf)
			log.Infof("Added option BooFileName %s", bf.String())

			ci := &dhcpv4.Option{
				Code:  dhcpv4.OptionClassIdentifier,
				Value: dhcpv4.String(httpClient),
			}
			resp.Options.Update(*ci)
			log.Infof("Added option ClassIdentifier %s", ci.String())
		} else {
			log.Errorf("non HTTPClient ClassIdentifier %s", string(cic))
			return resp, false
		}
	}
	log.Debugf("Sent DHCPv4 response: %s", resp.Summary())
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
	defer func() {
		_ = resp.Body.Close()
	}()

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
