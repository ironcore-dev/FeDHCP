// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ntp

import (
	"net"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ironcore-dev/fedhcp/internal/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	pollingInterval       = 50 * time.Millisecond
	eventuallyTimeout     = 3 * time.Second
	consistentlyDuration  = 1 * time.Second
	ntpConfigFile         = "config.yaml"
	ntpServerIPV4Address1 = "192.0.2.1"
	ntpServerIPV4Address2 = "192.0.2.2"
	ntpServerIPV6Address1 = "2001:db8::1"
	ntpServerIPV6Address2 = "2001:db8::2"
	relayIPV6Address      = "2001:db8:1::1"
	linkLocalIPV6Address  = "fe80::1322:33ff:fe44:5566"
	clientMACAddress      = "11:22:33:44:55:66"
)

var (
	ntpServersV4 = []net.IP{net.ParseIP(ntpServerIPV4Address1), net.ParseIP(ntpServerIPV4Address2)}
	ntpServersV6 = []net.IP{net.ParseIP(ntpServerIPV6Address1), net.ParseIP(ntpServerIPV6Address2)}
)

func TestNTP(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "NTP Plugin Suite")
}

func SetupTest() {
	BeforeEach(func() {
		configFile := writeConfig(api.NTPConfig{
			Servers:   ntpServersV4,
			ServersV6: ntpServersV6,
		})

		h6, err := setup6(configFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())

		h4, err := setup4(configFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(h4).NotTo(BeNil())

		Expect(ntpConfig.Servers).To(Equal(ntpServersV4))
		Expect(ntpConfig.ServersV6).To(Equal(ntpServersV6))
	})
}

func writeConfig(config api.NTPConfig) string {
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	return writeConfigData(configData)
}

func writeConfigData(configData []byte) string {
	file, err := os.CreateTemp(GinkgoT().TempDir(), ntpConfigFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	return file.Name()
}
