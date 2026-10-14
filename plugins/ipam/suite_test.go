// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
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
	ipamConfigFile       = "config.yaml"
	ipamSubnet1Name      = "ipam-subnet-1"
	ipamSubnet1CIDR      = "2001:db8:1::/64"
	ipamSubnet2Name      = "ipam-subnet-2"
	ipamSubnet2CIDR      = "2001:db8:2::/64"
	foreignSubnetName    = "foreign-subnet"
	foreignSubnetCIDR    = "2001:db8:ff::/64"
	relayIPV6Address1    = "2001:db8:1::10"
	relayIPV6Address2    = "2001:db8:2::10"
	unknownIPV6Address   = "2001:db8:ffff::10"
	linkLocalIPV6Prefix  = "fe80::"
	clientMACAddress     = "11:22:33:44:55:66"
	clientMACKey         = "112233445566"
	macLabelKey          = "mac"
)

var (
	cfg           *rest.Config
	k8sClientTest client.Client
	testEnv       *envtest.Environment

	ipamSubnetNames = []string{ipamSubnet1Name, ipamSubnet2Name}
)

func TestIPAM(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "IPAM Plugin Suite")
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

		By("Creating the configured subnets")
		createSubnet(ctx, ns.Name, ipamSubnet1Name, ipamSubnet1CIDR)
		createSubnet(ctx, ns.Name, ipamSubnet2Name, ipamSubnet2CIDR)

		By("Creating a subnet which is not configured")
		createSubnet(ctx, ns.Name, foreignSubnetName, foreignSubnetCIDR)

		h6, err := setup6(writeConfig(api.IPAMConfig{
			Namespace: ns.Name,
			Subnets:   ipamSubnetNames,
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())

		Expect(k8sClient.Namespace).To(Equal(ns.Name))
		Expect(k8sClient.SubnetNames).To(Equal(ipamSubnetNames))
	})

	return ns
}

func writeConfig(config api.IPAMConfig) string {
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), ipamConfigFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	return file.Name()
}

// createSubnet creates an IPv6 subnet; an empty cidr leaves the subnet without a reserved CIDR.
func createSubnet(ctx context.Context, namespace, name, cidr string) {
	subnet := &ipamv1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	Expect(k8sClientTest.Create(ctx, subnet)).To(Succeed())

	if cidr == "" {
		return
	}

	Eventually(UpdateStatus(subnet, func() {
		subnet.Status.Type = ipamv1alpha1.IPv6SubnetType
		subnet.Status.Reserved = &ipamv1alpha1.CIDR{Net: netip.MustParsePrefix(cidr)}
		subnet.Status.State = ipamv1alpha1.FinishedSubnetState
	})).Should(Succeed())
}

// fakeIPAMDisabledNamespaces contains the namespaces in which the fake IPAM operator does not reserve IPs.
var fakeIPAMDisabledNamespaces sync.Map

// disableFakeIPAM stops the fake IPAM operator from reserving IPs in the namespace for the current spec.
func disableFakeIPAM(namespace string) {
	fakeIPAMDisabledNamespaces.Store(namespace, struct{}{})
	DeferCleanup(fakeIPAMDisabledNamespaces.Delete, namespace)
}

// runFakeIPAM emulates the IPAM operator, which is not running in the test environment:
// every IP without a status gets its requested address reserved and is marked as finished.
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
		if _, disabled := fakeIPAMDisabledNamespaces.Load(ip.Namespace); disabled {
			continue
		}
		// the plugin always requests a specific IP
		if ip.Status.State != "" || !ip.DeletionTimestamp.IsZero() || ip.Spec.IP == nil {
			continue
		}

		base := ip.DeepCopy()
		ip.Status.Reserved = ip.Spec.IP
		ip.Status.State = ipamv1alpha1.FinishedIPState
		// optimistic lock: never override a status which has been set in the meantime (e.g. by a test)
		if err := k8sClientTest.Status().Patch(ctx, ip, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "fake IPAM: failed to patch IP %s: %v\n",
				client.ObjectKeyFromObject(ip), err)
		}
	}
}

// clientLinkLocalAddress returns the EUI-64 link local IPv6 address of clientMACAddress.
func clientLinkLocalAddress() net.IP {
	hwAddr, err := net.ParseMAC(clientMACAddress)
	Expect(err).NotTo(HaveOccurred())
	addr, err := eui64.ParseMAC(net.ParseIP(linkLocalIPV6Prefix), hwAddr)
	Expect(err).NotTo(HaveOccurred())
	return addr
}
