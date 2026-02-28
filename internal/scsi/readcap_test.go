package scsi

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleReadCapacity10(t *testing.T) {
	t.Run("basic capacity encoding", func(t *testing.T) {
		capacity := uint64(1024) // 1024 sectors
		blockSize := uint32(512)

		data := handleReadCapacity10(capacity, blockSize)
		require.Len(t, data, 8)

		lastLBA := binary.BigEndian.Uint32(data[0:4])
		bs := binary.BigEndian.Uint32(data[4:8])

		assert.Equal(t, uint32(1023), lastLBA, "last LBA must be capacity-1")
		assert.Equal(t, blockSize, bs, "block size mismatch")
	})

	t.Run("large capacity is capped at 0xFFFFFFFF", func(t *testing.T) {
		// 64-bit capacity larger than 32-bit max
		capacity := uint64(0x200000000) // 8GB
		data := handleReadCapacity10(capacity, 512)
		require.Len(t, data, 8)
		lastLBA := binary.BigEndian.Uint32(data[0:4])
		assert.Equal(t, uint32(0xFFFFFFFF), lastLBA, "must be capped at 0xFFFFFFFF")
	})

	t.Run("block size is big-endian", func(t *testing.T) {
		data := handleReadCapacity10(100, 512)
		assert.Equal(t, byte(0x00), data[4])
		assert.Equal(t, byte(0x00), data[5])
		assert.Equal(t, byte(0x02), data[6])
		assert.Equal(t, byte(0x00), data[7])
	})
}

func TestHandleReadCapacity16(t *testing.T) {
	t.Run("returns 32 bytes", func(t *testing.T) {
		data := handleReadCapacity16(1024, 512)
		require.Len(t, data, 32)
	})

	t.Run("64-bit last LBA encoded correctly", func(t *testing.T) {
		capacity := uint64(0x100000000) + 1 // > 32-bit
		data := handleReadCapacity16(capacity, 512)

		lastLBA := binary.BigEndian.Uint64(data[0:8])
		assert.Equal(t, capacity-1, lastLBA)
	})

	t.Run("block size at offset 8", func(t *testing.T) {
		data := handleReadCapacity16(1000, 512)
		bs := binary.BigEndian.Uint32(data[8:12])
		assert.Equal(t, uint32(512), bs)
	})

	t.Run("protection fields are zero", func(t *testing.T) {
		data := handleReadCapacity16(1000, 512)
		assert.Equal(t, byte(0), data[12])
		assert.Equal(t, byte(0), data[13])
	})
}
