# ZNS/SMR iSCSI Target - Implementation Tasks

## Document Info
- Version: 1.0
- Date: 2026-02-28
- Session: aida-zns-iscsi-20260228-135900
- Status: READY FOR IMPLEMENTATION

---

## Overview

Tasks are organized into phases. Each phase produces working, tested code before the next phase begins. All backend tasks use TDD (write test first, then implementation). All tasks are for Go unless marked [React] or [Docker].

**Total estimated tasks: 47**

---

## Phase A: Foundation (Prerequisite for all other phases)

### A-01: Project Scaffolding
- Initialize Go module: `go mod init github.com/yourorg/zns-iscsi-target`
- Create directory structure as per design (cmd/, internal/, pkg/, web/, tests/, docker/)
- Add go.sum with dependencies: chi, prometheus/client_golang, yaml.v3, testify, golang.org/x/sys/unix
- Create Makefile with targets: build, test, lint, docker-build
- Create `.golangci.yml` lint configuration
- **Acceptance**: `go build ./...` succeeds; `go test ./...` outputs "no test files"

### A-02: Configuration Package
- `internal/config/config.go`: Config struct with all fields from design Section 7
- `internal/config/defaults.go`: Default values
- Load from YAML file + CLI flag override
- **Test**: Parse valid config, parse config with missing optional fields (use defaults), parse invalid YAML returns error
- **Acceptance**: `go test ./internal/config/...` passes

### A-03: Package Types (pkg/zbc)
- `pkg/zbc/types.go`: ZoneState, ZoneType, ZoneDescriptor structs
- `pkg/zbc/constants.go`: ZBC opcode constants, zone state constants, zone condition constants
- **Acceptance**: no tests needed (pure type definitions); compiles cleanly

---

## Phase B: Zoned Device Backend

### B-01: Backend Interface
- `internal/backend/backend.go`: `ZonedDevice` interface (design Section 3.3)
- Include all methods: ReportZones, ZoneCount, ZoneSize, OpenZone, CloseZone, FinishZone, ResetZone, ReadSectors, WriteSectors, BlockSize, Capacity, MaxOpenZones, Close
- **Acceptance**: compiles; used as contract for B-02 and B-03

### B-02: In-Memory Emulator
- `internal/backend/emulator/emulator.go`: Implement ZonedDevice
  - State: `[]zoneState` with write pointer, zone state, and data buffer per zone
  - ReportZones: return all zone descriptors
  - WriteSectors: enforce sequential write (LBA must equal write pointer); advance pointer
  - ReadSectors: read from zone buffer
  - Zone state machine: Empty->Open->Full; Open->Closed->Open; Full requires Reset before reuse
  - MaxOpenZones enforcement: implicit open when write crosses zone boundary
- `internal/backend/emulator/emulator_test.go`:
  - Test sequential write enforcement (write at wrong LBA returns ErrOutOfOrder)
  - Test zone capacity fill leads to FULL state
  - Test ResetZone returns zone to EMPTY state
  - Test MaxOpenZones limit (14th implicit open fails with ErrTooManyOpenZones)
  - Test cross-zone read/write
- **Acceptance**: `go test ./internal/backend/emulator/...` passes with 100% statement coverage

### B-03: SATA SMR Backend (Linux only)
- `internal/backend/smr/sgioctl.go`: SG_IO ioctl wrapper
  - `func sgio(fd int, hdr *sgIOHdr) error`
  - Use `syscall.Syscall` with `SG_IO = 0x2285` ioctl number
- `internal/backend/smr/zbc.go`: ZBC command builders
  - `ReportZones(fd, startLBA, allocLen)` -> `[]ZoneDescriptor`
  - `ZoneAction(fd, action, startLBA, all bool)` -> error
  - Parse binary zone descriptor response (64-byte fixed format)
- `internal/backend/smr/smr.go`: Implement ZonedDevice using sgioctl + zbc
  - ReadSectors: SCSI READ(16)
  - WriteSectors: SCSI WRITE(16)
- Build tag: `//go:build linux`
- `internal/backend/smr/smr_test.go`:
  - Mock fd-based test for ZBC binary encoding/decoding
  - Test ZoneDescriptor parsing from known binary fixture
- **Acceptance**: `go test -tags linux ./internal/backend/smr/...` passes

---

## Phase C: Zone Translation Layer

