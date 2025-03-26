// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"net"
	"os"

	"github.com/insomniacslk/dhcp/dhcpv6"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ZTP Plugin", func() {
	Describe("Configuration Loading", func() {
		It("should return an error if the configuration file is missing", func() {
			_, err := loadConfig("nonexistent.yaml")
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the configuration file is invalid", func() {
			invalidConfigPath := "invalid_test_config.yaml"

			file, err := os.CreateTemp(GinkgoT().TempDir(), invalidConfigPath)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = file.Close()
			}()
			Expect(os.WriteFile(file.Name(), []byte("Invalid YAML"), 0644)).To(Succeed())

			_, err = loadConfig(file.Name())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DHCPv6 Message Handling", func() {
		It("should return ZTP option 239", func() {
			req := createRequest("11:22:33:44:55:66")
			stub, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			stub.MessageType = dhcpv6.MessageTypeReply
			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opt := resp.GetOneOption(optionZTPCode).(*dhcpv6.OptionGeneric)
			Expect(opt).NotTo(BeNil())
			Expect(int(opt.OptionCode)).To(Equal(optionZTPCode))
			Expect(opt.OptionData).To(Equal([]byte(provisioningScript)))
		})
	})
})

func createRequest(mac string) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())
	Expect(hwAddr).NotTo(BeNil())

	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	Expect(req).NotTo(BeNil())

	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(optionZTPCode))
	return req
}
