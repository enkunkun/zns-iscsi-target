package iscsi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeyValuePairs(t *testing.T) {
	t.Run("basic key=value parsing", func(t *testing.T) {
		data := []byte("InitiatorName=iqn.test\x00MaxRecvDataSegmentLength=8192\x00")
		kv := ParseKeyValuePairs(data)
		assert.Equal(t, "iqn.test", kv["InitiatorName"])
		assert.Equal(t, "8192", kv["MaxRecvDataSegmentLength"])
	})

	t.Run("trailing null", func(t *testing.T) {
		data := []byte("Key=Value\x00")
		kv := ParseKeyValuePairs(data)
		assert.Equal(t, "Value", kv["Key"])
		assert.Len(t, kv, 1)
	})

	t.Run("empty data", func(t *testing.T) {
		kv := ParseKeyValuePairs([]byte{})
		assert.Len(t, kv, 0)
	})

	t.Run("value with equals sign", func(t *testing.T) {
		data := []byte("CHAP_R=0x1234=5678\x00")
		kv := ParseKeyValuePairs(data)
		// Only first '=' is the separator
		assert.Equal(t, "0x1234=5678", kv["CHAP_R"])
	})

	t.Run("no value", func(t *testing.T) {
		data := []byte("Key=\x00")
		kv := ParseKeyValuePairs(data)
		assert.Equal(t, "", kv["Key"])
	})

	t.Run("multiple null separators", func(t *testing.T) {
		data := []byte("A=1\x00\x00B=2\x00")
		kv := ParseKeyValuePairs(data)
		assert.Equal(t, "1", kv["A"])
		assert.Equal(t, "2", kv["B"])
	})
}

func TestSerializeKeyValuePairs(t *testing.T) {
	t.Run("single key-value", func(t *testing.T) {
		kv := map[string]string{"ImmediateData": "Yes"}
		data := SerializeKeyValuePairs(kv)
		assert.Contains(t, string(data), "ImmediateData=Yes")
		assert.Contains(t, string(data), "\x00")
	})

	t.Run("round-trip", func(t *testing.T) {
		original := map[string]string{
			"MaxBurstLength": "262144",
			"ImmediateData":  "Yes",
		}
		serialized := SerializeKeyValuePairs(original)
		parsed := ParseKeyValuePairs(serialized)
		assert.Equal(t, original, parsed)
	})
}

func TestSerializeKeyValueList(t *testing.T) {
	pairs := []string{"Key1=Value1", "Key2=Value2"}
	data := SerializeKeyValueList(pairs)

	assert.Equal(t, byte(0x00), data[len("Key1=Value1")])
	assert.Contains(t, string(data), "Key1=Value1")
	assert.Contains(t, string(data), "Key2=Value2")

	// Count null separators
	nullCount := 0
	for _, b := range data {
		if b == 0x00 {
			nullCount++
		}
	}
	assert.Equal(t, 2, nullCount)
}

func TestNegotiateParamsMaxBurstLength(t *testing.T) {
	target := DefaultTargetParams()
	target.MaxBurstLength = 65536

	// Initiator requests lower value
	initKV := map[string]string{"MaxBurstLength": "32768"}
	rsp, p := NegotiateParams(initKV, target)

	assert.Equal(t, "32768", rsp["MaxBurstLength"])
	assert.Equal(t, 32768, p.MaxBurstLength)
}

func TestNegotiateParamsMaxBurstLengthInitiatorHigher(t *testing.T) {
	target := DefaultTargetParams()
	target.MaxBurstLength = 65536

	// Initiator requests higher value; target max wins
	initKV := map[string]string{"MaxBurstLength": "1048576"}
	rsp, p := NegotiateParams(initKV, target)

	assert.Equal(t, "65536", rsp["MaxBurstLength"])
	assert.Equal(t, 65536, p.MaxBurstLength)
}

func TestNegotiateParamsImmediateData(t *testing.T) {
	target := DefaultTargetParams()
	target.ImmediateData = true

	t.Run("both yes -> yes", func(t *testing.T) {
		initKV := map[string]string{"ImmediateData": "Yes"}
		rsp, p := NegotiateParams(initKV, target)
		assert.Equal(t, "Yes", rsp["ImmediateData"])
		assert.True(t, p.ImmediateData)
	})

	t.Run("initiator no -> no (AND logic)", func(t *testing.T) {
		initKV := map[string]string{"ImmediateData": "No"}
		rsp, p := NegotiateParams(initKV, target)
		assert.Equal(t, "No", rsp["ImmediateData"])
		assert.False(t, p.ImmediateData)
	})
}

