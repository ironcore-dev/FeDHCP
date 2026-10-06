// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ironcore-dev/controller-utils/modutils"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/helper"
	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	"github.com/mdlayher/netx/eui64"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	//+kubebuilder:scaffold:imports
)

const (
	pollingInterval      = 50 * time.Millisecond
	eventuallyTimeout    = 3 * time.Second
	consistentlyDuration = 1 * time.Second
	oobConfigFile        = "config.yaml"
	oobSubnetV6Name      = "oob-subnet-v6"
	oobSubnetV4Name      = "oob-subnet-v4"
	oobSubnetV6CIDR      = "2001:db8::/64"
	oobSubnetV4CIDR      = "192.168.1.0/24"
	foreignSubnetV6Name  = "foreign-subnet-v6"
	foreignSubnetV6CIDR  = "2001:db8:1::/64"
	relayIPV6Address     = "2001:db8::1"
	unknownIPV6Address   = "2001:db8:ffff::1"
	privateIPV4Address   = "192.168.1.11"
	unknownIPV4Address   = "10.0.0.11"
	linkLocalIPV6Prefix  = "fe80::"
	clientMACAddress     = "11:22:33:44:55:66"
	otherMACAddress      = "aa:bb:cc:dd:ee:ff"
)

var (
	cfg           *rest.Config
	k8sClientTest client.Client
	testEnv       *envtest.Environment

	oobSubnetLabels = []api.SubnetLabel{
		{Key: "subnet", Value: "oob"},
		{Key: "foo", Value: "bar"},
	}
)

func TestOOB(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "OOB Plugin Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			modutils.Dir("github.com/ironcore-dev/ipam", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.36.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	DeferCleanup(testEnv.Stop)

	Expect(ipamv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClientTest, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientTest).NotTo(BeNil())

	// set komega client
	SetClient(k8sClientTest)

	// assign global k8s client and config in plugin
	kubernetes.SetClient(&k8sClientTest)
	kubernetes.SetConfig(cfg)

	// the plugin waits for the IPAM operator to reserve an IP, keep the waiting short
	helper.Config.IpPollingInterval = pollingInterval
	helper.Config.IpPollingTimeout = eventuallyTimeout

	By("starting a fake IPAM operator")
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go runFakeIPAM(ctx)
})

func SetupTest() *corev1.Namespace {
	ns := &corev1.Namespace{}

	BeforeEach(func(ctx SpecContext) {
		*ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		Expect(k8sClientTest.Create(ctx, ns)).To(Succeed(), "failed to create test namespace")
		DeferCleanup(k8sClientTest.Delete, ns)

		By("Creating the OOB subnets")
		createSubnet(ctx, ns.Name, oobSubnetV6Name, oobSubnetV6CIDR, ipamv1alpha1.IPv6SubnetType, subnetLabelsMap(oobSubnetLabels))
		createSubnet(ctx, ns.Name, oobSubnetV4Name, oobSubnetV4CIDR, ipamv1alpha1.IPv4SubnetType, subnetLabelsMap(oobSubnetLabels))

		By("Creating a non-OOB subnet")
		createSubnet(ctx, ns.Name, foreignSubnetV6Name, foreignSubnetV6CIDR, ipamv1alpha1.IPv6SubnetType, map[string]string{"subnet": "foreign"})

		configFile := writeConfig(api.OOBConfig{
			Namespace:    ns.Name,
			SubnetLabels: oobSubnetLabels,
		})

		h6, err := setup6(configFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())

		h4, err := setup4(configFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(h4).NotTo(BeNil())

		Expect(k8sClient.Namespace).To(Equal(ns.Name))
		Expect(k8sClient.SubnetLabels).To(Equal(oobSubnetLabels))
	})

	return ns
}

func writeConfig(config api.OOBConfig) string {
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), oobConfigFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	return file.Name()
}

func subnetLabelsMap(labels []api.SubnetLabel) map[string]string {
	m := make(map[string]string, len(labels))
	for _, label := range labels {
		m[label.Key] = label.Value
	}
	return m
}

