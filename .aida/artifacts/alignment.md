# Phase 3: Alignment

## Session
- Session ID: aida-zns-iscsi-20260228-135900
- Phase: 3 - Alignment
- Date: 2026-02-28

---

## 1. Requirements Consistency Check

### 1.1 iSCSI Layer vs ZTL Layer
- PASS: iSCSI SCSI command dispatcher calls ZTL.Read/Write interface cleanly
- PASS: CmdSN ordering is handled at the iSCSI session layer; ZTL sees ordered operations
- PASS: Data-Out handling (multi-PDU write): iSCSI layer reassembles full write buffer before calling ZTL.Write
- POTENTIAL GAP: MaxBurstLength negotiation must be consistent with write buffer size; if negotiated burst is larger than buffer segment, ZTL buffer must handle split writes gracefully

### 1.2 L2P Table Sizing
- ISSUE: Initial design used 512-byte LBA granularity for L2P (64 GB RAM for 4 TB device)
- RESOLUTION (DD-01): Switched to 8 KB segment granularity
  - 4 TB / 8 KB = 524 million segments
  - At 8 bytes per entry: ~4 GB RAM for 4 TB device
  - This is acceptable for a server-class machine
  - Make segment size configurable (4KB, 8KB, 16KB) to tune RAM usage
- PASS: Windows formats NTFS with 4KB clusters by default; 8KB segment granularity causes up to 2x write amplification at ZTL level (acceptable for initial version)

### 1.3 Zone Size vs Buffer Size
- PASS: Buffer size (default 512 MB) is 2x the zone size (256 MB), allowing 2 zones worth of buffer
- PASS: Buffer flush triggers when a zone's worth of data is accumulated, ensuring sequential write alignment
- EDGE CASE: If multiple zones receive concurrent writes (random access pattern), buffer must batch per-zone independently

### 1.4 GC Correctness
- PASS: GC reads live data from victim zone, re-writes to fresh zone, then updates L2P table, then resets victim
- CONSISTENCY RISK: Between GC L2P update and zone reset, a crash leaves the victim zone in FULL state but L2P points to new location. Recovery must handle this.
  - MITIGATION: Journal GC operation as an atomic transaction: log (old_zone_reset_intent) before reset, so recovery can complete or rollback

### 1.5 Crash Recovery Completeness
- PASS: WAL records every L2P update before buffer flush
- PASS: Checkpoint captures L2P snapshot
- PASS: Recovery replays WAL entries after last checkpoint
- GAP: Zone write pointer position after crash must be re-read from device (Report Zones), not from journal. Write pointer is device-authoritative.
  - ACTION: Recovery flow must call ReportZones after journal replay to reconcile device write pointers

### 1.6 SG_IO Platform Dependency
- PASS: SG_IO ioctl is Linux-specific; SMR backend only compiles on Linux
- PASS: Emulator backend is platform-agnostic
- ACTION: Use build tags (`//go:build linux`) for SG_IO dependent code
- ACTION: CLI must return clear error if SMR backend selected on non-Linux

### 1.7 Windows iSCSI Initiator Compatibility
- PASS: INQUIRY response with 512-byte block size
- PASS: READ CAPACITY (10) and (16) returning correct LUN size
- PASS: VPD page 0x83 (Device Identification) required by Windows for disk signature
- GAP: Windows may send PERSISTENT RESERVE (PR) commands; must return appropriate error response (RESERVATION CONFLICT or NOT SUPPORTED) to avoid Windows stalling
  - ACTION: Add stub handler for PERSISTENT RESERVE IN/OUT returning Check Condition with ILLEGAL REQUEST

### 1.8 REST API vs Frontend
- PASS: Zone list response structure matches ZoneMap component expectations
- PASS: Stats response covers all dashboard panels
- PASS: TypeScript types in /web/src/types/api.ts must mirror Go API response types
  - ACTION: Consider generating TypeScript types from Go structs (e.g., using tygo or manual sync)

---

## 2. Identified Conflicts

### Conflict-01: iSCSI MaxRecvDataSegmentLength vs Write Buffer
- iSCSI negotiates max data segment per PDU (default 8KB, Windows typically negotiates 256KB-1MB)
- ZTL write buffer expects zone-aligned writes for efficiency
- RESOLUTION: iSCSI layer must reassemble scattered Data-Out PDUs into contiguous write before calling ZTL. Buffer within iSCSI connection object, keyed by InitiatorTaskTag.