### C-01: Segment Address Encoding
- `internal/ztl/segment.go`:
  - `PhysAddr` type (uint64), encode/decode ZoneID + OffsetSectors
  - `SegmentID` type (uint64)
  - Helper: `LBAToSegmentID(lba, sectorsPerSegment)`
  - Helper: `SegmentToLBARange(segID, sectorsPerSegment)`
- **Test**: encode then decode round-trip, boundary values, max zone ID
- **Acceptance**: `go test ./internal/ztl/...` passes

### C-02: L2P Table
- `internal/ztl/l2p.go`:
  - `L2PTable` struct: `entries []atomic.Uint64`
  - `New(totalSegments uint64) *L2PTable`
  - `Get(segID uint64) PhysAddr` (atomic load)
  - `Set(segID uint64, phys PhysAddr)` (atomic store)
  - `CAS(segID uint64, old, new PhysAddr) bool` (atomic compare-and-swap)
  - `Snapshot() []PhysAddr` (for checkpoint)
  - `LoadSnapshot([]PhysAddr)` (for recovery)
- **Test**: concurrent reads during single write (race detector), CAS conflict simulation
- **Acceptance**: passes `go test -race ./internal/ztl/...`

### C-03: P2L Reverse Map
- `internal/ztl/p2l.go`:
  - `P2LMap` struct: `entries map[PhysAddr]uint64` (phys -> segID)
  - `Set(phys PhysAddr, segID uint64)` with mutex
  - `Get(phys PhysAddr) (uint64, bool)`
  - `Delete(phys PhysAddr)`
  - `IterateZone(zoneID uint32, fn func(offset uint32, segID uint64))` for GC scan
- **Test**: set/get, delete, iterate zone
- **Acceptance**: `go test ./internal/ztl/...` passes

### C-04: Zone Manager
- `internal/ztl/zone_manager.go`:
  - `ZoneManager` struct: `[]ZoneInfo`, `openZones map[uint32]struct{}`, `freeList []uint32`
  - `Initialize(device ZonedDevice)`: populate from ReportZones
  - `AllocateFree() (ZoneID uint32, err error)`: pop from freeList, open zone on device
  - `GetOrOpen(zoneID uint32)`: ensure zone is open (LRU eviction if at limit)
  - `MarkFull(zoneID uint32)`: update state, send FINISH to device
  - `MarkEmpty(zoneID uint32)`: push back to freeList
  - `Freeze(zoneID uint32) / Unfreeze(zoneID uint32)`: GC freeze support
  - `FreeZoneCount() int`
  - `OpenZoneCount() int`
- **Test**: allocate up to maxOpenZones, verify LRU close eviction, freeze prevents allocation
- **Acceptance**: `go test ./internal/ztl/...` passes

### C-05: Write Buffer
- `internal/ztl/buffer.go`:
  - `WriteBuffer` struct: per-zone byte buffer, size tracking
  - `Add(zoneID uint32, data []byte, writePointer uint64) error`
  - `Lookup(zoneID uint32, offsetSectors uint32, length uint32) ([]byte, bool)`: read-from-buffer
  - `Flush(zoneID uint32, device ZonedDevice, zm *ZoneManager) error`: sequential write to device
  - `FlushAll(device ZonedDevice, zm *ZoneManager) error`
  - Background flush goroutine: age-based (configurable timeout) + size-based trigger
- **Test**: add data, lookup hit, lookup miss, flush writes to mock device in sequential order, buffer miss after flush
- **Acceptance**: `go test ./internal/ztl/...` passes

### C-06: GC Statistics
- `internal/ztl/gc_stats.go`:
  - `GCStats` struct with atomic counters: ZonesReclaimed, BytesMigrated, RunCount
  - `GCStatsSnapshot` for API response serialization
- **Acceptance**: compiles; used by C-07

### C-07: GC Engine
- `internal/ztl/gc.go`:
  - `GCEngine` struct
  - `Start(ctx context.Context)`: launch background goroutine
  - `Stop()`: signal goroutine and wait
  - `TriggerManual()`: channel-based trigger from API
  - Background loop: monitor free zone ratio, select victim, migrate, reset
  - Use L2P.CAS for atomic migration (GC vs foreground race handling)
  - Journal GC intent before zone reset
- **Test**: mock device + emulator; run write until GC triggers; verify data integrity after GC; verify zone reset called on victim; CAS race simulation
- **Acceptance**: `go test ./internal/ztl/...` passes with -race

