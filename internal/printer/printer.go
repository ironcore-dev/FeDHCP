// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package printer

import (
	h "github.com/ironcore-dev/fedhcp/internal/helper"
	"github.com/sirupsen/logrus"
)

type IPFamily string

var (
	IPv4 IPFamily = "IPv4"
	IPv6 IPFamily = "IPv6"
)

func VerboseRequest(req h.DHCPPacket, log *logrus.Entry, ipFamily IPFamily) {
	if req != nil {
		log.Debugf("Received %s request: %s", ipFamily, req.Summary())
	} else {
		// should not happen, checked in all handlers
		log.Errorf("No %s request received", ipFamily)
	}
}

func VerboseResponse(req, resp h.DHCPPacket, log *logrus.Entry, ipFamily IPFamily) {
	if resp != nil {
		log.Debugf("Sent %s response: %s", ipFamily, resp.Summary())
	} else {
		log.Debugf("No response sent for %s request: %s", ipFamily, req.Summary())
	}
}
