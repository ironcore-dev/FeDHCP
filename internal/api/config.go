package api

type Machines struct {
	MachineList `json:",inline"`
}

type Machine struct {
	Name       string `json:"name"`
	MacAddress string `json:"macAddress"`
}

type MachineList []Machine
