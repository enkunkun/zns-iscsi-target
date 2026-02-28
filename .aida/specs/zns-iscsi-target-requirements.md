# ZNS/SMR iSCSI Target - Requirements Specification

## Document Info
- Version: 1.0
- Date: 2026-02-28
- Session: aida-zns-iscsi-20260228-135900
- Status: APPROVED FOR IMPLEMENTATION

---

## 1. Problem Statement

A SATA HDD with SMR (Shingled Magnetic Recording) in Host Managed mode enforces a sequential-write-only constraint per zone. Conventional operating systems (Windows, Linux without ZNS drivers) expect a block device that accepts random read/write at any logical block address. These constraints are fundamentally incompatible.

This project implements a **ZNS/SMR iSCSI Target** that:
1. Presents a conventional random-access block device to any iSCSI Initiator (primarily Windows).
2. Internally translates all I/O through a Zone Translation Layer (ZTL) that satisfies the sequential write constraint of the SMR device.
3. Maintains correctness across crashes through a write-ahead journal.
4. Exposes monitoring via REST API and a React dashboard.

---

## 2. Functional Requirements

### 2.1 iSCSI Target Server

**REQ-ISCSI-01**: The server MUST implement the iSCSI protocol (RFC 7143) at a level sufficient for Windows iSCSI Initiator compatibility.

**REQ-ISCSI-02**: The following iSCSI PDU types MUST be handled:
- Login Request / Login Response (all three stages: Security, Operational, FullFeature)
- Logout Request / Logout Response
- SCSI Command / SCSI Response
- Data-Out (write data from initiator to target)
- Data-In (read data from target to initiator)
- NOP-Out / NOP-In
- Task Management Function Request / Response
- Text Request / Text Response (iSCSI discovery)
- R2T (Ready to Transfer, for unsolicited data)
- Reject

**REQ-ISCSI-03**: The following Operational Text Parameters MUST be negotiated during Login:
- `MaxRecvDataSegmentLength` (default: 262144 bytes)
- `MaxBurstLength` (default: 16776192 bytes)
- `FirstBurstLength` (default: 65536 bytes)
- `ImmediateData` (default: Yes)
- `MaxOutstandingR2T` (default: 1)
- `HeaderDigest` (default: None; CRC32C optional)
- `DataDigest` (default: None; CRC32C optional)
- `InitialR2T` (default: Yes)
- `MaxConnections` (default: 1)

**REQ-ISCSI-04**: The target MUST support CHAP authentication (single-level CHAP per RFC 3720 Section 11.1) as an optional configuration. When disabled, no authentication is performed.

**REQ-ISCSI-05**: CmdSN/ExpCmdSN sequence numbering MUST be maintained to ensure correct command ordering and duplicate detection.

**REQ-ISCSI-06**: Data-Out PDUs for a single SCSI WRITE command MUST be reassembled in the iSCSI layer before passing the complete write buffer to the ZTL.

**REQ-ISCSI-07**: The target MUST present at least one LUN with a configurable IQN and portal address.

**REQ-ISCSI-08**: The target MUST support up to `max_sessions` (default: 4) concurrent iSCSI sessions.

### 2.2 SCSI Command Set

**REQ-SCSI-01**: The following SCSI commands MUST be implemented:
- INQUIRY (opcode 0x12): Standard inquiry + VPD pages 0x00, 0x80, 0x83
- READ CAPACITY (10) (opcode 0x25)
- READ CAPACITY (16) (opcode 0x9E/0x10)
- READ (6) (opcode 0x08)
- READ (10) (opcode 0x28)
- READ (16) (opcode 0x88)
- WRITE (6) (opcode 0x0A)
- WRITE (10) (opcode 0x2A)
- WRITE (16) (opcode 0x8A)
- TEST UNIT READY (opcode 0x00)
- SYNCHRONIZE CACHE (10) (opcode 0x35) - maps to ZTL.Flush
- REPORT SUPPORTED OPERATION CODES (opcode 0xA3/0x0C)

**REQ-SCSI-02**: The following SCSI commands MUST return an appropriate error (CHECK CONDITION, ILLEGAL REQUEST, ASC=0x20 INVALID COMMAND) to avoid Windows initiator stalling:
- PERSISTENT RESERVE IN / PERSISTENT RESERVE OUT
- REPORT LUNS (return single LUN 0)
- MODE SENSE (10) - return basic page 0x3F

**REQ-SCSI-03**: The INQUIRY response MUST report:
- Peripheral Device Type: 0x00 (Direct-access block device)
- Logical Block Size: 512 bytes
- Logical Block Count: total capacity / 512

**REQ-SCSI-04**: VPD page 0x83 (Device Identification) MUST include an EUI-64 or NAA identifier to satisfy Windows disk signature requirements.

