// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type Arch string

const (
	AMD64       Arch = "amd64"
	ARM64       Arch = "arm64"
	UnknownArch Arch = "unknown"
)

type PxeBootConfig struct {
	TFTPAddress Addresses `yaml:"tftpAddress"`
	IPXEAddress Addresses `yaml:"ipxeAddress"`
}

type Addresses struct {
	IPv4 map[Arch]string `yaml:"ipv4"`
	IPv6 map[Arch]string `yaml:"ipv6"`
}

type Architectures struct {
	Amd64 string `yaml:"amd64"`
	Arm64 string `yaml:"arm64"`
}
