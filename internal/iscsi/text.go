package iscsi

import (
	"fmt"
	"net"
)

// handleSendTargets processes an iSCSI discovery text request.
// Returns the SendTargets response data segment.
func handleSendTargets(req *PDU, targets []*Target, localAddr net.Addr) []byte {
	kvIn := ParseKeyValuePairs(req.DataSegment)
	sendTargets, ok := kvIn["SendTargets"]
	if !ok {
		return nil
	}

	var responseKV []string

	for _, t := range targets {
		if sendTargets == "All" || sendTargets == t.IQN {
			responseKV = append(responseKV, fmt.Sprintf("TargetName=%s", t.IQN))
			// Use the configured portal or the connection's local address
			portal := t.Portal
			if portal == "" && localAddr != nil {
				portal = localAddr.String()
			}
			if portal != "" {
				responseKV = append(responseKV, fmt.Sprintf("TargetAddress=%s,1", portal))
			}
		}
	}

	if len(responseKV) == 0 {
		return nil
	}

	return SerializeKeyValueList(responseKV)
}