**REQ-SCSI-05**: UNMAP (opcode 0x42) SHOULD be implemented as an L2P invalidation (mark segments invalid in ZTL without physically zeroing). This is optional for v1 but recommended to reduce GC pressure.

### 2.3 Zone Translation Layer (ZTL)

**REQ-ZTL-01**: The ZTL MUST maintain a Logical-to-Physical (L2P) mapping table that maps each logical segment (default 8 KB) to a physical zone address.

**REQ-ZTL-02**: The L2P table MUST be held entirely in memory for low read latency. The segment size MUST be configurable (4 KB, 8 KB, 16 KB) to control RAM usage.

**REQ-ZTL-03**: The ZTL MUST maintain a Physical-to-Logical (P2L) reverse mapping for GC victim data migration.

**REQ-ZTL-04**: The ZTL MUST implement a write buffer that:
- Accumulates writes keyed by zone ID
- Flushes a zone's buffer when it reaches zone capacity
- Flushes on a configurable dirty data age timeout (default: 5 seconds)
- Flushes under GC pressure (when free zones fall below threshold)
- Respects zone sequential write order (no out-of-order writes within a zone)

**REQ-ZTL-05**: The ZTL MUST implement a zone state machine with states: EMPTY, IMPLICIT_OPEN, EXPLICIT_OPEN, CLOSED, FULL, READ_ONLY, OFFLINE. State transitions MUST follow ZBC/ZAC specification.

**REQ-ZTL-06**: The ZTL MUST track the number of simultaneously open zones and MUST NOT exceed the device's `max_open_zones` limit.

**REQ-ZTL-07**: Before acknowledging a WRITE to the iSCSI initiator, the ZTL MUST ensure the data is written to stable storage (either flushed to the SMR device zone with fsync, or committed to the WAL journal).

**REQ-ZTL-08**: The ZTL MUST implement zone allocation to assign free (EMPTY) zones to incoming write streams.

### 2.4 Garbage Collector (GC)

**REQ-GC-01**: The GC MUST run as a background goroutine and MUST NOT block foreground I/O beyond a configurable latency target.

**REQ-GC-02**: GC MUST be triggered when the ratio of free zones to total zones falls below a configurable threshold (default: 0.20).

**REQ-GC-03**: GC MUST enter emergency mode when free zones fall below a configurable absolute count (default: 3). In emergency mode, GC runs synchronously before any new write is accepted.

**REQ-GC-04**: Victim zone selection MUST use the lowest valid_blocks/zone_capacity ratio (most stale data = best GC candidate).

**REQ-GC-05**: GC MUST:
1. Select victim zone
2. Read all valid segments from victim zone (consult L2P/P2L)
3. Write valid segments to a fresh zone
4. Update L2P table atomically
5. Journal the GC operation before zone reset
6. Reset victim zone (send Zone Reset command to device)

**REQ-GC-06**: GC operations MUST be journaled such that a crash mid-GC can be recovered without data loss.

**REQ-GC-07**: GC statistics (zones reclaimed, bytes migrated, current free zone count) MUST be exposed via the REST API and Prometheus metrics.

**REQ-GC-08**: The GC MUST be manually triggerable via the REST API (POST /api/v1/gc/trigger).

### 2.5 Zoned Device Backend

**REQ-BACKEND-01**: All device operations MUST go through the `ZonedDevice` interface, allowing the backend to be swapped without changes to ZTL.

**REQ-BACKEND-02 (In-Memory Emulator)**: An in-memory emulator MUST be provided for development and testing with:
- Configurable zone count (default: 64)
- Configurable zone size in MB (default: 256 MB)
- Configurable max open zones (default: 14)
- Strict sequential write enforcement (write to non-write-pointer LBA returns error)
- Zone state machine simulation

**REQ-BACKEND-03 (SMR Backend)**: A real device backend MUST be provided for Linux, using SG_IO ioctl (SCSI passthrough) to issue:
- ZBC REPORT ZONES (opcode 0x95) for zone discovery
- ZBC ZONE ACTION (opcode 0x9F) with action codes:
  - 0x01 CLOSE ZONE
  - 0x02 FINISH ZONE
  - 0x03 OPEN ZONE
  - 0x04 RESET WRITE POINTER
- SCSI READ (10)/(16) for data reads
- SCSI WRITE (10)/(16) for sequential data writes

**REQ-BACKEND-04**: The SMR backend MUST compile only on Linux (build tag `//go:build linux`). The server MUST return a clear error on non-Linux systems if the SMR backend is configured.

**REQ-BACKEND-05**: Zone size MUST be detected at runtime via REPORT ZONES rather than hardcoded.

### 2.6 Write-Ahead Journal

**REQ-WAL-01**: The WAL MUST record every L2P table update before it is applied to the in-memory table.

**REQ-WAL-02**: WAL records MUST include a CRC32 checksum for corruption detection.

