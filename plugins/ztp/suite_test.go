// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"os"
	"testing"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

const (
	pollingInterval               = 50 * time.Millisecond
	eventuallyTimeout             = 3 * time.Second
	consistentlyDuration          = 1 * time.Second
	testConfigPath                = "config.yaml"
	testZtpProvisioningScriptPath = "https://2001:db8::1/ztp/provisioning.sh"
	testZtpOverrideScriptPath     = "https://2001:db8::1/ztp/provision-override.sh"
	linkLocalIPV6Prefix           = "fe80::"
	inventoryMAC                  = "00:11:22:33:44:55"
	inventoryMACWithOverride      = "00:11:22:33:44:66"
	nonInventoryMAC               = "47:11:47:11:47:11"
	testONIEVendor                = "onie_vendor:x86_64-accton_as7726_32x-r0"
	testONIEInstallerURL          = "http://[2001:db8::1]/onie/accton_as7726_32x.bin"
	testONIEUnknownVendor         = "onie_vendor:x86_64-unknown_switch-r0"
)

func TestZTP(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "ZTP Plugin Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	log.Print("BeforeSuite: Runs once before all tests")

	configFile := testConfigPath
	config := &api.ZTPConfig{
		ProvisioningScriptAddress: testZtpProvisioningScriptPath,
		Switches: []api.Switch{
			{
				MacAddress: inventoryMAC,
				Name:       "test-switch",
			},
			{
				MacAddress:                inventoryMACWithOverride,
				ProvisioningScriptAddress: testZtpOverrideScriptPath,
				Name:                      "test-switch-override",
			},
		},
		ONIEInstallers: []api.ONIEInstaller{
			{
				Vendor:       testONIEVendor,
				InstallerURL: testONIEInstallerURL,
			},
		},
	}
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	_, err = setup6(file.Name())
	Expect(err).NotTo(HaveOccurred())
	Expect(inventory).To(HaveLen(2))
	Expect(onieInstallers).To(HaveLen(1))
})