### C-08: ZTL Orchestrator
- `internal/ztl/ztl.go`:
  - `ZTL` struct: owns L2P, P2L, WriteBuffer, ZoneManager, GCEngine, Journal
  - `Read(lba uint64, count uint32) ([]byte, error)`
  - `Write(lba uint64, data []byte) error`
  - `Flush() error`
  - `Unmap(lba uint64, count uint32) error`
  - `Close() error`
  - `New(cfg *config.Config, device backend.ZonedDevice, journal *journal.Journal) (*ZTL, error)`
- **Test**: full write-then-read roundtrip; write past zone boundary (crosses to next zone); write amplification check; unmap followed by read returns zeros
- **Acceptance**: `go test ./internal/ztl/...` passes (all sub-packages including C-01 through C-07)

---

## Phase D: Write-Ahead Journal

### D-01: WAL Record Encoding
- `internal/journal/record.go`:
  - WAL record structs and binary encode/decode
  - CRC32C computation
  - `MarshalBinary() []byte`, `UnmarshalBinary([]byte) error`
- **Test**: encode then decode round-trip for each record type; CRC corruption detection
- **Acceptance**: `go test ./internal/journal/...` passes

### D-02: WAL Journal
- `internal/journal/journal.go`:
  - `Journal` struct: file handle, LSN counter, pending batch
  - `Open(path string) (*Journal, error)`
  - `LogL2PUpdate(segID, oldPhys, newPhys PhysAddr) (LSN uint64, error)`
  - `LogZoneAction(action RecordType, zoneID uint32) error`
  - `GroupCommit() error`: fsync batch, reset pending
  - `StartGroupCommitLoop(ctx context.Context, syncPeriodMs int)`
- **Test**: write 100 records, read back in order; group commit batches correctly
- **Acceptance**: `go test ./internal/journal/...` passes

### D-03: Checkpoint
- `internal/journal/checkpoint.go`:
  - `WriteCheckpoint(j *Journal, l2p *L2PTable) error`
  - `ReadLatestCheckpoint(path string) (l2pSnapshot []PhysAddr, checkpointLSN uint64, error)`
- **Test**: write checkpoint, read back, verify L2P snapshot matches
- **Acceptance**: `go test ./internal/journal/...` passes

### D-04: Crash Recovery
- `internal/journal/recovery.go`:
  - `Recover(j *Journal, l2p *L2PTable, zm *ZoneManager, device backend.ZonedDevice) error`
  - Load checkpoint, replay WAL entries, reconcile write pointers via ReportZones
- **Test**: simulate crash at 3 different points (before WAL, after WAL, after flush); verify recovery restores consistent state
- **Acceptance**: `go test ./internal/journal/...` passes

---

## Phase E: SCSI Command Layer

### E-01: Sense Data Builder
- `internal/scsi/sense.go`:
  - `BuildSense(key, asc, ascq byte) []byte`
  - Common pre-built sense buffers: NO_SENSE, ILLEGAL_REQUEST, MEDIUM_ERROR, NOT_READY
- **Acceptance**: `go test ./internal/scsi/...` passes

### E-02: INQUIRY Handler
- `internal/scsi/inquiry.go`:
  - Standard INQUIRY response: device type=0x00, version=0x05 (SPC-3), block size=512
  - VPD page 0x00: supported pages list
  - VPD page 0x80: unit serial number
  - VPD page 0x83: device identification (NAA type 6)
- **Test**: parse INQUIRY CDB, verify response byte layout, verify VPD pages
- **Acceptance**: `go test ./internal/scsi/...` passes

### E-03: READ CAPACITY Handler
- `internal/scsi/readcap.go`:
  - READ CAPACITY (10): return LBA count and block size (512)
  - READ CAPACITY (16): same with 64-bit LBA
- **Test**: verify big-endian encoding of capacity values
- **Acceptance**: `go test ./internal/scsi/...` passes

### E-04: READ Handler
- `internal/scsi/read.go`:
  - Handle READ(6), READ(10), READ(16)
  - Extract LBA and transfer length from CDB
  - Call ZTL.Read, return data
- **Test**: READ(10) with known LBA, verify returned data matches what was written (integration with mock ZTL)
- **Acceptance**: `go test ./internal/scsi/...` passes

### E-05: WRITE Handler
- `internal/scsi/write.go`:
  - Handle WRITE(6), WRITE(10), WRITE(16)
  - Call ZTL.Write with reassembled data