**REQ-WAL-03**: The WAL MUST support group commit: multiple L2P updates MAY be batched into a single fsync to reduce write latency. The batch period MUST be configurable (default: 10 ms).

**REQ-WAL-04**: The WAL MUST support checkpointing: a complete L2P snapshot written to stable storage, after which WAL entries before the checkpoint LSN may be discarded.

**REQ-WAL-05**: On startup, the system MUST perform crash recovery:
1. Load the most recent valid checkpoint.
2. Replay WAL entries with LSN > checkpoint LSN.
3. Call ReportZones on the device to reconcile write pointers.
4. Resume normal operation.

**REQ-WAL-06**: The WAL file MUST be bounded in size by compaction (triggered when WAL exceeds configurable max_size_mb).

### 2.7 REST Monitoring API

**REQ-API-01**: The REST API MUST be served on a configurable address (default: 0.0.0.0:8080) using the Chi router.

**REQ-API-02**: The following endpoints MUST be implemented:
- `GET /api/v1/zones` - list all zones with state, write pointer, valid block count, GC score
- `GET /api/v1/stats` - iSCSI I/O stats, GC stats, buffer stats, journal stats
- `GET /api/v1/health` - liveness probe returning `{"status":"ok"}`
- `POST /api/v1/gc/trigger` - trigger a manual GC run
- `GET /api/v1/config` - return effective configuration (secrets redacted)
- `GET /metrics` - Prometheus exposition format

**REQ-API-03**: All API responses MUST use `Content-Type: application/json` and return appropriate HTTP status codes.

**REQ-API-04**: The API server MUST run in a separate goroutine and MUST NOT affect iSCSI target availability.

### 2.8 React Dashboard

**REQ-UI-01**: The dashboard MUST display:
- Device info panel (device path/type, zone size, zone count, total capacity)
- Zone heatmap (2D grid, color-coded by zone state and valid block ratio)
- GC statistics panel (running state, zones reclaimed, bytes migrated, free zone count)
- iSCSI connection panel (active sessions, I/O counters, latency p99)
- Write buffer occupancy (dirty bytes / max bytes)

**REQ-UI-02**: The dashboard MUST auto-refresh stats every 1 second and zones every 5 seconds.

**REQ-UI-03**: The zone heatmap MUST allow hover-over to display zone details (ID, state, write pointer, valid%).

**REQ-UI-04**: The frontend MUST be built with React + TypeScript + Vite.

**REQ-UI-05**: The built frontend artifacts MUST be served by the Go binary itself (embedded via `embed.FS`) or as a separate Docker service.

### 2.9 Configuration & Operations

**REQ-CFG-01**: The system MUST be configurable via a YAML configuration file. All settings MUST have reasonable defaults.

**REQ-CFG-02**: Configuration file path MUST be specifiable via `--config` CLI flag.

**REQ-CFG-03**: The Docker image MUST use a multi-stage build: Go builder + minimal runtime image (distroless or alpine).

**REQ-CFG-04**: A `docker-compose.yml` MUST provide a development environment with both the target and dashboard services.

---

## 3. Non-Functional Requirements

**REQ-NFR-01 (Performance)**: Read latency from the ZTL MUST NOT add more than 1 ms overhead over the raw device read latency (L2P lookup must be O(1)).

**REQ-NFR-02 (Performance)**: Write throughput MUST achieve at least 80% of the device's sequential write bandwidth when the write buffer is active.

**REQ-NFR-03 (Reliability)**: No L2P data loss for writes acknowledged to the iSCSI initiator, assuming the WAL is fsynced before acknowledgment.

**REQ-NFR-04 (Reliability)**: The system MUST be able to recover to a consistent state after a crash at any point during normal operation or GC.

**REQ-NFR-05 (Scalability)**: L2P table RAM usage MUST scale predictably: (TotalCapacitySectors / SectorsPerSegment) * 8 bytes. For a 4 TB device with 8 KB segments: ~4 GB RAM.

**REQ-NFR-06 (Compatibility)**: The iSCSI target MUST be compatible with the Windows iSCSI Initiator (Windows 10 / Windows Server 2019+) for GPT + NTFS disk initialization and usage.

**REQ-NFR-07 (Observability)**: All key operational metrics MUST be exposed via Prometheus. Log level MUST be configurable.

**REQ-NFR-08 (Portability)**: The Go codebase MUST compile on Linux (amd64) as the primary target. The emulator backend MUST compile on macOS and Linux for development.

---

## 4. Explicit Exclusions (v1.0 Scope)

- Multi-LUN targets (single LUN per target for v1)
- iSCSI jumbo frame / jumbo packet support
- NVMe ZNS device support (SATA SMR only for real device)
- Write-back cache to DRAM beyond the write buffer
- Replication or DRBD-style mirroring
- ALUA (Asymmetric Logical Unit Access)
- Active-Active multipath
- iSCSI offload (TOE) support
