// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package leases

import (
	"net"
	"os"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	fedhcpv1alpha1 "github.com/ironcore-dev/fedhcp/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("Leases", func() {
	ns := SetupTest()

	It("Setup6 should return error if no arguments are provided", func() {
		_, err := setup6()
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if too many arguments are provided", func() {
		_, err := setup6("foo", "bar")
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if config file does not exist", func() {
		_, err := setup6("does-not-exist.yaml")
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if config file is invalid", func() {
		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), []byte("Invalid YAML"), 0644)).To(Succeed())

		_, err = setup6(file.Name())
		Expect(err).To(HaveOccurred())
	})

	It("Should drop the request, if the lease cannot be recorded", func(ctx SpecContext) {
		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), []byte("namespace: does-not-exist\n"), 0644)).To(Succeed())
		_, err = setup6(file.Name())
		Expect(err).NotTo(HaveOccurred())

		// MAC aa:bb:cc:dd:ee:ff -> EUI-64: a8:bb:cc:ff:fe:dd:ee:ff
		relayedRequest := newRelayedRequest(net.ParseIP("fe80::a8bb:ccff:fedd:eeff"))
		stub := newReplyWithIANA(net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff"))

		resp, stop := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(stop).To(BeTrue())
	})

	It("Should drop the request, if the client MAC cannot be determined", func(ctx SpecContext) {
		relayedRequest := newRelayedRequest(net.ParseIP("192.0.2.1"))
		stub := newReplyWithIANA(net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff"))

		resp, stop := handler6(relayedRequest, stub)
		Expect(resp).To(BeNil())
		Expect(stop).To(BeTrue())

		leaseList := &fedhcpv1alpha1.LeaseList{}
		Eventually(ObjectList(leaseList, client.InNamespace(ns.Name))).Should(HaveField("Items", BeEmpty()))
	})

	It("Should create a lease for a valid relay request with IANA in response", func(ctx SpecContext) {
		req, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		req.MessageType = dhcpv6.MessageTypeRequest

		// MAC aa:bb:cc:dd:ee:ff -> EUI-64: a8:bb:cc:ff:fe:dd:ee:ff
		peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
		linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")

		relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
		Expect(err).NotTo(HaveOccurred())

		leasedIP := net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff")

		stub, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		stub.MessageType = dhcpv6.MessageTypeReply
		stub.AddOption(&dhcpv6.OptIANA{
			IaId: [4]byte{1, 2, 3, 4},
			Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
				&dhcpv6.OptIAAddress{
					IPv6Addr:          leasedIP,
					PreferredLifetime: 24 * time.Hour,
					ValidLifetime:     24 * time.Hour,
				},
			}},
		})

		resp, stop := handler6(relayedRequest, stub)
		Expect(resp).NotTo(BeNil())
		Expect(stop).To(BeFalse())

		expectedName := "2001-0db8-1111-2222-3333-aabb-ccdd-eeff"
		lease := &fedhcpv1alpha1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedName,
				Namespace: ns.Name,
			},
		}

		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MAC", "aa:bb:cc:dd:ee:ff"),
			HaveField("Spec.IP", leasedIP.String()),
			HaveField("Spec.FirstSeen.IsZero()", BeFalse()),
			HaveField("Spec.Renewed.IsZero()", BeFalse()),
			HaveField("Spec.ExpiresAt.IsZero()", BeFalse()),
		))

		DeferCleanup(testK8sClient.Delete, lease)
	})

	It("Should preserve FirstSeen on lease renewal", func(ctx SpecContext) {
		leasedIP := net.ParseIP("2001:db8:aaaa:bbbb:cccc:1122:3344:5566")
		expectedName := "2001-0db8-aaaa-bbbb-cccc-1122-3344-5566"
		originalFirstSeen := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

		existingLease := &fedhcpv1alpha1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedName,
				Namespace: ns.Name,
			},
			Spec: fedhcpv1alpha1.LeaseSpec{
				MAC:       "aa:bb:cc:dd:ee:ff",
				IP:        leasedIP.String(),
				FirstSeen: originalFirstSeen,
				Renewed:   originalFirstSeen,
				ExpiresAt: metav1.NewTime(originalFirstSeen.Add(24 * time.Hour)),
			},
		}
		Expect(testK8sClient.Create(ctx, existingLease)).To(Succeed())
		DeferCleanup(testK8sClient.Delete, existingLease)

		req, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		req.MessageType = dhcpv6.MessageTypeRequest

		peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
		linkAddr := net.ParseIP("2001:db8:aaaa:bbbb:cccc::")

		relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
		Expect(err).NotTo(HaveOccurred())

		stub, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		stub.MessageType = dhcpv6.MessageTypeReply
		stub.AddOption(&dhcpv6.OptIANA{
			IaId: [4]byte{1, 2, 3, 4},
			Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
				&dhcpv6.OptIAAddress{
					IPv6Addr:          leasedIP,
					PreferredLifetime: 24 * time.Hour,
					ValidLifetime:     24 * time.Hour,
				},
			}},
		})

		resp, stop := handler6(relayedRequest, stub)
		Expect(resp).NotTo(BeNil())
		Expect(stop).To(BeFalse())

		lease := &fedhcpv1alpha1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedName,
				Namespace: ns.Name,
			},
		}

		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.FirstSeen.Time", BeTemporally("~", originalFirstSeen.Time)),
			HaveField("Spec.Renewed.Time", Not(BeTemporally("~", originalFirstSeen.Time))),
		))
	})

	It("Should create and then patch a lease for the same IP", func(ctx SpecContext) {
		leasedIP := net.ParseIP("2001:db8:cccc:dddd:eeee:ff00:1122:3344")
		expectedName := "2001-0db8-cccc-dddd-eeee-ff00-1122-3344"

		req, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		req.MessageType = dhcpv6.MessageTypeRequest

		peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
		linkAddr := net.ParseIP("2001:db8:cccc:dddd:eeee::")

		relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
		Expect(err).NotTo(HaveOccurred())

		stub, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		stub.MessageType = dhcpv6.MessageTypeReply
		stub.AddOption(&dhcpv6.OptIANA{
			IaId: [4]byte{1, 2, 3, 4},
			Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
				&dhcpv6.OptIAAddress{
					IPv6Addr:          leasedIP,
					PreferredLifetime: 24 * time.Hour,
					ValidLifetime:     24 * time.Hour,
				},
			}},
		})

		resp, stop := handler6(relayedRequest, stub)
		Expect(resp).NotTo(BeNil())
		Expect(stop).To(BeFalse())

		lease := &fedhcpv1alpha1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedName,
				Namespace: ns.Name,
			},
		}

		Eventually(Object(lease)).Should(HaveField("Spec.IP", leasedIP.String()))

		firstRenewed := lease.Spec.Renewed

		time.Sleep(1100 * time.Millisecond)

		resp, stop = handler6(relayedRequest, stub)
		Expect(resp).NotTo(BeNil())
		Expect(stop).To(BeFalse())

		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.IP", leasedIP.String()),
			HaveField("Spec.Renewed.Time", Not(BeTemporally("==", firstRenewed.Time))),
		))

		DeferCleanup(testK8sClient.Delete, lease)
	})

	It("Should pass through when response has no IANA", func(ctx SpecContext) {
		req, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		req.MessageType = dhcpv6.MessageTypeRequest

		peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
		linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")

		relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
		Expect(err).NotTo(HaveOccurred())

		stub, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		stub.MessageType = dhcpv6.MessageTypeReply

		resp, stop := handler6(relayedRequest, stub)
		Expect(resp).NotTo(BeNil())
		Expect(stop).To(BeFalse())
	})

	It("Should drop non-relay requests", func(ctx SpecContext) {
		req, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		req.MessageType = dhcpv6.MessageTypeRequest

		stub, err := dhcpv6.NewMessage()
		Expect(err).NotTo(HaveOccurred())
		stub.MessageType = dhcpv6.MessageTypeReply

		resp, stop := handler6(req, stub)
		Expect(resp).To(BeNil())
		Expect(stop).To(BeTrue())
	})
})

func newRelayedRequest(peerAddr net.IP) *dhcpv6.RelayMessage {
	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333::"), peerAddr)
	Expect(err).NotTo(HaveOccurred())
	return relayedRequest
}

func newReplyWithIANA(leasedIP net.IP) *dhcpv6.Message {
	stub, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	stub.MessageType = dhcpv6.MessageTypeReply
	stub.AddOption(&dhcpv6.OptIANA{
		IaId: [4]byte{1, 2, 3, 4},
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          leasedIP,
				PreferredLifetime: 24 * time.Hour,
				ValidLifetime:     24 * time.Hour,
			},
		}},
	})
	return stub
}
