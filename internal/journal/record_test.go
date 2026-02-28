package journal

import (
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		rec  Record
	}{
		{
			name: "L2PUpdate",
			rec: Record{
				Type:        RecordTypeL2PUpdate,
				LSN:         1042,
				SegmentID:   256,
				OldPhysAddr: ztl.PhysAddr(0),
				NewPhysAddr: ztl.EncodePhysAddr(5, 0x50000),
				Timestamp:   1706000000000000000,
			},
		},
		{
			name: "ZoneOpen",
			rec: Record{
				Type:      RecordTypeZoneOpen,
				LSN:       1,
				SegmentID: 3, // zone ID
				Timestamp: 12345,
			},
		},
		{
			name: "ZoneClose",
			rec: Record{
				Type:      RecordTypeZoneClose,
				LSN:       2,
				SegmentID: 3,
				Timestamp: 12346,
			},
		},
		{
			name: "ZoneReset",
			rec: Record{
				Type:      RecordTypeZoneReset,
				LSN:       3,
				SegmentID: 7,
				Timestamp: 99999,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.rec.MarshalBinary()
			require.NoError(t, err)
			assert.Len(t, data, baseRecordSize)

			var decoded Record
			err = decoded.UnmarshalBinary(data)
			require.NoError(t, err)

			assert.Equal(t, tt.rec.Type, decoded.Type)
			assert.Equal(t, tt.rec.LSN, decoded.LSN)
			assert.Equal(t, tt.rec.SegmentID, decoded.SegmentID)
			assert.Equal(t, tt.rec.OldPhysAddr, decoded.OldPhysAddr)
			assert.Equal(t, tt.rec.NewPhysAddr, decoded.NewPhysAddr)
			assert.Equal(t, tt.rec.Timestamp, decoded.Timestamp)
		})
	}
}

func TestRecordCRCCorruption(t *testing.T) {
	rec := Record{
		Type:        RecordTypeL2PUpdate,
		LSN:         42,
		SegmentID:   100,
		NewPhysAddr: ztl.EncodePhysAddr(1, 1000),
	}

	data, err := rec.MarshalBinary()
	require.NoError(t, err)

	// Corrupt a byte in the payload
	data[20] ^= 0xFF

	var decoded Record
	err = decoded.UnmarshalBinary(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CRC32C")
}

func TestRecordInvalidMagic(t *testing.T) {
	rec := Record{
		Type:  RecordTypeL2PUpdate,
		LSN:   1,
	}

	data, err := rec.MarshalBinary()
	require.NoError(t, err)

	// Corrupt magic bytes
	data[0] = 0xFF
	data[1] = 0xFF

	var decoded Record
	err = decoded.UnmarshalBinary(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "magic")
}

func TestRecordTooShort(t *testing.T) {
	var decoded Record
	err := decoded.UnmarshalBinary(make([]byte, 10))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestCheckpointRecordRoundtrip(t *testing.T) {
	entries := []ztl.PhysAddr{
		ztl.EncodePhysAddr(0, 0),
		ztl.EncodePhysAddr(1, 100),
		ztl.EncodePhysAddr(2, 200),
		ztl.EncodePhysAddr(3, 300),
	}

	cpRec := &CheckpointRecord{
		Record: Record{
			Type:      RecordTypeCheckpoint,
			LSN:       999,
			Timestamp: 987654321,
		},
		L2PTableLen: uint64(len(entries)),
		L2PEntries:  entries,
	}

	data, err := cpRec.MarshalBinary()
	require.NoError(t, err)
	assert.Equal(t, baseRecordSize+len(entries)*8, len(data))

	var decoded CheckpointRecord
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, RecordTypeCheckpoint, decoded.Type)
	assert.Equal(t, uint64(999), decoded.LSN)
	assert.Equal(t, uint64(len(entries)), decoded.L2PTableLen)
	assert.Equal(t, entries, decoded.L2PEntries)
}

func TestCheckpointRecordEmpty(t *testing.T) {
	cpRec := &CheckpointRecord{
		Record: Record{
			Type:      RecordTypeCheckpoint,
			LSN:       1,
			Timestamp: 0,
		},
		L2PTableLen: 0,
		L2PEntries:  nil,
	}

	data, err := cpRec.MarshalBinary()
	require.NoError(t, err)
	assert.Equal(t, baseRecordSize, len(data))

	var decoded CheckpointRecord
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), decoded.L2PTableLen)
}

func TestRecordMagicValue(t *testing.T) {
	assert.Equal(t, uint32(0xADA50001), RecordMagic)
}

func TestAllRecordTypes(t *testing.T) {
	types := []uint8{
		RecordTypeL2PUpdate,
		RecordTypeZoneOpen,
		RecordTypeZoneClose,
		RecordTypeZoneReset,
		RecordTypeCheckpoint,
	}

	for _, rt := range types {
		rec := Record{Type: rt, LSN: 1}
		data, err := rec.MarshalBinary()
		require.NoError(t, err)
		var decoded Record
		require.NoError(t, decoded.UnmarshalBinary(data))
		assert.Equal(t, rt, decoded.Type)
	}
}
