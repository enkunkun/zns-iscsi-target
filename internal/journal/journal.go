package journal

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

// Journal manages the Write-Ahead Log (WAL) file.
type Journal struct {
	mu      sync.Mutex
	file    *os.File
	writer  *bufio.Writer
	lsn     atomic.Uint64
	pending [][]byte   // pending records not yet fsynced
	path    string
}

// Open opens (or creates) a journal file at the given path.
func Open(path string) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening journal %q: %w", path, err)
	}

	// Determine the current LSN by scanning the file
	lsn, err := scanMaxLSN(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("scanning journal LSN: %w", err)
	}

	// Seek to end for appending
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("seeking to end of journal: %w", err)
	}

	j := &Journal{
		file:   f,
		writer: bufio.NewWriterSize(f, 64*1024),
		path:   path,
	}
	j.lsn.Store(lsn)
	return j, nil
}

// scanMaxLSN reads the journal file to find the highest LSN written.
func scanMaxLSN(f *os.File) (uint64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}

	var maxLSN uint64
	buf := make([]byte, baseRecordSize)

	for {
		// Read the length prefix (4 bytes)
		var length uint32
		if err := binary.Read(f, binary.BigEndian, &length); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return 0, err
		}

		if length < baseRecordSize {
			break
		}

		// Read the record
		if int(length) > len(buf) {
			buf = make([]byte, length)
		}
		recBuf := buf[:length]

		if _, err := io.ReadFull(f, recBuf); err != nil {
			break
		}

		// Parse just the LSN
		var rec Record
		if err := rec.UnmarshalBinary(recBuf); err != nil {
			continue
		}
		if rec.LSN > maxLSN {
			maxLSN = rec.LSN
		}
	}

	return maxLSN, nil
}

// nextLSN atomically increments and returns the next LSN.
func (j *Journal) nextLSN() uint64 {
	return j.lsn.Add(1)
}

// CurrentLSN returns the current LSN.
func (j *Journal) CurrentLSN() uint64 {
	return j.lsn.Load()
}

// appendRecord serializes and buffers a record for writing.
// Must be called with j.mu held.
func (j *Journal) appendRecord(rec *Record) error {
	data, err := rec.MarshalBinary()
	if err != nil {
		return err
	}

	// Write length prefix then record data
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))

	payload := make([]byte, 4+len(data))
	copy(payload[:4], lenBuf[:])
	copy(payload[4:], data)

	j.pending = append(j.pending, payload)
	return nil
}

// LogL2PUpdate logs an L2P table update.
func (j *Journal) LogL2PUpdate(segID uint64, oldPhys, newPhys ztl.PhysAddr) (uint64, error) {
	lsn := j.nextLSN()
	rec := &Record{
		Type:        RecordTypeL2PUpdate,
		LSN:         lsn,
		SegmentID:   segID,
		OldPhysAddr: oldPhys,
		NewPhysAddr: newPhys,
		Timestamp:   time.Now().UnixNano(),
	}

	j.mu.Lock()
	err := j.appendRecord(rec)
	j.mu.Unlock()

	if err != nil {
		return 0, err
	}
	return lsn, nil
}

// LogZoneOpen logs a zone open operation.
func (j *Journal) LogZoneOpen(zoneID uint32) error {
	return j.logZoneAction(RecordTypeZoneOpen, uint64(zoneID))
}

// LogZoneClose logs a zone close operation.
func (j *Journal) LogZoneClose(zoneID uint32) error {
	return j.logZoneAction(RecordTypeZoneClose, uint64(zoneID))
}

// LogZoneReset logs a zone reset intent.
func (j *Journal) LogZoneReset(zoneID uint32) error {
	return j.logZoneAction(RecordTypeZoneReset, uint64(zoneID))
}

// logZoneAction logs a zone management operation.
func (j *Journal) logZoneAction(recType uint8, zoneID uint64) error {
	lsn := j.nextLSN()
	rec := &Record{
		Type:      recType,
		LSN:       lsn,
		SegmentID: zoneID,
		Timestamp: time.Now().UnixNano(),
	}

	j.mu.Lock()
	err := j.appendRecord(rec)
	j.mu.Unlock()

	return err
}

// GroupCommit flushes all pending records to disk with fsync.
func (j *Journal) GroupCommit() error {
	j.mu.Lock()
	if len(j.pending) == 0 {
		j.mu.Unlock()
		return nil
	}

	// Write all pending records
	for _, payload := range j.pending {
		if _, err := j.writer.Write(payload); err != nil {
			j.mu.Unlock()
			return fmt.Errorf("writing WAL record: %w", err)
		}
	}
	j.pending = j.pending[:0]

	if err := j.writer.Flush(); err != nil {
		j.mu.Unlock()
		return fmt.Errorf("flushing WAL writer: %w", err)
	}
	j.mu.Unlock()

	// fsync outside the lock (it's slow)
	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("syncing WAL: %w", err)
	}

	return nil
}

// StartGroupCommitLoop starts a background goroutine that periodically commits.
func (j *Journal) StartGroupCommitLoop(ctx context.Context, syncPeriodMs int) {
	go func() {
		ticker := time.NewTicker(time.Duration(syncPeriodMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = j.GroupCommit() // final commit on shutdown
				return
			case <-ticker.C:
				_ = j.GroupCommit()
			}
		}
	}()
}

// ReadAllRecords reads all records from the journal file from the given offset.
// If afterLSN > 0, only returns records with LSN > afterLSN.
func (j *Journal) ReadAllRecords(afterLSN uint64) ([]*Record, error) {
	j.mu.Lock()
	// Flush buffered writes first to ensure we read everything
	if err := j.writer.Flush(); err != nil {
		j.mu.Unlock()
		return nil, err
	}
	j.mu.Unlock()

	f, err := os.Open(j.path)
	if err != nil {
		return nil, fmt.Errorf("opening journal for read: %w", err)
	}
	defer f.Close()

	return readRecords(f, afterLSN)
}

// readRecords reads all valid records from the given reader.
func readRecords(r io.Reader, afterLSN uint64) ([]*Record, error) {
	var records []*Record
	lenBuf := make([]byte, 4)

	for {
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}

		length := binary.BigEndian.Uint32(lenBuf)
		if length < baseRecordSize || length > 100*1024*1024 {
			break // corrupted or end of valid records
		}

		recBuf := make([]byte, length)
		if _, err := io.ReadFull(r, recBuf); err != nil {
			break
		}

		var rec Record
		if err := rec.UnmarshalBinary(recBuf); err != nil {
			break // corrupted record; stop here
		}

		if rec.LSN > afterLSN {
			r2 := rec
			records = append(records, &r2)
		}
	}

	return records, nil
}

// Close flushes pending records and closes the file.
func (j *Journal) Close() error {
	if err := j.GroupCommit(); err != nil {
		return err
	}
	return j.file.Close()
}

// Path returns the journal file path.
func (j *Journal) Path() string {
	return j.path
}
