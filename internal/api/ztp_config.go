// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type ZTPConfig struct {
	Switches []Switch `yaml:"switches"`
}

type Switch struct {
	MacAddress                string `yaml:"macAddress"`
	ProvisioningScriptAddress string `yaml:"provisioningScriptAddress"`
	Name                      string `yaml:"name"`
}
