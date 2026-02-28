# Phase 2: Structure

## Session
- Session ID: aida-zns-iscsi-20260228-135900
- Phase: 2 - Structure
- Date: 2026-02-28

---

## 1. Directory Structure

```
zns-iscsi-target/
├── cmd/
│   └── zns-iscsi/
│       └── main.go                    # Entry point: parse config, wire components, start server
│
├── internal/
│   ├── iscsi/                         # iSCSI protocol implementation
│   │   ├── server.go                  # TCP listener, session accept loop
│   │   ├── session.go                 # iSCSI session state machine
│   │   ├── connection.go              # iSCSI connection (one TCP conn per iSCSI connection)
│   │   ├── pdu.go                     # PDU structs: BHS + AHS + data segment
│   │   ├── login.go                   # Login phase PDU handling + parameter negotiation
│   │   ├── text.go                    # Text request/response (discovery)
│   │   ├── scsi_cmd.go                # SCSI command PDU dispatch
│   │   ├── data_out.go                # Data-Out PDU handling (write data from initiator)
│   │   ├── data_in.go                 # Data-In PDU handling (read data to initiator)
│   │   ├── nop.go                     # NOP-In / NOP-Out PDU
│   │   ├── tmf.go                     # Task Management Function PDU
│   │   ├── logout.go                  # Logout PDU
│   │   ├── params.go                  # Operational text parameter negotiation constants
│   │   ├── target.go                  # Target metadata (IQN, LUN list)
│   │   └── chap.go                    # CHAP authentication (optional)
│   │
│   ├── scsi/                          # SCSI command set emulation
│   │   ├── handler.go                 # Dispatcher: opcode -> handler function
│   │   ├── inquiry.go                 # INQUIRY command (VPD pages 0x00, 0x80, 0x83, 0xB1)
│   │   ├── readcap.go                 # READ CAPACITY (10) and (16)
│   │   ├── read.go                    # READ (6/10/16) -> ZTL.Read
│   │   ├── write.go                   # WRITE (6/10/16) -> ZTL.Write
│   │   ├── sync.go                    # SYNCHRONIZE CACHE -> ZTL.Flush
│   │   ├── unmap.go                   # UNMAP (TRIM) -> ZTL.Unmap (optional)
│   │   └── sense.go                   # SCSI sense data builder
│   │
│   ├── ztl/                           # Zone Translation Layer
│   │   ├── ztl.go                     # ZTL struct, Read/Write/Flush interface
│   │   ├── l2p.go                     # L2P table: logical segment -> physical address
│   │   ├── p2l.go                     # P2L reverse map for GC
│   │   ├── buffer.go                  # Write buffer: coalescing, dirty tracking, flush
│   │   ├── zone_manager.go            # Zone state machine, open zone tracking, allocation
│   │   ├── gc.go                      # GC engine: victim selection, migration, reclamation
│   │   ├── gc_stats.go                # GC statistics counters
│   │   └── segment.go                 # Segment/page size constants, address encoding
│   │
│   ├── journal/                       # Write-Ahead Journal
│   │   ├── journal.go                 # WAL writer/reader
│   │   ├── checkpoint.go              # Checkpoint: L2P snapshot to stable storage
│   │   ├── recovery.go                # Crash recovery: load checkpoint + replay WAL
│   │   └── record.go                  # WAL record types and binary encoding
│   │
│   ├── backend/                       # Zoned device backends
│   │   ├── backend.go                 # Backend interface definition
│   │   ├── emulator/
│   │   │   ├── emulator.go            # In-memory zone emulator
│   │   │   └── emulator_test.go
│   │   └── smr/
│   │       ├── smr.go                 # SATA SMR backend via SG_IO ioctl
│   │       ├── sgioctl.go             # SG_IO ioctl wrapper (syscall.Syscall)
│   │       ├── zbc.go                 # ZBC command builders (Report Zones, Open, Close, Finish, Reset)
│   │       └── smr_test.go
│   │
│   ├── api/                           # REST monitoring API
│   │   ├── server.go                  # Chi router setup, middleware
│   │   ├── handlers.go                # Route handlers: zones, stats, health, gc trigger
│   │   ├── metrics.go                 # Prometheus metrics collector
│   │   └── types.go                   # API response types (JSON structs)
│   │
│   └── config/
│       ├── config.go                  # Config struct + YAML unmarshalling
│       └── defaults.go                # Default values
│
├── pkg/
│   └── zbc/                           # Public ZBC/ZAC types (Zone descriptor, zone state enum)
│       ├── types.go
│       └── constants.go
│
├── web/                               # React dashboard
│   ├── src/
│   │   ├── App.tsx
│   │   ├── main.tsx
│   │   ├── components/
│   │   │   ├── ZoneMap.tsx            # Zone heatmap visualization
│   │   │   ├── GCStats.tsx            # GC statistics panel
│   │   │   ├── ISCSIPanel.tsx         # iSCSI session panel
│   │   │   ├── BufferGauge.tsx        # Write buffer occupancy
│   │   │   └── DeviceInfo.tsx         # Device info panel
│   │   ├── hooks/
│   │   │   └── useAPI.ts              # Fetch + auto-refresh hooks
│   │   ├── types/
│   │   │   └── api.ts                 # TypeScript types matching Go API response types
│   │   └── api/
│   │       └── client.ts              # API client wrapper
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
│
├── tests/
│   ├── integration/
│   │   ├── iscsi_test.go              # iSCSI protocol integration tests
│   │   └── ztl_test.go                # ZTL + backend integration tests
│   └── e2e/
│       └── windows_compat_test.go     # Simulated Windows iSCSI Initiator compatibility
│
├── docker/
│   ├── Dockerfile                     # Multi-stage: Go builder + runtime
│   └── Dockerfile.dev                 # Development with hot reload
│
├── docker-compose.yml                 # Target + dashboard services
├── config.yaml.example                # Example configuration
├── go.mod
└── go.sum
```

