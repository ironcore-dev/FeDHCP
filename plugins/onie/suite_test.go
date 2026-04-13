// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package onie

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
)

const (
	pollingInterval       = 50 * time.Millisecond
	eventuallyTimeout     = 3 * time.Second
	consistentlyDuration  = 1 * time.Second
	testConfigPath        = "config.yaml"
	testONIEImagesAddress = "http://[2001:db8::1]:8086/onie"
	linkLocalIPV6Prefix   = "fe80::"
	testMAC               = "00:11:22:33:44:55"
)

func TestONIE(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "ONIE Plugin Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	log.Print("BeforeSuite: Runs once before all tests")

	configFile := testConfigPath
	config := &api.ONIEConfig{
		OnieImagesAddress: testONIEImagesAddress,
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
	Expect(onieImagesAddress).To(Equal(testONIEImagesAddress))
})
