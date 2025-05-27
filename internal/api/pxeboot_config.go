// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type PxebootConfig struct {
	TFTPServer string `yaml:"tftpServer"`
	IPXEServer string `yaml:"ipxeServer"`
}