- **Test**: WRITE(10) followed by READ(10), verify roundtrip
- **Acceptance**: `go test ./internal/scsi/...` passes

### E-06: SYNCHRONIZE CACHE + Stub Handlers
- `internal/scsi/sync.go`: SYNCHRONIZE CACHE calls ZTL.Flush
- `internal/scsi/unmap.go`: UNMAP calls ZTL.Unmap
- Stub handlers for: TEST UNIT READY, REPORT LUNS, MODE SENSE (10), PERSISTENT RESERVE IN/OUT
- **Test**: each stub returns correct sense code
- **Acceptance**: `go test ./internal/scsi/...` passes

### E-07: SCSI Command Dispatcher
- `internal/scsi/handler.go`:
  - `Handler` struct with ZTL reference
  - `Execute(cdb []byte, dataOut []byte) (dataIn []byte, status byte, sense []byte, error)`
  - Route by CDB byte 0 (opcode) to handlers E-02 through E-06
  - Unknown opcode: CHECK CONDITION + ILLEGAL REQUEST sense
- **Test**: all known opcodes dispatch correctly; unknown opcode returns correct error
- **Acceptance**: `go test ./internal/scsi/...` passes

---

## Phase F: iSCSI Protocol

### F-01: PDU Types
- `internal/iscsi/pdu.go`:
  - `BHS` struct (48 bytes, big-endian fields)
  - `PDU` struct: BHS + AHS bytes + DataSegment bytes
  - `ReadPDU(conn net.Conn) (*PDU, error)`: read BHS, AHS, DataSegment with padding
  - `WritePDU(conn net.Conn, pdu *PDU) error`: write with digest if negotiated
  - Opcode constants
- **Test**: read known binary PDU bytes; write PDU and compare to expected bytes
- **Acceptance**: `go test ./internal/iscsi/...` passes

### F-02: Parameter Negotiation
- `internal/iscsi/params.go`:
  - `Params` struct: MaxRecvDataSegmentLength, MaxBurstLength, ImmediateData, etc.
  - `ParseKeyValuePairs(data []byte) map[string]string`
  - `SerializeKeyValuePairs(kv map[string]string) []byte`
  - `Negotiate(initiator, target Params) Params`: apply RFC 7143 negotiation rules
- **Test**: negotiate MaxBurstLength (min of two), negotiate ImmediateData (AND logic), unknown key passthrough
- **Acceptance**: `go test ./internal/iscsi/...` passes

### F-03: Login Handler
- `internal/iscsi/login.go`:
  - Handle Security stage: CHAP or bypass
  - Handle Operational stage: parameter negotiation
  - Transition to FullFeature
- `internal/iscsi/chap.go`: CHAP challenge/response (if auth enabled)
- **Test**: login with no auth; login with CHAP; login with wrong credentials
- **Acceptance**: `go test ./internal/iscsi/...` passes

### F-04: Connection & Session
- `internal/iscsi/connection.go`:
  - `Connection` struct: net.Conn, negotiated params, CmdSN tracking, reassembly buffers
  - Read loop: `ReadPDU` then route by opcode
  - Write queue: serialize response PDUs
- `internal/iscsi/session.go`:
  - `Session` struct: ISID, TSID, connection list
  - `SessionManager`: map[TSID]*Session, max sessions enforcement
- **Test**: connection close on read error; CmdSN out-of-order detection
- **Acceptance**: `go test ./internal/iscsi/...` passes

### F-05: SCSI Command PDU + Data-Out Reassembly
- `internal/iscsi/scsi_cmd.go`:
  - Parse SCSI Command PDU: extract CDB, ITT, Expected Data Transfer Length
  - If write: initiate Data-Out reassembly or ImmediateData path
  - Call SCSI dispatcher with assembled data
  - Send SCSI Response PDU
- `internal/iscsi/data_out.go`:
  - Reassembly buffer keyed by ITT
  - Handle multiple Data-Out PDUs per command (DataSN ordering)
  - On F-bit: trigger SCSI dispatch
- **Test**: single PDU write; multi-PDU write reassembly; read command with Data-In split
- **Acceptance**: `go test ./internal/iscsi/...` passes