---

## 2. Data Schemas

### 2.1 L2P Entry (in-memory)
```go
// Segment size: configurable, default 8KB
// SegmentID = LBA / (SegmentSize / BlockSize)
// PhysAddr encodes: zone_id (24 bits) + offset_within_zone (40 bits)

type PhysAddr uint64 // 0 = unmapped

func EncodePhysAddr(zoneID uint32, offsetSectors uint32) PhysAddr
func (p PhysAddr) ZoneID() uint32
func (p PhysAddr) OffsetSectors() uint32

// L2P table: []PhysAddr indexed by SegmentID
// Size: (TotalCapacitySectors / SectorsPerSegment) * 8 bytes
type L2PTable struct {
    entries    []atomic.Uint64 // PhysAddr per segment, atomic for lock-free reads
    segSize    uint32          // segment size in sectors
    totalSegs  uint64
}
```

### 2.2 Zone Descriptor (matches ZBC Report Zones response)
```go
type ZoneState uint8
const (
    ZoneStateEmpty       ZoneState = 0x01
    ZoneStateImplicitOpen ZoneState = 0x02
    ZoneStateExplicitOpen ZoneState = 0x03
    ZoneStateClosed      ZoneState = 0x04
    ZoneStateFull        ZoneState = 0x0E
    ZoneStateReadOnly    ZoneState = 0x0D
    ZoneStateOffline     ZoneState = 0x0F
)

type ZoneType uint8
const (
    ZoneTypeConventional  ZoneType = 0x01
    ZoneTypeSequentialReq ZoneType = 0x02
    ZoneTypeSequentialPre ZoneType = 0x03
)

type ZoneDescriptor struct {
    ZoneType      ZoneType
    ZoneState     ZoneState
    ZoneCondition uint8
    StartLBA      uint64
    Length        uint64 // zone size in sectors
    WritePointer  uint64 // absolute LBA of write pointer (for sequential zones)
}
```

### 2.3 Zone Manager State (in-memory)
```go
type ZoneInfo struct {
    ID           uint32
    Desc         ZoneDescriptor
    ValidBlocks  uint32    // number of valid (live) segments in this zone
    TotalBlocks  uint32    // total segments written to this zone
    GCScore      float32   // ValidBlocks / TotalBlocks (lower = better GC candidate)
    mu           sync.Mutex
}

type ZoneManager struct {
    zones        []ZoneInfo
    openZones    map[uint32]struct{} // set of currently open zone IDs
    maxOpenZones uint32
    freeList     []uint32            // IDs of EMPTY zones
    mu           sync.RWMutex
}
```

