// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"net"
	"os"
	"strings"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/helper"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("OOB Plugin", func() {
	ns := SetupTest()

	It("Setup6 should return error if less arguments are provided", func() {
		_, err := setup6()
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if more arguments are provided", func() {
		_, err := setup6("foo", "bar")
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if config file does not exist", func() {
		_, err := setup6("does-not-exist.yaml")
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if config file is invalid", func() {
		_, err := setup6(writeInvalidConfig())
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if less arguments are provided", func() {
		_, err := setup4()
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if more arguments are provided", func() {
		_, err := setup4("foo", "bar")
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if config file does not exist", func() {
		_, err := setup4("does-not-exist.yaml")
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if config file is invalid", func() {
		_, err := setup4(writeInvalidConfig())
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return a non-nil handler for an empty subnet labels config", func() {
		h6, err := setup6(writeConfig(api.OOBConfig{
			Namespace:    ns.Name,
			SubnetLabels: []api.SubnetLabel{},
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())
	})

	It("Setup4 should return a non-nil handler for an empty subnet labels config", func() {
		h4, err := setup4(writeConfig(api.OOBConfig{
			Namespace:    ns.Name,
			SubnetLabels: []api.SubnetLabel{},
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h4).NotTo(BeNil())
	})

	It("Should return a valid config for a valid config file", func() {
		config, err := loadConfig(writeConfig(api.OOBConfig{
			Namespace:    ns.Name,
			SubnetLabels: oobSubnetLabels,
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Namespace).To(Equal(ns.Name))
		Expect(config.SubnetLabels).To(Equal(oobSubnetLabels))
	})

	It("Should return and break plugin chain, if getting a nil IPv6 DHCP request", func() {
		stub, _ := dhcpv6.NewMessage()
		resp, breakChain := handler6(nil, stub)

		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())
	})

	It("Should return and break plugin chain, if getting an IPv6 DHCP request directly (no relay)", func() {
		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		resp, breakChain := handler6(req, stub)

		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())
	})

	It("Should return and break plugin chain without creating an IP, if the relayed request cannot be decapsulated", func() {
		// relay message without an encapsulated message
		relayedRequest := &dhcpv6.RelayMessage{
			MessageType: dhcpv6.MessageTypeRelayForward,
			LinkAddr:    net.ParseIP(relayIPV6Address),
			PeerAddr:    linkLocalAddress(clientMACAddress),
		}

		resp, breakChain := handler6(relayedRequest, newReply6())
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Consistently(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})

	It("Should create an IP and lease it for a relayed IPv6 DHCP request", func() {
		relayedRequest := newRelayedRequest(relayIPV6Address, clientMACAddress, true)

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())

		expectedIP := eui64Address(subnetPrefix(oobSubnetV6CIDR), clientMACAddress)
		leasedIP, lifetime := helper.GetIANAAddressAndLifetime(resp)
		Expect(leasedIP.String()).To(Equal(expectedIP.String()))
		Expect(lifetime).To(Equal(validLifeTime))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", HavePrefix(macKey(clientMACAddress)+"-"+origin+"-")),
				HaveField("ObjectMeta.Labels", Equal(expectedIPLabels())),
				HaveField("Spec.Subnet.Name", oobSubnetV6Name),
				HaveField("Status.State", ipamv1alpha1.FinishedIPState),
				HaveField("Status.Reserved.Net.String()", expectedIP.String()),
			))),
		))
	})

	It("Should create an IP, but not add an IANA option, if no address is requested", func() {
		relayedRequest := newRelayedRequest(relayIPV6Address, clientMACAddress, false)

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())

		leasedIP, _ := helper.GetIANAAddressAndLifetime(resp)
		Expect(leasedIP).To(BeNil())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(
			HaveField("Items", HaveLen(1)))
	})

	It("Should create an IP for the MAC from the ClientLinkLayer option (RFC6939)", func() {
		relayedRequest := newRelayedRequest(relayIPV6Address, otherMACAddress, true)
		clientMAC, _ := net.ParseMAC(clientMACAddress)
		relayedRequest.AddOption(dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, clientMAC))

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())

		leasedIP, _ := helper.GetIANAAddressAndLifetime(resp)
		Expect(leasedIP.String()).To(Equal(eui64Address(subnetPrefix(oobSubnetV6CIDR), clientMACAddress).String()))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(
			HaveField("Items", HaveLen(1)))
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(otherMACAddress))).Should(
			HaveField("Items", BeEmpty()))
	})

	It("Should reuse an existing IP and apply the subnet labels for a relayed IPv6 DHCP request", func(ctx SpecContext) {
		existingIP := createIP(ctx, ns.Name, oobSubnetV6Name, "2001:db8::4711", ipamv1alpha1.FinishedIPState)

		relayedRequest := newRelayedRequest(relayIPV6Address, clientMACAddress, true)
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())

		leasedIP, _ := helper.GetIANAAddressAndLifetime(resp)
		Expect(leasedIP.String()).To(Equal("2001:db8::4711"))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", existingIP.Name),
				HaveField("ObjectMeta.Labels", HaveKeyWithValue("subnet", "oob")),
				HaveField("ObjectMeta.Labels", HaveKeyWithValue("foo", "bar")),
			))),
		))
	})

	It("Should delete a failed IP and create a new one for a relayed IPv6 DHCP request", func(ctx SpecContext) {
		failedIP := createIP(ctx, ns.Name, oobSubnetV6Name, "2001:db8::4711", ipamv1alpha1.FailedIPState)

		relayedRequest := newRelayedRequest(relayIPV6Address, clientMACAddress, true)
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())

		expectedIP := eui64Address(subnetPrefix(oobSubnetV6CIDR), clientMACAddress)
		leasedIP, _ := helper.GetIANAAddressAndLifetime(resp)
		Expect(leasedIP.String()).To(Equal(expectedIP.String()))

		Eventually(Get(failedIP)).Should(Satisfy(apierrors.IsNotFound))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", Not(Equal(failedIP.Name))),
				HaveField("ObjectMeta.Labels", Equal(expectedIPLabels())),
				HaveField("Status.State", ipamv1alpha1.FinishedIPState),
				HaveField("Status.Reserved.Net.String()", expectedIP.String()),
			))),
		))
	})

	It("Should ignore an existing IP of the same MAC in another subnet for a relayed IPv6 DHCP request", func(ctx SpecContext) {
		ipv4 := createIP(ctx, ns.Name, oobSubnetV4Name, privateIPV4Address, ipamv1alpha1.FinishedIPState)

		relayedRequest := newRelayedRequest(relayIPV6Address, clientMACAddress, true)
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())

		leasedIP, _ := helper.GetIANAAddressAndLifetime(resp)
		Expect(leasedIP.String()).To(Equal(eui64Address(subnetPrefix(oobSubnetV6CIDR), clientMACAddress).String()))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(2)),
			HaveField("Items", ContainElement(HaveField("ObjectMeta.Name", ipv4.Name))),
			HaveField("Items", ContainElement(HaveField("Spec.Subnet.Name", oobSubnetV6Name))),
		))
	})

	It("Should return and break plugin chain, if the relay is not in an OOB subnet", func() {
		relayedRequest := newRelayedRequest(unknownIPV6Address, clientMACAddress, true)

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})

	It("Should return and break plugin chain, if the relay is in a subnet not matching the subnet labels", func() {
		relayedRequest := newRelayedRequest(subnetPrefix(foreignSubnetV6CIDR)+"1", clientMACAddress, true)

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})

	It("Should return and break plugin chain, if getting a nil IPv4 DHCP request", func() {
		stub, _ := dhcpv4.New()
		resp, breakChain := handler4(nil, stub)

		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())
	})

	It("Should create an IP and lease it for an IPv4 DHCP discover", func() {
		mac, _ := net.ParseMAC(clientMACAddress)
		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())
		// first free address of the subnet, assigned by the (fake) IPAM
		Expect(resp.YourIPAddr.String()).To(Equal("192.168.1.1"))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Labels", Equal(expectedIPLabels())),
				HaveField("Spec.Subnet.Name", oobSubnetV4Name),
				HaveField("Spec.IP", BeNil()),
				HaveField("Status.State", ipamv1alpha1.FinishedIPState),
			))),
		))
	})

	It("Should create an IP with the client IP address for an IPv4 DHCP request", func() {
		mac, _ := net.ParseMAC(clientMACAddress)
		req, _ := dhcpv4.New(
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
			dhcpv4.WithClientIP(net.ParseIP(privateIPV4Address)),
		)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())
		Expect(resp.YourIPAddr.String()).To(Equal(privateIPV4Address))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("Spec.Subnet.Name", oobSubnetV4Name),
				HaveField("Spec.IP.Net.String()", privateIPV4Address),
				HaveField("Status.Reserved.Net.String()", privateIPV4Address),
			))),
		))
	})

	It("Should reuse an existing IP and apply the subnet labels for an IPv4 DHCP request", func(ctx SpecContext) {
		existingIP := createIP(ctx, ns.Name, oobSubnetV4Name, privateIPV4Address, ipamv1alpha1.FinishedIPState)

		mac, _ := net.ParseMAC(clientMACAddress)
		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())
		Expect(resp.YourIPAddr.String()).To(Equal(privateIPV4Address))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", existingIP.Name),
				HaveField("ObjectMeta.Labels", HaveKeyWithValue("subnet", "oob")),
				HaveField("ObjectMeta.Labels", HaveKeyWithValue("foo", "bar")),
			))),
		))
	})

	It("Should delete a failed IP and create a new one for an IPv4 DHCP request", func(ctx SpecContext) {
		failedIP := createIP(ctx, ns.Name, oobSubnetV4Name, privateIPV4Address, ipamv1alpha1.FailedIPState)

		mac, _ := net.ParseMAC(clientMACAddress)
		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).NotTo(BeNil())
		Expect(resp.YourIPAddr.String()).To(Equal("192.168.1.1"))

		Eventually(Get(failedIP)).Should(Satisfy(apierrors.IsNotFound))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel(clientMACAddress))).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", Not(Equal(failedIP.Name))),
				HaveField("ObjectMeta.Labels", Equal(expectedIPLabels())),
				HaveField("Status.State", ipamv1alpha1.FinishedIPState),
			))),
		))
	})

	It("Should return and break plugin chain, if the IPv4 client IP address is not in an OOB subnet", func() {
		mac, _ := net.ParseMAC(clientMACAddress)
		req, _ := dhcpv4.New(
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
			dhcpv4.WithClientIP(net.ParseIP(unknownIPV4Address)),
		)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})
})

