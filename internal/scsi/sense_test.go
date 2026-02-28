package scsi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSense(t *testing.T) {
	t.Run("response code is 0x70 fixed format", func(t *testing.T) {
		sense := BuildSense(SenseKeyIllegalRequest, ASCInvalidCommandOpCode, 0x00)
		require.Len(t, sense, 18)
		assert.Equal(t, byte(0x70), sense[0], "response code must be 0x70")
	})

	t.Run("sense key stored in byte 2 lower nibble", func(t *testing.T) {
		sense := BuildSense(SenseKeyMediumError, ASCUnrecoveredReadError, 0x00)
		assert.Equal(t, SenseKeyMediumError, sense[2]&0x0F)
	})

	t.Run("additional sense length is 10", func(t *testing.T) {
		sense := BuildSense(SenseKeyNoSense, 0x00, 0x00)
		assert.Equal(t, byte(0x0A), sense[7])
	})

	t.Run("ASC and ASCQ stored at bytes 12 and 13", func(t *testing.T) {
		sense := BuildSense(SenseKeyIllegalRequest, 0x24, 0x01)
		assert.Equal(t, byte(0x24), sense[12], "ASC mismatch")
		assert.Equal(t, byte(0x01), sense[13], "ASCQ mismatch")
	})

	t.Run("sense key high bits masked off", func(t *testing.T) {
		sense := BuildSense(0xFF, 0x00, 0x00) // pass 0xFF, only lower 4 bits used
		assert.Equal(t, byte(0x0F), sense[2], "high bits must be masked")
	})
}

func TestPrebuiltSenseBuffers(t *testing.T) {
	t.Run("SenseNoSense has no sense key", func(t *testing.T) {
		assert.Equal(t, byte(0x70), SenseNoSense[0])
		assert.Equal(t, SenseKeyNoSense, SenseNoSense[2]&0x0F)
	})

	t.Run("SenseIllegalRequest has illegal request key", func(t *testing.T) {
		assert.Equal(t, SenseKeyIllegalRequest, SenseIllegalRequest[2]&0x0F)
		assert.Equal(t, ASCInvalidCommandOpCode, SenseIllegalRequest[12])
	})

	t.Run("SenseMediumError has medium error key", func(t *testing.T) {
		assert.Equal(t, SenseKeyMediumError, SenseMediumError[2]&0x0F)
	})

	t.Run("SenseNotReady has not ready key", func(t *testing.T) {
		assert.Equal(t, SenseKeyNotReady, SenseNotReady[2]&0x0F)
	})

	t.Run("all pre-built senses are 18 bytes", func(t *testing.T) {
		for _, s := range [][]byte{SenseNoSense, SenseIllegalRequest, SenseMediumError, SenseNotReady} {
			assert.Len(t, s, 18)
		}
	})
}