func TestNegotiateParamsInitialR2T(t *testing.T) {
	target := DefaultTargetParams()
	target.InitialR2T = false

	t.Run("initiator yes -> yes (OR logic)", func(t *testing.T) {
		initKV := map[string]string{"InitialR2T": "Yes"}
		rsp, p := NegotiateParams(initKV, target)
		assert.Equal(t, "Yes", rsp["InitialR2T"])
		assert.True(t, p.InitialR2T)
	})

	t.Run("both no -> no", func(t *testing.T) {
		initKV := map[string]string{"InitialR2T": "No"}
		rsp, p := NegotiateParams(initKV, target)
		assert.Equal(t, "No", rsp["InitialR2T"])
		assert.False(t, p.InitialR2T)
	})
}

func TestNegotiateParamsDeclarative(t *testing.T) {
	target := DefaultTargetParams()
	initKV := map[string]string{
		"InitiatorName": "iqn.2001-04.com.example:storage.disk2",
		"SessionType":   "Normal",
	}
	_, p := NegotiateParams(initKV, target)
	assert.Equal(t, "iqn.2001-04.com.example:storage.disk2", p.InitiatorName)
	assert.Equal(t, "Normal", p.SessionType)
}

func TestNegotiateParamsUnknownKey(t *testing.T) {
	target := DefaultTargetParams()
	initKV := map[string]string{"SomeUnknownKey": "SomeValue"}
	rsp, _ := NegotiateParams(initKV, target)
	assert.Equal(t, "NotUnderstood", rsp["SomeUnknownKey"])
}

func TestNegotiateParamsHeaderDigest(t *testing.T) {
	target := DefaultTargetParams()
	initKV := map[string]string{"HeaderDigest": "CRC32C,None"}
	rsp, p := NegotiateParams(initKV, target)
	// We only support None
	assert.Equal(t, "None", rsp["HeaderDigest"])
	assert.Equal(t, "None", p.HeaderDigest)
}

func TestNegotiateParamsDataDigest(t *testing.T) {
	target := DefaultTargetParams()
	initKV := map[string]string{"DataDigest": "CRC32C"}
	rsp, p := NegotiateParams(initKV, target)
	assert.Equal(t, "None", rsp["DataDigest"])
	assert.Equal(t, "None", p.DataDigest)
}

func TestNegotiateParamsMaxRecvDataSegmentLength(t *testing.T) {
	target := DefaultTargetParams()
	target.MaxRecvDataSegmentLength = 32768

	initKV := map[string]string{"MaxRecvDataSegmentLength": "16384"}
	rsp, p := NegotiateParams(initKV, target)
	assert.Equal(t, "16384", rsp["MaxRecvDataSegmentLength"])
	assert.Equal(t, 16384, p.MaxRecvDataSegmentLength)
}

func TestDefaultTargetParams(t *testing.T) {
	p := DefaultTargetParams()
	require.Equal(t, DefaultMaxRecvDataSegmentLength, p.MaxRecvDataSegmentLength)
	require.Equal(t, DefaultMaxBurstLength, p.MaxBurstLength)
	require.Equal(t, DefaultFirstBurstLength, p.FirstBurstLength)
	require.True(t, p.InitialR2T)
	require.True(t, p.ImmediateData)
	require.Equal(t, "None", p.HeaderDigest)
	require.Equal(t, "None", p.DataDigest)
}

func TestParseBoolParam(t *testing.T) {
	assert.True(t, parseBoolParam("Yes"))
	assert.True(t, parseBoolParam("yes"))
	assert.True(t, parseBoolParam("YES"))
	assert.False(t, parseBoolParam("No"))
	assert.False(t, parseBoolParam("no"))
	assert.False(t, parseBoolParam(""))
}

func TestBoolToYesNo(t *testing.T) {
	assert.Equal(t, "Yes", boolToYesNo(true))
	assert.Equal(t, "No", boolToYesNo(false))
}

func TestMinMax(t *testing.T) {
	assert.Equal(t, 3, min(3, 5))
	assert.Equal(t, 3, min(5, 3))
	assert.Equal(t, 5, max(3, 5))
	assert.Equal(t, 5, max(5, 3))
}
