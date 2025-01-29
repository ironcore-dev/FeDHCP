// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"os"

	"github.com/ironcore-dev/fedhcp/internal/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("OOB Plugin", func() {
	var (
		testConfigPath string
		err            error
	)

	BeforeEach(func() {
		// Setup temporary test config file
		testConfigPath = "oob_config.yaml"
		config := &api.OOBConfig{
			Namespace:   "oob-ns",
			SubnetLabel: "subnet=dhcp",
		}
		configData, err := yaml.Marshal(config)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(testConfigPath, configData, 0644)
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
			Expect(config.Namespace).To(Equal("oob-ns"))
			Expect(config.SubnetLabel).To(Equal("subnet=dhcp"))
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

	Describe("Plugin Setup6", func() {
		It("should successfully initialize the plugin with a valid config", func() {
			//_, err := NewK8sClient("oob-ns", "subnet=dhcp")
			//Expect(err).NotTo(HaveOccurred())
			handler, err := setup6(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   "oob-ns",
				SubnetLabel: "subnet-dhcp",
			}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, []byte(invalidConfigData), 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = setup6(testConfigPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be 'key=value'"))
		})
	})

})
