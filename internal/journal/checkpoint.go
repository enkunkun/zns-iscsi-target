package journal

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

// WriteCheckpoint writes a complete L2P snapshot as a checkpoint record to the journal.
func WriteCheckpoint(j *Journal, l2p *ztl.L2PTable) error {
	lsn := j.nextLSN()
	entries := l2p.Snapshot()

	cpRec := &CheckpointRecord{
		Record: Record{
			Type:      RecordTypeCheckpoint,
			LSN:       lsn,
			Timestamp: time.Now().UnixNano(),
		},
		L2PTableLen: uint64(len(entries)),
		L2PEntries:  entries,
	}

	data, err := cpRec.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshaling checkpoint record: %w", err)
	}

	// Write length + data atomically under the lock
	j.mu.Lock()
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	payload := make([]byte, 4+len(data))
	copy(payload[:4], lenBuf[:])
	copy(payload[4:], data)
	j.pending = append(j.pending, payload)
	j.mu.Unlock()

	// Immediately commit the checkpoint
	return j.GroupCommit()
}

// ReadLatestCheckpoint scans the journal file for the most recent valid checkpoint.
// Returns the L2P snapshot, the checkpoint's LSN, and any error.
// If no checkpoint exists, returns nil, 0, nil.
func ReadLatestCheckpoint(path string) (l2pSnapshot []ztl.PhysAddr, checkpointLSN uint64, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("opening journal for checkpoint scan: %w", err)
	}
	defer f.Close()

	lenBuf := make([]byte, 4)
	var latestCheckpoint *CheckpointRecord

	for {
		if _, err := io.ReadFull(f, lenBuf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, 0, fmt.Errorf("reading record length: %w", err)
		}

		length := binary.BigEndian.Uint32(lenBuf)
		if length < baseRecordSize || length > 1024*1024*1024 {
			break
		}

		recBuf := make([]byte, length)
		if _, err := io.ReadFull(f, recBuf); err != nil {
			break
		}

		// Quick check: is this a checkpoint record?
		if len(recBuf) >= 9 && recBuf[8] == RecordTypeCheckpoint {
			var cpRec CheckpointRecord
			if err := cpRec.UnmarshalBinary(recBuf); err == nil {
				if latestCheckpoint == nil || cpRec.LSN > latestCheckpoint.LSN {
					latestCheckpoint = &cpRec
				}
			}
		}
	}

	if latestCheckpoint == nil {
		return nil, 0, nil
	}

	return latestCheckpoint.L2PEntries, latestCheckpoint.LSN, nil
}
