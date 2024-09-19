// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type Machines struct {
	MachineList `json:",inline"`
}

type Machine struct {
	Name       string `json:"name"`
	MacAddress string `json:"macAddress"`
}

type MachineList []Machine