### 2.4 Write Buffer Entry
```go
type BufferEntry struct {
    ZoneID    uint32
    Offset    uint32   // offset in zone (sectors)
    Data      []byte
    LogSeqNo  uint64   // journal LSN assigned at write
    Dirty     bool
    Timestamp time.Time
}

type WriteBuffer struct {
    entries     map[uint32]*ZoneBuffer // keyed by ZoneID
    maxBytes    int64
    currentBytes int64
    flushCh     chan uint32            // zone IDs to flush
    mu          sync.Mutex
}
```

### 2.5 WAL Record
```go
type RecordType uint8
const (
    RecordTypeL2PUpdate RecordType = 0x01
    RecordTypeZoneOpen  RecordType = 0x02
    RecordTypeZoneClose RecordType = 0x03
    RecordTypeZoneReset RecordType = 0x04
    RecordTypeCheckpoint RecordType = 0xFF
)

// Binary encoding: little-endian
// | Magic (4B) | CRC32 (4B) | Type (1B) | LSN (8B) | SegmentID (8B) | OldPhys (8B) | NewPhys (8B) | Timestamp (8B) |
// Total: 49 bytes per L2P update record

type WALRecord struct {
    Magic     uint32     // 0xAIDA5A0E
    CRC32     uint32
    Type      RecordType
    LSN       uint64
    SegmentID uint64
    OldPhys   PhysAddr
    NewPhys   PhysAddr
    Timestamp int64      // Unix nano
}
```

### 2.6 iSCSI PDU (Basic Header Segment)
```go
// RFC 7143 Section 10.2
type BHS struct {
    Opcode          uint8
    Flags           uint8
    Specific1       [2]byte
    TotalAHSLength  uint8
    DataSegmentLength [3]byte // 24-bit big-endian
    LUN             [8]byte
    InitiatorTaskTag uint32
    Specific2       [28]byte  // opcode-specific
}

// Opcodes (initiator -> target)
const (
    OpLoginReq     = 0x03
    OpLogoutReq    = 0x06
    OpSCSICmd      = 0x01
    OpSCSIDataOut  = 0x05
    OpNOPOut       = 0x00
    OpTMFReq       = 0x02
    OpTextReq      = 0x04
)

// Opcodes (target -> initiator)
const (
    OpLoginResp    = 0x23
    OpLogoutResp   = 0x26
    OpSCSIResp     = 0x21
    OpSCSIDataIn   = 0x25
    OpNOPIn        = 0x20
    OpTMFResp      = 0x22
    OpTextResp     = 0x24
    OpR2T          = 0x31
    OpAsyncMsg     = 0x32
    OpReject       = 0x3F
)
```

---

## 3. API Contracts

### 3.1 REST API (Chi Router, base path /api/v1)

#### GET /api/v1/zones
Returns all zone descriptors with ZTL metadata.
```json
{
  "total": 8192,
  "zones": [
    {
      "id": 0,
      "type": "sequential_required",
      "state": "open",
      "start_lba": 0,
      "length_sectors": 524288,
      "write_pointer": 32768,
      "valid_blocks": 200,
      "total_blocks": 256,
      "gc_score": 0.78
    }
  ]
}
```

#### GET /api/v1/stats
```json
{
  "iscsi": {
    "active_sessions": 1,
    "total_read_bytes": 1073741824,
    "total_write_bytes": 536870912,
    "read_iops": 1200,
    "write_iops": 450,
    "read_latency_p99_us": 850,
    "write_latency_p99_us": 1200
  },
  "gc": {
    "running": false,
    "zones_reclaimed": 42,
    "bytes_migrated": 10737418240,
    "last_run_at": "2026-02-28T13:00:00Z",
    "free_zones": 512,
    "total_zones": 8192
  },
  "buffer": {
    "dirty_bytes": 67108864,
    "max_bytes": 536870912,
    "pending_flushes": 2
  },
  "journal": {
    "lsn": 100042,
    "last_checkpoint_lsn": 99000,
    "journal_size_bytes": 52428800
  }
}
```