### F-06: Data-In, NOP, TMF, Logout
- `internal/iscsi/data_in.go`: split large read data into multiple Data-In PDUs
- `internal/iscsi/nop.go`: NOP-Out -> NOP-In response
- `internal/iscsi/tmf.go`: Task Management (ABORT TASK, LUN RESET) stubs returning FUNCTION COMPLETE
- `internal/iscsi/logout.go`: Logout request -> close connection gracefully
- **Test**: NOP round-trip; logout closes connection without error
- **Acceptance**: `go test ./internal/iscsi/...` passes

### F-07: iSCSI Target Server
- `internal/iscsi/server.go`:
  - `Server` struct: TCPListener, SessionManager, SCSI Handler, config
  - `Listen() error`: accept loop, goroutine per connection
  - `Shutdown(ctx context.Context) error`: graceful shutdown
- `internal/iscsi/target.go`: Target metadata (IQN, LUN list)
- `internal/iscsi/text.go`: Text PDU for iSCSI discovery (SendTargets)
- **Test**: start server, connect, complete login, send NOP, logout, verify clean shutdown
- **Acceptance**: `go test ./internal/iscsi/...` passes

---

## Phase G: REST API

### G-01: API Types
- `internal/api/types.go`: JSON response structs for all endpoints
  - `ZoneListResponse`, `ZoneDetail`, `StatsResponse`, `ISCSIStats`, `GCStats`, `BufferStats`, `JournalStats`, `HealthResponse`, `ConfigResponse`
- **Acceptance**: compiles; used by G-02

### G-02: Prometheus Metrics
- `internal/api/metrics.go`:
  - Register all Prometheus metrics from design Section 2.6.2
  - `MetricsCollector` struct with Update method
  - Collect from iSCSI session stats, ZTL stats
- **Test**: register metrics, update values, scrape and verify exposition format
- **Acceptance**: `go test ./internal/api/...` passes

### G-03: Route Handlers
- `internal/api/handlers.go`:
  - `Handler` struct referencing ZTL, iSCSI server, config
  - `ListZones`: query ZoneManager, serialize response
  - `GetStats`: aggregate from all components
  - `Health`: return 200 OK
  - `TriggerGC`: call GC.TriggerManual()
  - `GetConfig`: serialize config (redact CHAP secret)
- **Test**: HTTP test for each handler; verify JSON structure and status codes
- **Acceptance**: `go test ./internal/api/...` passes

### G-04: API Server
- `internal/api/server.go`:
  - Chi router setup with middleware
  - Serve embedded React app via `embed.FS` from `/web/dist`
  - `Start(ctx context.Context)` and `Shutdown()`
- **Test**: router setup; 404 for unknown paths returns JSON error
- **Acceptance**: `go test ./internal/api/...` passes

---

## Phase H: Main Entry Point

### H-01: Wire Components
- `cmd/zns-iscsi/main.go`:
  - Parse `--config` flag
  - Load config, apply defaults
  - Create backend (emulator or SMR based on config)
  - Open journal, run crash recovery
  - Create ZTL with GC engine
  - Create SCSI handler
  - Create iSCSI target server
  - Create REST API server
  - Start all servers
  - Handle SIGTERM/SIGINT for graceful shutdown
- **Acceptance**: `go build ./cmd/zns-iscsi && ./zns-iscsi --config config.yaml.example` starts without panic

---

## Phase I: React Dashboard

### I-01: Project Setup [React]
- Initialize Vite project in `/web/` with React + TypeScript template
- Install dependencies: no external UI library needed (use CSS grid for zone map)
- Configure Vite to proxy `/api/v1/` to `localhost:8080` in dev mode
- **Acceptance**: `npm run dev` starts dashboard at localhost:5173

### I-02: API Client + Types [React]
- `web/src/types/api.ts`: TypeScript types matching Go API response types
- `web/src/api/client.ts`: fetch wrapper with base URL, error handling
- `web/src/hooks/useAPI.ts`: polling hook `usePolling(url, intervalMs)`
- **Acceptance**: TypeScript compiles without errors

### I-03: Zone Heatmap Component [React]
- `web/src/components/ZoneMap.tsx`:
  - Grid of colored cells (CSS Grid auto-fit)
  - Color coding: EMPTY=gray, OPEN=blue, FULL=red, CLOSED=yellow
  - Cell opacity proportional to GC score (darker = more valid data)
  - Hover tooltip: zone ID, state, write pointer %, valid%
  - Polls /api/v1/zones every 5 seconds
- **Acceptance**: renders 64+ zone cells correctly; tooltip shows on hover

