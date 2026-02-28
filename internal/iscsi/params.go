package iscsi

import (
	"bytes"
	"strconv"
	"strings"
)

// Default iSCSI operational parameters (RFC 7143 Appendix A).
const (
	DefaultMaxRecvDataSegmentLength = 65536
	DefaultMaxBurstLength           = 262144
	DefaultFirstBurstLength         = 65536
	DefaultMaxOutstandingR2T        = 1
	DefaultDefaultTime2Wait         = 2
	DefaultDefaultTime2Retain       = 20
	DefaultMaxConnections           = 1
)

// Params holds the negotiated iSCSI operational parameters for a session.
type Params struct {
	// Negotiated parameters
	MaxRecvDataSegmentLength int
	MaxBurstLength           int
	FirstBurstLength         int
	MaxOutstandingR2T        int
	DefaultTime2Wait         int
	DefaultTime2Retain       int
	MaxConnections           int
	InitialR2T               bool
	ImmediateData            bool
	DataPDUInOrder           bool
	DataSequenceInOrder      bool
	HeaderDigest             string
	DataDigest               string

	// Informational (not negotiated but used)
	InitiatorName string
	TargetName    string
	SessionType   string // "Normal" or "Discovery"
}

// DefaultTargetParams returns the default parameters for the target side.
func DefaultTargetParams() Params {
	return Params{
		MaxRecvDataSegmentLength: DefaultMaxRecvDataSegmentLength,
		MaxBurstLength:           DefaultMaxBurstLength,
		FirstBurstLength:         DefaultFirstBurstLength,
		MaxOutstandingR2T:        DefaultMaxOutstandingR2T,
		DefaultTime2Wait:         DefaultDefaultTime2Wait,
		DefaultTime2Retain:       DefaultDefaultTime2Retain,
		MaxConnections:           DefaultMaxConnections,
		InitialR2T:               true,
		ImmediateData:            true,
		DataPDUInOrder:           true,
		DataSequenceInOrder:      true,
		HeaderDigest:             "None",
		DataDigest:               "None",
	}
}

// ParseKeyValuePairs parses a null-separated key=value text format.
// This is the format used in iSCSI login and text PDU data segments.
func ParseKeyValuePairs(data []byte) map[string]string {
	result := make(map[string]string)
	// Split on null bytes
	parts := bytes.Split(data, []byte{0x00})
	for _, part := range parts {
		kv := string(part)
		if kv == "" {
			continue
		}
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		val := kv[idx+1:]
		result[key] = val
	}
	return result
}

// SerializeKeyValuePairs serializes a map of key=value pairs into
// the null-separated format used in iSCSI login PDUs.
func SerializeKeyValuePairs(kv map[string]string) []byte {
	var buf bytes.Buffer
	for k, v := range kv {
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(v)
		buf.WriteByte(0x00)
	}
	return buf.Bytes()
}

// SerializeKeyValueList serializes an ordered slice of key=value pairs.
// Use this when ordering matters (e.g., login response must preserve order).
func SerializeKeyValueList(pairs []string) []byte {
	var buf bytes.Buffer
	for _, kv := range pairs {
		buf.WriteString(kv)
		buf.WriteByte(0x00)
	}
	return buf.Bytes()
}