func writeInvalidConfig() string {
	file, err := os.CreateTemp(GinkgoT().TempDir(), oobConfigFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), []byte("Invalid YAML"), 0644)).To(Succeed())

	return file.Name()
}

func newRelayedRequest(linkAddr string, peerMAC string, withIANA bool) *dhcpv6.RelayMessage {
	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest
	if withIANA {
		req.AddOption(&dhcpv6.OptIANA{IaId: [4]byte{1, 2, 3, 4}})
	}

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP(linkAddr), linkLocalAddress(peerMAC))
	Expect(err).NotTo(HaveOccurred())

	return relayedRequest
}

func newReply6() *dhcpv6.Message {
	stub, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	stub.MessageType = dhcpv6.MessageTypeReply
	return stub
}

// createIP creates an IP for clientMACAddress with the given reserved address and state, but without subnet labels.
func createIP(ctx context.Context, namespace, subnetName, address string, state ipamv1alpha1.IPState) *ipamv1alpha1.IP {
	addr, err := ipamv1alpha1.IPAddrFromString(address)
	Expect(err).NotTo(HaveOccurred())

	ip := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "existing-ip-",
			Labels: map[string]string{
				"mac": macKey(clientMACAddress),
			},
		},
		Spec: ipamv1alpha1.IPSpec{
			Subnet: corev1.LocalObjectReference{
				Name: subnetName,
			},
			IP: addr,
		},
	}
	Expect(k8sClientTest.Create(ctx, ip)).To(Succeed())

	Eventually(UpdateStatus(ip, func() {
		ip.Status.Reserved = addr
		ip.Status.State = state
	})).Should(Succeed())
	Eventually(Object(ip)).Should(HaveField("Status.State", state))

	return ip
}

func macKey(mac string) string {
	return strings.ReplaceAll(mac, ":", "")
}

func macLabel(mac string) client.MatchingLabels {
	return client.MatchingLabels{"mac": macKey(mac)}
}

// expectedIPLabels returns the labels of an IP created by the plugin for clientMACAddress.
func expectedIPLabels() map[string]string {
	labels := subnetLabelsMap(oobSubnetLabels)
	labels["mac"] = macKey(clientMACAddress)
	labels["origin"] = origin
	return labels
}

// subnetPrefix returns the network address of a CIDR, e.g. "2001:db8::" for "2001:db8::/64".
func subnetPrefix(cidr string) string {
	ip, _, err := net.ParseCIDR(cidr)
	Expect(err).NotTo(HaveOccurred())
	return ip.String()
}