func createSubnet(ctx context.Context, namespace, name, cidr string,
	subnetType ipamv1alpha1.SubnetAddressType, labels map[string]string) {
	subnet := &ipamv1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
	}
	Expect(k8sClientTest.Create(ctx, subnet)).To(Succeed())

	Eventually(UpdateStatus(subnet, func() {
		subnet.Status.Type = subnetType
		subnet.Status.Reserved = &ipamv1alpha1.CIDR{Net: netip.MustParsePrefix(cidr)}
		subnet.Status.State = ipamv1alpha1.FinishedSubnetState
	})).Should(Succeed())
}

// runFakeIPAM emulates the IPAM operator, which is not running in the test environment:
// every IP without a status gets an address reserved and is marked as finished.
func runFakeIPAM(ctx context.Context) {
	defer GinkgoRecover()

	ticker := time.NewTicker(pollingInterval / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileIPs(ctx)
		}
	}
}

func reconcileIPs(ctx context.Context) {
	ipList := &ipamv1alpha1.IPList{}
	if err := k8sClientTest.List(ctx, ipList); err != nil {
		return
	}

	for i := range ipList.Items {
		ip := &ipList.Items[i]
		if ip.Status.State != "" || !ip.DeletionTimestamp.IsZero() {
			continue
		}

		addr, err := reserveAddress(ctx, ip, ipList.Items)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "fake IPAM: failed to reserve address for IP %s: %v\n",
				client.ObjectKeyFromObject(ip), err)
			continue
		}

		base := ip.DeepCopy()
		ip.Status.Reserved = addr
		ip.Status.State = ipamv1alpha1.FinishedIPState
		// optimistic lock: never override a status which has been set in the meantime (e.g. by a test)
		if err := k8sClientTest.Status().Patch(ctx, ip, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "fake IPAM: failed to patch IP %s: %v\n",
				client.ObjectKeyFromObject(ip), err)
		}
	}
}

// reserveAddress returns the requested IP if set, otherwise an EUI-64 address for IPv6 subnets
// or the first free address for IPv4 subnets.
func reserveAddress(ctx context.Context, ip *ipamv1alpha1.IP, allIPs []ipamv1alpha1.IP) (*ipamv1alpha1.IPAddr, error) {
	if ip.Spec.IP != nil {
		return ip.Spec.IP, nil
	}

	subnet := &ipamv1alpha1.Subnet{}
	if err := k8sClientTest.Get(ctx, client.ObjectKey{Namespace: ip.Namespace, Name: ip.Spec.Subnet.Name}, subnet); err != nil {
		return nil, err
	}
	if subnet.Status.Reserved == nil {
		return nil, fmt.Errorf("subnet %s has no reserved CIDR", client.ObjectKeyFromObject(subnet))
	}
	prefix := subnet.Status.Reserved.Net

	if prefix.Addr().Is6() {
		mac, err := hex.DecodeString(ip.Labels["mac"])
		if err != nil {
			return nil, fmt.Errorf("invalid mac label: %w", err)
		}
		addr, err := eui64.ParseMAC(prefix.Addr().AsSlice(), mac)
		if err != nil {
			return nil, err
		}
		return ipamv1alpha1.IPAddrFromString(addr.String())
	}

	used := make(map[netip.Addr]bool)
	for _, other := range allIPs {
		if other.Namespace == ip.Namespace && other.Spec.Subnet.Name == ip.Spec.Subnet.Name && other.Status.Reserved != nil {
			used[other.Status.Reserved.Net] = true
		}
	}
	for addr := prefix.Addr().Next(); prefix.Contains(addr); addr = addr.Next() {
		if !used[addr] {
			return &ipamv1alpha1.IPAddr{Net: addr}, nil
		}
	}

	return nil, fmt.Errorf("subnet %s exhausted", client.ObjectKeyFromObject(subnet))
}

// linkLocalAddress returns the EUI-64 link local IPv6 address of a MAC address.
func linkLocalAddress(mac string) net.IP {
	return eui64Address(linkLocalIPV6Prefix, mac)
}

// eui64Address returns the EUI-64 IPv6 address of a MAC address within the given prefix.
func eui64Address(prefix string, mac string) net.IP {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())
	addr, err := eui64.ParseMAC(net.ParseIP(prefix), hwAddr)
	Expect(err).NotTo(HaveOccurred())
	return addr
}