// NegotiateParams applies RFC 7143 negotiation rules to produce the final
// set of parameters for a session. The initiator parameters are parsed from
// the login PDU data segment; the target parameters are the local defaults.
// Returns the response key=value pairs and the negotiated Params.
func NegotiateParams(initiatorKV map[string]string, target Params) (map[string]string, Params) {
	result := make(map[string]string)
	p := target // start with target defaults

	for key, initVal := range initiatorKV {
		switch key {
		case "InitiatorName":
			p.InitiatorName = initVal
			// Declarative: echo back
		case "TargetName":
			p.TargetName = initVal
		case "SessionType":
			p.SessionType = initVal
		case "InitiatorAlias":
			// Accept but don't echo back

		case "MaxRecvDataSegmentLength":
			// The initiator is declaring its own max receive size.
			// We respect it for sending Data-In to the initiator.
			v := parseIntParam(initVal, DefaultMaxRecvDataSegmentLength)
			// The negotiated value for the initiator's receive = min(init, target)
			negVal := min(v, target.MaxRecvDataSegmentLength)
			p.MaxRecvDataSegmentLength = negVal
			result[key] = strconv.Itoa(negVal)

		case "MaxBurstLength":
			v := parseIntParam(initVal, DefaultMaxBurstLength)
			negVal := min(v, target.MaxBurstLength)
			p.MaxBurstLength = negVal
			result[key] = strconv.Itoa(negVal)

		case "FirstBurstLength":
			v := parseIntParam(initVal, DefaultFirstBurstLength)
			negVal := min(v, target.FirstBurstLength)
			p.FirstBurstLength = negVal
			result[key] = strconv.Itoa(negVal)

		case "MaxOutstandingR2T":
			v := parseIntParam(initVal, DefaultMaxOutstandingR2T)
			negVal := min(v, target.MaxOutstandingR2T)
			p.MaxOutstandingR2T = negVal
			result[key] = strconv.Itoa(negVal)

		case "DefaultTime2Wait":
			v := parseIntParam(initVal, DefaultDefaultTime2Wait)
			negVal := max(v, target.DefaultTime2Wait)
			p.DefaultTime2Wait = negVal
			result[key] = strconv.Itoa(negVal)

		case "DefaultTime2Retain":
			v := parseIntParam(initVal, DefaultDefaultTime2Retain)
			negVal := min(v, target.DefaultTime2Retain)
			p.DefaultTime2Retain = negVal
			result[key] = strconv.Itoa(negVal)

		case "MaxConnections":
			v := parseIntParam(initVal, 1)
			negVal := min(v, target.MaxConnections)
			p.MaxConnections = negVal
			result[key] = strconv.Itoa(negVal)

		case "InitialR2T":
			// Boolean OR: if either side requires InitialR2T, use Yes
			initBool := parseBoolParam(initVal)
			negVal := initBool || target.InitialR2T
			p.InitialR2T = negVal
			result[key] = boolToYesNo(negVal)

		case "ImmediateData":
			// Boolean AND: only Yes if both agree
			initBool := parseBoolParam(initVal)
			negVal := initBool && target.ImmediateData
			p.ImmediateData = negVal
			result[key] = boolToYesNo(negVal)

		case "DataPDUInOrder":
			initBool := parseBoolParam(initVal)
			negVal := initBool || target.DataPDUInOrder
			p.DataPDUInOrder = negVal
			result[key] = boolToYesNo(negVal)

		case "DataSequenceInOrder":
			initBool := parseBoolParam(initVal)
			negVal := initBool || target.DataSequenceInOrder
			p.DataSequenceInOrder = negVal
			result[key] = boolToYesNo(negVal)

		case "HeaderDigest":
			// We only support None
			result[key] = "None"
			p.HeaderDigest = "None"

		case "DataDigest":
			result[key] = "None"
			p.DataDigest = "None"

		case "AuthMethod":
			// Handled by login handler, not here

		case "CHAP_A", "CHAP_C", "CHAP_I", "CHAP_N", "CHAP_R":
			// CHAP parameters: pass through for login handler

		default:
			// Unknown parameters: respond with "NotUnderstood"
			result[key] = "NotUnderstood"
		}
	}

	return result, p
}

// parseIntParam parses an integer parameter value, returning defaultVal on error.
func parseIntParam(s string, defaultVal int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}

// parseBoolParam parses a "Yes"/"No" boolean parameter.
func parseBoolParam(s string) bool {
	return strings.EqualFold(s, "Yes")
}

// boolToYesNo converts a bool to "Yes" or "No".
func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