### I-04: Stats Panels [React]
- `web/src/components/GCStats.tsx`: free zones / total zones, reclaimed count, migrated bytes, running indicator
- `web/src/components/ISCSIPanel.tsx`: active sessions, read/write IOPS, latency p99
- `web/src/components/BufferGauge.tsx`: horizontal progress bar dirty/max
- `web/src/components/DeviceInfo.tsx`: device path, zone size, zone count, total capacity
- All poll /api/v1/stats every 1 second
- **Acceptance**: all panels render with mock data; no TypeScript errors

### I-05: App Layout [React]
- `web/src/App.tsx`: top-level layout with all panels
- Responsive CSS Grid layout: sidebar (DeviceInfo + ISCSIPanel), main (ZoneMap), bottom (GCStats + BufferGauge)
- **Acceptance**: `npm run build` succeeds; `dist/` directory contains bundled assets

---

## Phase J: Docker & Integration

### J-01: Dockerfile
- `docker/Dockerfile`: multi-stage build as per design Section 8.1
- Build React dashboard in stage 2, embed result path for Go binary (or copy to static dir)
- **Acceptance**: `docker build -f docker/Dockerfile -t zns-iscsi .` succeeds

### J-02: docker-compose
- `docker-compose.yml`: target service + optional web service (if not embedded)
- Development override: `docker-compose.override.yml` for hot-reload with air
- **Acceptance**: `docker-compose up` starts target; `curl localhost:8080/api/v1/health` returns 200

### J-03: Integration Test: Full Write-Read-GC Cycle
- `tests/integration/ztl_test.go`:
  - Use emulator backend
  - Write 32 MB of random data at random LBAs
  - Read back all data and verify checksums
  - Fill device to 80% capacity
  - Trigger GC
  - Verify data integrity post-GC
- **Acceptance**: `go test ./tests/integration/...` passes without -race errors

### J-04: Integration Test: iSCSI Protocol
- `tests/integration/iscsi_test.go`:
  - Start iSCSI target in goroutine
  - Open TCP connection, manually perform iSCSI login
  - Send SCSI INQUIRY, verify response
  - Send SCSI WRITE(10) at LBA 0, then SCSI READ(10) at LBA 0
  - Verify data matches
  - Send Logout
- **Acceptance**: `go test ./tests/integration/...` passes

---

## Phase K: Quality Gates

### K-01: Code Coverage
- Run `go test -coverprofile=coverage.out ./...`
- Generate HTML report: `go tool cover -html=coverage.out`
- **Target**: >= 80% statement coverage on non-main packages
- **Acceptance**: coverage report generated; threshold met

### K-02: Race Detector
- Run `go test -race ./...`
- **Acceptance**: no data race warnings

### K-03: Lint
- Run `golangci-lint run`
- Fix any errors or explicitly suppress with justification
- **Acceptance**: zero lint errors

### K-04: Build Matrix
- `go build -tags linux ./...` on Linux
- `go build ./...` on macOS (SMR package excluded by build tag)
- `npm run build` in /web/
- `docker build -f docker/Dockerfile .`
- **Acceptance**: all four build commands succeed

---

## Task Summary

| Phase | Tasks | Description |
|-------|-------|-------------|
| A | 3 | Foundation, config, types |
| B | 3 | Zoned device backends (emulator + SMR) |
| C | 8 | Zone Translation Layer |
| D | 4 | Write-Ahead Journal |
| E | 7 | SCSI command layer |
| F | 7 | iSCSI protocol |
| G | 4 | REST API |
| H | 1 | Main entry point |
| I | 5 | React dashboard |
| J | 4 | Docker + integration tests |
| K | 4 | Quality gates |
| **Total** | **50** | |

---

## Implementation Priority (First Vertical Slice)

To get a working demo quickly, implement in this order:
1. A-01, A-02, A-03 (scaffolding)
2. B-01, B-02 (emulator backend only)
3. C-01 through C-08 (full ZTL with emulator)
4. D-01 through D-04 (journal)
5. E-01 through E-07 (SCSI layer)
6. F-01 through F-07 (iSCSI protocol)
7. H-01 (wire everything together)

At this point: Windows iSCSI Initiator can connect to the target using the in-memory emulator, and data is durable across restarts via the journal.

Then continue with:
8. B-03 (SMR backend for real device)
9. G-01 through G-04 (REST API)
10. I-01 through I-05 (dashboard)
11. J-01, J-02 (Docker)
12. J-03, J-04, K-01 through K-04 (quality gates)