#### GET /api/v1/health
```json
{"status": "ok", "uptime_seconds": 3600}
```

#### POST /api/v1/gc/trigger
Request body: none or `{"urgent": true}`
Response: `{"status": "triggered"}`

#### GET /api/v1/config
Returns current effective configuration (redacts secrets).

#### GET /metrics
Prometheus exposition format.

### 3.2 Key Prometheus Metrics
```
zns_iscsi_read_bytes_total{lun="0"} 1073741824
zns_iscsi_write_bytes_total{lun="0"} 536870912
zns_ztl_free_zones{device="/dev/sda"} 512
zns_ztl_open_zones{device="/dev/sda"} 8
zns_gc_reclaimed_zones_total{device="/dev/sda"} 42
zns_gc_migrated_bytes_total{device="/dev/sda"} 10737418240
zns_buffer_dirty_bytes{device="/dev/sda"} 67108864
zns_journal_lsn{device="/dev/sda"} 100042
zns_iscsi_session_count 1
```

### 3.3 Backend Interface
```go
type ZonedDevice interface {
    // Zone discovery
    ReportZones(startLBA uint64, partial bool) ([]ZoneDescriptor, error)
    ZoneCount() (uint64, error)
    ZoneSize() (uint64, error) // in sectors

    // Zone state management
    OpenZone(startLBA uint64) error
    CloseZone(startLBA uint64) error
    FinishZone(startLBA uint64) error
    ResetZone(startLBA uint64) error

    // I/O
    ReadSectors(lba uint64, count uint32) ([]byte, error)
    WriteSectors(lba uint64, data []byte) error

    // Device info
    BlockSize() uint32      // bytes (512 for SMR)
    Capacity() uint64       // total sectors
    MaxOpenZones() uint32

    // Lifecycle
    Close() error
}
```

### 3.4 ZTL Interface (called by SCSI layer)
```go
type ZonedTranslationLayer interface {
    Read(lba uint64, count uint32) ([]byte, error)
    Write(lba uint64, data []byte) error
    Flush() error
    Unmap(lba uint64, count uint32) error // optional TRIM
    Close() error
}
```

---

## 4. Configuration Schema

```yaml
# config.yaml
target:
  iqn: "iqn.2026-02.io.zns:target0"
  portal: "0.0.0.0:3260"
  max_sessions: 4
  auth:
    enabled: false
    chap_user: ""
    chap_secret: ""

device:
  backend: "smr"          # "smr" or "emulator"
  path: "/dev/sdb"        # for smr backend
  emulator:
    zone_count: 64
    zone_size_mb: 256
    max_open_zones: 14

ztl:
  segment_size_kb: 8       # L2P granularity
  buffer_size_mb: 512      # write buffer max size
  buffer_flush_age_sec: 5  # flush dirty data after N seconds
  gc_trigger_free_ratio: 0.20   # start GC when free zones < 20%
  gc_emergency_free_zones: 3    # emergency GC when free zones < 3

journal:
  path: "/var/lib/zns-iscsi/journal.wal"
  checkpoint_interval_sec: 60
  max_size_mb: 1024

api:
  listen: "0.0.0.0:8080"
  metrics_path: "/metrics"

log:
  level: "info"   # debug, info, warn, error
  format: "json"
```

---

## 5. Frontend Component Architecture

```
App
├── DeviceInfo          (device path, zone size, zone count, capacity)
├── ConnectionStatus    (iSCSI sessions, IQN, portal)
├── ZoneMap             (2D grid heatmap, color = zone state/GC score)
│   └── ZoneTooltip     (on hover: zone ID, state, valid%, write pointer)
├── BufferGauge         (dirty bytes / max bytes progress bar)
├── GCStats             (running indicator, reclaimed zones, throughput)
└── IOCounters          (read/write IOPS, bytes, latency p99)
```

### API Polling Strategy
- /api/v1/stats: poll every 1s
- /api/v1/zones: poll every 5s (expensive)
- /api/v1/health: poll every 10s
