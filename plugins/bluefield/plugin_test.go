// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package bluefield

import (
	"net"
	"os"
	"time"

	"github.com/insomniacslk/dhcp/iana"

	"github.com/insomniacslk/dhcp/dhcpv6"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bluefield Plugin", func() {
	var (
		testConfigPath string
		testIP         string

		err error
	)

	BeforeEach(func() {
		// Setup temporary test config file
		testConfigPath = "test_config.yaml"
		testIP = "2001:db8::1"
		configData := `bluefieldIP: 2001:db8::1`
		err = os.WriteFile(testConfigPath, []byte(configData), 0644)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Cleanup temporary config file
		err = os.Remove(testConfigPath)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Configuration Loading", func() {
		It("should successfully load a valid configuration file", func() {
			config, err := loadConfig(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.BluefieldIP).To(Equal(testIP))
		})

		It("should return an error if the configuration file is missing", func() {
			_, err := loadConfig("nonexistent.yaml")
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the configuration file is invalid", func() {
			err = os.WriteFile(testConfigPath, []byte("Invalid YAML"), 0644)
			Expect(err).NotTo(HaveOccurred())
			_, err = loadConfig(testConfigPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Plugin Setup", func() {
		It("should successfully initialize the plugin with a valid config", func() {
			handler, err := setupPlugin(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should return an error for invalid IP addresses in the config", func() {
			invalidConfigData := `bluefieldIP: "invalid-ip"`
			err = os.WriteFile(testConfigPath, []byte(invalidConfigData), 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = setupPlugin(testConfigPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid IPv6 address"))
		})
	})

	Describe("DHCPv6 Message Handling", func() {

		BeforeEach(func() {
			handler, err := setupPlugin(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		Context("when handling Solicit messages", func() {
			It("should respond with an Advertise message", func() {
				resp, stop := handleDHCPv6(createSolicitMessage(), nil)
				Expect(stop).To(BeFalse())

				respm, err := resp.GetInnerMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeAdvertise))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr.String()).To(Equal(testIP))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(Equal(24 * time.Hour))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(Equal(48 * time.Hour))
				Expect(respm.Options.OneIANA().T1).To(Equal(1 * time.Hour))
				Expect(respm.Options.OneIANA().T2).To(Equal(2 * time.Hour))
				Expect(respm.Options.OneIANA().IaId).NotTo(BeNil())
			})
		})

		Context("when handling Request messages", func() {
			It("should respond with a Reply message", func() {
				resp, stop := handleDHCPv6(createRequestMessage(), nil)
				Expect(stop).To(BeFalse())

				respm, err := resp.GetInnerMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeReply))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr.String()).To(Equal(testIP))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(Equal(24 * time.Hour))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(Equal(48 * time.Hour))
				Expect(respm.Options.OneIANA().T1).To(Equal(1 * time.Hour))
				Expect(respm.Options.OneIANA().T2).To(Equal(2 * time.Hour))
				Expect(respm.Options.OneIANA().IaId).NotTo(BeNil())
			})
		})

		Context("when handling Release messages", func() {
			It("should respond with SUCCESS for the right IANA address", func() {
				resp, stop := handleDHCPv6(createRequestMessage(), nil)
				Expect(stop).To(BeFalse())

				respm, err := resp.GetInnerMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeReply))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr.String()).To(Equal(testIP))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(Equal(24 * time.Hour))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(Equal(48 * time.Hour))
				Expect(respm.Options.OneIANA().T1).To(Equal(1 * time.Hour))
				Expect(respm.Options.OneIANA().T2).To(Equal(2 * time.Hour))
				Expect(respm.Options.OneIANA().IaId).NotTo(BeNil())

				resp, stop = handleDHCPv6(createReleaseMessage("2001:db8::1"), nil)
				Expect(stop).To(BeTrue())

				respm, err = resp.GetInnerMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeReply))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr).To(BeNil())
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(BeZero())
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(BeZero())
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).Options.Options[0].(*dhcpv6.OptStatusCode).StatusCode).To(Equal(iana.StatusSuccess))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).Options.Options[0].(*dhcpv6.OptStatusCode).StatusMessage).To(Equal("Successfully released address"))
				Expect(respm.Options.OneIANA().T1).To(BeZero())
				Expect(respm.Options.OneIANA().T2).To(BeZero())
				Expect(respm.Options.OneIANA().IaId).NotTo(BeNil())
			})

			It("should respond with NoBinding for the wrong IANA address", func() {
				resp, stop := handleDHCPv6(createRequestMessage(), nil)
				Expect(stop).To(BeFalse())

				respm, err := resp.GetInnerMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeReply))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr.String()).To(Equal(testIP))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(Equal(24 * time.Hour))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(Equal(48 * time.Hour))
				Expect(respm.Options.OneIANA().T1).To(Equal(1 * time.Hour))
				Expect(respm.Options.OneIANA().T2).To(Equal(2 * time.Hour))
				Expect(respm.Options.OneIANA().IaId).NotTo(BeNil())

				resp, stop = handleDHCPv6(createReleaseMessage("2001:db8::ca:fe"), nil)
				Expect(stop).To(BeTrue())

				respm, err = resp.GetInnerMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeReply))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr).To(BeNil())
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(BeZero())
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(BeZero())
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).Options.Options[0].(*dhcpv6.OptStatusCode).StatusCode).To(Equal(iana.StatusNoBinding))
				Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).Options.Options[0].(*dhcpv6.OptStatusCode).StatusMessage).To(Equal("No binding for address"))
				Expect(respm.Options.OneIANA().T1).To(BeZero())
				Expect(respm.Options.OneIANA().T2).To(BeZero())
				Expect(respm.Options.OneIANA().IaId).NotTo(BeNil())
			})
		})

		Context("when handling unsupported message types", func() {
			It("should return nil for unsupported types", func() {
				resp, stop := handleDHCPv6(createUnsupportedMessage(), nil)

				Expect(resp).To(BeNil())
				Expect(stop).To(BeFalse())
			})
		})
	})
})

func createSolicitMessage() dhcpv6.DHCPv6 {
	hwaddr, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		return nil
	}
	req, _ := dhcpv6.NewSolicit(hwaddr)
	req.MessageType = dhcpv6.MessageTypeSolicit
	return req
}

func createRequestMessage() dhcpv6.DHCPv6 {
	hwaddr, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		return nil
	}
	req, _ := dhcpv6.NewSolicit(hwaddr)
	req.MessageType = dhcpv6.MessageTypeRequest
	return req
}

func createReleaseMessage(ipAddress string) dhcpv6.DHCPv6 {
	hwaddr, _ := net.ParseMAC("00:11:22:33:44:55")
	clientID := &dhcpv6.DUIDLL{
		HWType:        iana.HWTypeEthernet,
		LinkLayerAddr: hwaddr,
	}

	msg := &dhcpv6.Message{
		TransactionID: dhcpv6.TransactionID{0xa, 0xb, 0xc},
		MessageType:   dhcpv6.MessageTypeRelease,
	}
	msg.AddOption(dhcpv6.OptClientID(clientID))
	msg.AddOption(&dhcpv6.OptIANA{
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          net.ParseIP(ipAddress),
				PreferredLifetime: 24 * time.Hour,
				ValidLifetime:     48 * time.Hour,
			},
		}},
	})

	return msg
}

func createUnsupportedMessage() dhcpv6.DHCPv6 {
	hwaddr, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		return nil
	}
	req, _ := dhcpv6.NewSolicit(hwaddr)
	req.MessageType = dhcpv6.MessageTypeDecline
	return req
}