### Conflict-02: GC Migration vs Active Write Path
- GC reads from a zone while normal I/O may be writing to a different zone
- If GC migrates data from Zone A while iSCSI writes new data to Zone A's L2P entry (before the buffer is flushed), the GC may migrate stale data
- RESOLUTION: ZTL must hold a zone-level reader lock during GC migration; iSCSI write path holds zone write lock during L2P update. GC must acquire write lock before modifying L2P.

### Conflict-03: Journal Size vs Performance
- Fsyncing journal on every write adds latency (fsync ~1-10ms on HDD)
- RESOLUTION: Group commit: batch multiple L2P updates into a single journal write + fsync
  - Configurable: sync_period_ms (default 10ms) groups writes before fsync
  - Trade-off: up to 10ms data loss on crash (acceptable for block device workloads)

---

## 3. Identified Gaps

### Gap-01: TRIM/UNMAP Support
- Windows NTFS may issue UNMAP (SCSI TRIM equivalent) for freed blocks
- Without UNMAP support, ZTL accumulates stale data, increasing GC pressure
- RECOMMENDATION: Implement UNMAP as L2P invalidation (mark segments as invalid without zeroing physical)
- PRIORITY: Medium (improves GC efficiency but not required for basic operation)

### Gap-02: Power Loss Protection
- SMR HDD may not complete in-flight write on power loss
- RECOMMENDATION: Write buffer flush must be followed by zone FINISH command to ensure write pointer is stable
- ACTION: Before acknowledging write to iSCSI, ensure zone data is fsynced on the SMR device

### Gap-03: Zone Reclaim Statistics Persistence
- GC statistics are in-memory; lost on restart
- RECOMMENDATION: Persist aggregate GC stats (zones reclaimed, bytes migrated) to a small stats file
- PRIORITY: Low (cosmetic, not functional)

### Gap-04: Multi-LUN Support
- Current design assumes single LUN per target
- RECOMMENDATION: Architect ZTL as per-LUN instance; server can host multiple targets/LUNs
- PRIORITY: Low for initial version, but keep interface clean for future extension

### Gap-05: Zero-copy Read Path
- Current ReadSectors returns []byte copy; for large reads (e.g., 1MB) this causes GC pressure
- RECOMMENDATION: Consider io.Reader / scatter-gather interface for read path
- PRIORITY: Medium (performance optimization)

---

## 4. Resolution Actions Summary

| ID | Issue | Action | Priority |
|----|-------|--------|---------|
| A-01 | MaxBurstLength vs buffer split | Reassemble at iSCSI layer before ZTL.Write | HIGH |
| A-02 | GC crash recovery with zone reset | Journal GC intent before zone reset | HIGH |
| A-03 | Recovery: reconcile write pointer | Call ReportZones after WAL replay | HIGH |
| A-04 | SG_IO Linux build tag | `//go:build linux` on smr package | HIGH |
| A-05 | PR command stub | Return ILLEGAL REQUEST for PR IN/OUT | MEDIUM |
| A-06 | GC/Write L2P race | Zone-level RW lock in ZTL | HIGH |
| A-07 | Journal group commit | Configurable sync_period_ms | MEDIUM |
| A-08 | UNMAP support | L2P invalidation for SCSI UNMAP | MEDIUM |
| A-09 | Zone FINISH before ACK | Flush path includes FINISH command | HIGH |
| A-10 | TypeScript type sync | Document manual sync process (or add tygo) | LOW |

---

## 5. Alignment Score

| Area | Status | Notes |
|------|--------|-------|
| iSCSI Protocol | ALIGNED | Core PDUs covered; PR stub needed |
| Zone Translation Layer | ALIGNED | L2P granularity decision made |
| GC Engine | ALIGNED | Race condition resolved with RW lock |
| Write-Ahead Journal | ALIGNED | Group commit resolves perf/safety trade-off |
| SMR Backend | ALIGNED | Linux build tag required |
| Crash Recovery | ALIGNED | ReportZones reconciliation step added |
| REST API | ALIGNED | All panels have data sources |
| Frontend | ALIGNED | API types must be kept in sync |
| Configuration | ALIGNED | All tunable parameters captured |

**Overall: READY FOR SPECIFICATION OUTPUT**
