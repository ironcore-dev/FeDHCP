// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package leases

import (
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	fedhcpv1alpha1 "github.com/ironcore-dev/fedhcp/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
