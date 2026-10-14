// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"context"
	"net"
	"os"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("IPAM Plugin", func() {
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

	It("Setup6 should return a non-nil handler for an empty config", func() {
		h6, err := setup6(writeConfig(api.IPAMConfig{}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())
	})

	It("Should return a valid config for a valid config file", func() {
		config, err := loadConfig(writeConfig(api.IPAMConfig{
			Namespace: ns.Name,
			Subnets:   ipamSubnetNames,
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Namespace).To(Equal(ns.Name))
		Expect(config.Subnets).To(Equal(ipamSubnetNames))
	})

	It("Should return and break plugin chain, if getting a nil IPv6 DHCP request", func() {
		stub := newReply6()
		resp, breakChain := handler6(nil, stub)

		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())
	})

	It("Should return and break plugin chain, if getting an IPv6 DHCP request directly (no relay)", func() {
		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest

		stub := newReply6()
		resp, breakChain := handler6(req, stub)

		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())
	})

	It("Should return and break plugin chain, if the peer address is not an IPv6 address", func() {
		relayedRequest := newRelayedRequest(relayIPV6Address1, net.ParseIP("192.0.2.1"))

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})

	It("Should create an IP for a relayed IPv6 DHCP request", func() {
		relayedRequest := newRelayedRequest(relayIPV6Address1, clientLinkLocalAddress())

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(stub.Options.OneIANA()).To(BeNil())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", HavePrefix(clientMACKey+"-"+origin+"-")),
				HaveField("ObjectMeta.Labels", Equal(expectedIPLabels())),
				HaveField("Spec.Subnet.Name", ipamSubnet1Name),
				// the IP following the relay link address
				HaveField("Spec.IP.Net.String()", "2001:db8:1::11"),
				HaveField("Status.State", ipamv1alpha1.FinishedIPState),
				HaveField("Status.Reserved.Net.String()", "2001:db8:1::11"),
			))),
		))
	})

	It("Should create an IP in the configured subnet matching the relay link address", func() {
		relayedRequest := newRelayedRequest(relayIPV6Address2, clientLinkLocalAddress())

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("Spec.Subnet.Name", ipamSubnet2Name),
				HaveField("Spec.IP.Net.String()", "2001:db8:2::11"),
			))),
		))
	})

	It("Should skip configured subnets which do not exist or have no CIDR reserved", func(ctx SpecContext) {
		createSubnet(ctx, ns.Name, "pending-subnet", "")
		h6, err := setup6(writeConfig(api.IPAMConfig{
			Namespace: ns.Name,
			Subnets:   []string{"does-not-exist", "pending-subnet", ipamSubnet1Name},
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())

		relayedRequest := newRelayedRequest(relayIPV6Address1, clientLinkLocalAddress())
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(HaveField("Spec.Subnet.Name", ipamSubnet1Name))),
		))
	})

	It("Should reuse an existing IP for a relayed IPv6 DHCP request", func(ctx SpecContext) {
		existingIP := createIP(ctx, ns.Name, ipamSubnet1Name, "2001:db8:1::4711", ipamv1alpha1.FinishedIPState)

		relayedRequest := newRelayedRequest(relayIPV6Address1, clientLinkLocalAddress())
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", existingIP.Name),
				HaveField("Spec.IP.Net.String()", "2001:db8:1::4711"),
			))),
		))
	})

	It("Should delete a failed IP and create a new one for a relayed IPv6 DHCP request", func(ctx SpecContext) {
		failedIP := createIP(ctx, ns.Name, ipamSubnet1Name, "2001:db8:1::4711", ipamv1alpha1.FailedIPState)

		relayedRequest := newRelayedRequest(relayIPV6Address1, clientLinkLocalAddress())
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))

		Eventually(Get(failedIP)).Should(Satisfy(apierrors.IsNotFound))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", Not(Equal(failedIP.Name))),
				HaveField("ObjectMeta.Labels", Equal(expectedIPLabels())),
				HaveField("Spec.IP.Net.String()", "2001:db8:1::11"),
				HaveField("Status.State", ipamv1alpha1.FinishedIPState),
			))),
		))
	})

	It("Should ignore an existing IP of the same MAC in another subnet for a relayed IPv6 DHCP request", func(ctx SpecContext) {
		otherIP := createIP(ctx, ns.Name, ipamSubnet2Name, "2001:db8:2::4711", ipamv1alpha1.FinishedIPState)

		relayedRequest := newRelayedRequest(relayIPV6Address1, clientLinkLocalAddress())
		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(2)),
			HaveField("Items", ContainElement(HaveField("ObjectMeta.Name", otherIP.Name))),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("Spec.Subnet.Name", ipamSubnet1Name),
				HaveField("Spec.IP.Net.String()", "2001:db8:1::11"),
			))),
		))
	})

	It("Should return and break plugin chain, if IPAM does not reserve the IP in time, without duplicating it on retry", func() {
		disableFakeIPAM(ns.Name)

		relayedRequest := newRelayedRequest(relayIPV6Address1, clientLinkLocalAddress())
		resp, breakChain := handler6(relayedRequest, newReply6())
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		// the pending IP is reused instead of creating another one
		_, _ = handler6(relayedRequest, newReply6())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name), macLabel())).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("Spec.IP.Net.String()", "2001:db8:1::11"),
				HaveField("Status.Reserved", BeNil()),
			))),
		))
	})

	It("Should return and break plugin chain, if the relay is not in any subnet", func() {
		relayedRequest := newRelayedRequest(unknownIPV6Address, clientLinkLocalAddress())

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})

	It("Should return and break plugin chain, if the relay is in a subnet which is not configured", func() {
		relayedRequest := newRelayedRequest("2001:db8:ff::10", clientLinkLocalAddress())

		stub := newReply6()
		resp, breakChain := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(breakChain).To(BeTrue())

		ipList := &ipamv1alpha1.IPList{}
		Eventually(ObjectList(ipList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})
})

func writeInvalidConfig() string {
	file, err := os.CreateTemp(GinkgoT().TempDir(), ipamConfigFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), []byte("Invalid YAML"), 0644)).To(Succeed())

	return file.Name()
}

func newRelayedRequest(linkAddr string, peerAddr net.IP) *dhcpv6.RelayMessage {
	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP(linkAddr), peerAddr)
	Expect(err).NotTo(HaveOccurred())

	return relayedRequest
}

func newReply6() *dhcpv6.Message {
	stub, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	stub.MessageType = dhcpv6.MessageTypeReply
	return stub
}

// createIP creates an IP for clientMACAddress with the given reserved address and state.
func createIP(ctx context.Context, namespace, subnetName, address string, state ipamv1alpha1.IPState) *ipamv1alpha1.IP {
	addr, err := ipamv1alpha1.IPAddrFromString(address)
	Expect(err).NotTo(HaveOccurred())

	ip := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "existing-ip-",
			Labels: map[string]string{
				macLabelKey: clientMACKey,
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

func macLabel() client.MatchingLabels {
	return client.MatchingLabels{macLabelKey: clientMACKey}
}

// expectedIPLabels returns the labels of an IP created by the plugin for clientMACAddress.
func expectedIPLabels() map[string]string {
	return map[string]string{
		macLabelKey: clientMACKey,
		"origin":    origin,
	}
}
