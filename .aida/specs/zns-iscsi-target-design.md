# ZNS/SMR iSCSI Target - Technical Design

## Document Info
- Version: 1.0
- Date: 2026-02-28
- Session: aida-zns-iscsi-20260228-135900
- Status: APPROVED FOR IMPLEMENTATION

---

## 1. Architecture Overview

```
+=====================================================================+
|                     Windows iSCSI Initiator                         |
|          (or any OS: Linux, macOS with iSCSI Initiator)             |
+============================+========================================+
                             |
                     TCP port 3260
                             |
+============================v========================================+
|                    iSCSI Target Server                              |
|  +--------------------------+  +--------------------------------+   |
|  |   TCP Listener / Accept  |  |   iSCSI Discovery (Text PDU)  |   |
|  +-----------+--------------+  +--------------------------------+   |
|              |                                                       |
|  +-----------v--------------+                                       |
|  |   Session Manager        | (goroutine per session)               |
|  |   Connection FSM         |                                       |
|  |   CmdSN tracker          |                                       |
|  +-----------+--------------+                                       |
|              |                                                       |
|  +-----------v--------------+                                       |
|  |   SCSI Command           |                                       |
|  |   Dispatcher             |                                       |
|  |   (opcode -> handler)    |                                       |
|  +--------+---------+-------+                                       |
|           |         |                                               |
|       Read()     Write()                                            |
+===========|=========|===============================================+
            |         |
+===========v=========v===============================================+
|                Zone Translation Layer (ZTL)                         |
|                                                                     |
|  +------------------+    +------------------+                       |
|  |   Write Buffer   |    |   L2P Table      |                       |
|  |   (per-zone)     |    |   []PhysAddr     |                       |
|  |   coalescing     |    |   atomic uint64  |                       |
|  +-------+----------+    +--------+---------+                       |
|          |                        |                                 |
|  +-------v----------+    +--------v---------+                       |
|  |   Zone Manager   |    |   P2L Table      |                       |
|  |   state machine  |    |   (for GC)       |                       |
|  |   open tracking  |    +------------------+                       |
|  +-------+----------+                                               |
|          |                                                           |
|  +-------v----------+    +------------------+                       |
|  |   GC Engine      |    |   Write-Ahead    |                       |
|  |   (background    |    |   Journal (WAL)  |                       |
|  |    goroutine)    |    |   + Checkpoint   |                       |
|  +-------+----------+    +--------+---------+                       |
+----------|--------------------------|---------------------------------+
           |                          |
           | Zone Commands            | WAL writes (fsync)
+----------v--------------------------v---------------------------------+
|                     Zoned Device Backend                              |
|                                                                       |
|   +-----------------------+    +----------------------------------+   |
|   |   In-Memory Emulator  |    |   SATA SMR via SG_IO ioctl      |   |
|   |   (development/test)  |    |   ZBC/ZAC commands              |   |
|   +-----------------------+    |   /dev/sdX                      |   |
|                                +----------------------------------+   |
+-----------------------------------------------------------------------+

+===========================+   +=====================================+
|   REST API Server         |   |   React Dashboard                   |
|   Chi router :8080        |   |   TypeScript + Vite :5173           |
|   /api/v1/zones           |<--+   Zone heatmap                      |
|   /api/v1/stats           |   |   GC stats panel                    |
|   /metrics (Prometheus)   |   |   iSCSI session panel               |
+===========================+   +=====================================+
```

---

## 2. Component Design

### 2.1 iSCSI Target Layer

#### 2.1.1 Connection State Machine
```
                +----------+
                |  FREE    |
                +----+-----+
                     | TCP Accept
                     v
                +----+-----+
                |  SECURITY|  <-- Login PDU (SecurityNegotiation stage)
                |  STAGE   |      CHAP exchange if auth enabled
                +----+-----+
                     | Auth OK
                     v
                +----+-----+
                | OPERATL  |  <-- Login PDU (OperationalNegotiation stage)
                | STAGE    |      MaxBurstLength, ImmediateData, etc.
                +----+-----+
                     | Params agreed
                     v
                +----+-----+
                |  FULL    |  <-- Normal I/O phase
                | FEATURE  |      SCSI commands, NOP, TMF, Text
                +----+-----+
                     | Logout PDU or TCP close
                     v
                +----+-----+
                | CLEANUP  |
                +----------+
```

#### 2.1.2 PDU Processing Pipeline
```
TCP Read -> BHS parse (48 bytes) -> AHS parse (if TotalAHSLength > 0)
         -> DataSegment read (DataSegmentLength bytes, padded to 4-byte boundary)
         -> Digest verify (if HeaderDigest/DataDigest negotiated)
         -> Route by Opcode:
              0x01 SCSI CMD     -> SCSI dispatcher
              0x05 DATA-OUT     -> reassembly buffer, then SCSI dispatcher
              0x00 NOP-OUT      -> NOP-In response
              0x02 TMF REQ      -> Task Manager
              0x06 LOGOUT REQ   -> close connection
              0x04 TEXT REQ     -> discovery handler
```

#### 2.1.3 Write Data Reassembly
- Each SCSI WRITE command has an InitiatorTaskTag (ITT)
- Multiple Data-Out PDUs carry fragments identified by (ITT, DataSN)
- Reassembly buffer: `map[uint32][]byte` keyed by ITT
- When `F` (Final) bit set in Data-Out, pass assembled buffer to ZTL.Write
- If InitialR2T=Yes, send R2T for second and subsequent bursts

### 2.2 Zone Translation Layer

#### 2.2.1 L2P Table Design
```
SegmentID = LBA * BlockSize / SegmentSize
           = LBA / (SegmentSize / BlockSize)
           = LBA / SectorsPerSegment

PhysAddr (uint64):
  Bits 63-40: ZoneID      (24 bits = up to 16M zones)
  Bits 39-0:  OffsetSects (40 bits = up to 1T sectors per zone)

Unmapped entry: PhysAddr == 0 (zero is an invalid physical addr since Zone 0 sector 0 is always reserved or a conventional zone)
```

Memory layout: contiguous `[]atomic.Uint64` slice. All reads are lock-free. Writes use CAS (compare-and-swap) during GC migration to detect concurrent updates.

#### 2.2.2 Write Path
```
ZTL.Write(lba, data []byte):
  1. segments = split data into SegmentSize chunks
  2. for each segment:
     a. zoneID = ZoneManager.AllocateOrGetZone(segmentID)
     b. WriteBuffer.Add(zoneID, writePointer, segData)
     c. WAL.LogUpdate(segmentID, oldPhys, newPhys)
     d. L2P.Set(segmentID, newPhys) [atomic store]
  3. if WriteBuffer[zoneID].Size >= ZoneSize:
     flush(zoneID)
  4. WAL.GroupCommit() [may block up to sync_period_ms]
  5. return nil
```

#### 2.2.3 Read Path (lock-free critical path)
```
ZTL.Read(lba, count):
  1. segments = resolve LBA range to segment IDs
  2. result = make([]byte, count * BlockSize)
  3. for each segmentID:
     a. physAddr = L2P.Get(segmentID) [atomic load]
     b. if physAddr == 0: fill zeros (unmapped = zeros)
     c. else: check WriteBuffer first (may have unflushed data)
        if buffer hit: copy from buffer
        else: device.ReadSectors(physAddr.ZoneID, physAddr.Offset, SegmentSectors)
  4. return result
```

#### 2.2.4 Zone Manager
```go
type ZoneState struct {
    ID           uint32
    State        ZoneStateEnum
    WritePointer uint64     // absolute LBA of write pointer
    ValidSegs    uint32     // segments with live L2P mappings
    TotalSegs    uint32     // total segments ever written (for GC scoring)
}

// Allocation policy: round-robin over EMPTY zones
// Open zone list: LRU eviction when max_open_zones reached
//   eviction: send CLOSE ZONE to device, update state to CLOSED
```

### 2.3 GC Engine

#### 2.3.1 GC Algorithm
```
GC.Run():
  loop:
    wait for trigger (free_ratio < threshold OR manual trigger)

    victim = selectVictim()  // lowest ValidSegs/TotalSegs
    if victim == nil: continue

    // Freeze victim zone (no new writes assigned)
    ZoneManager.Freeze(victim.ID)

    // Migrate live data
    for each segment in victim zone (by P2L scan):
      currentPhys = L2P.Get(segID)  // re-read in case concurrent update
      if currentPhys.ZoneID != victim.ID: continue  // already migrated
      data = device.ReadSectors(currentPhys)
      newZone = ZoneManager.Allocate()
      newPhys = device.WriteSectors(newZone, data)
      WAL.LogUpdate(segID, currentPhys, newPhys)
      if !L2P.CAS(segID, currentPhys, newPhys):
        // Concurrent write happened; new write wins, our copy is stale
        // Discard our copy; zone stays valid (foreground write will finalize)
        continue

    // Journal zone reset intent
    WAL.LogZoneReset(victim.ID)
    WAL.Flush()

    // Reset victim zone
    device.ResetZone(victim.StartLBA)
    ZoneManager.MarkEmpty(victim.ID)

    // Update GC stats
    gcStats.ZonesReclaimed.Add(1)
```

#### 2.3.2 GC/Write Synchronization
```
Write path:    L2P.CAS(segID, old=0, new=physAddr)
               OR
               L2P.Set(segID, physAddr) for new allocation

GC path:       L2P.CAS(segID, old=victimPhys, new=newPhys)
               if CAS fails: foreground write won, discard GC copy

Result: no mutex needed on L2P; CAS provides atomic update with conflict detection
```

### 2.4 SMR Backend via SG_IO

#### 2.4.1 SG_IO ioctl Structure
```c
// sg_io_hdr_t fields relevant for ZBC commands
struct sg_io_hdr {
    int interface_id;        // 'S' for SCSI
    int dxfer_direction;     // SG_DXFER_FROM_DEV / SG_DXFER_TO_DEV
    unsigned char cmd_len;   // CDB length
    unsigned char mx_sb_len; // max sense buffer length
    unsigned char *cmdp;     // CDB pointer
    void *dxferp;            // data buffer
    unsigned int dxfer_len;  // data length
    unsigned char *sbp;      // sense buffer
    unsigned int timeout;    // timeout in ms
    unsigned int info;       // output: residual, status, etc.
    unsigned char status;    // output: SCSI status
    unsigned char sb_len_wr; // output: sense buffer length written
};
```

#### 2.4.2 REPORT ZONES CDB (ZBC opcode 0x95)
```
Byte 0:  0x95 (REPORT ZONES)
Byte 1:  0x00 (reserved)
Byte 2-9: Starting LBA (big-endian uint64)
Byte 10-13: Allocation Length (big-endian uint32)
Byte 14: Reporting Options (0x00=all, 0x01=empty, 0x02=open, etc.)
Byte 15: 0x00 (control)

Response: 64-byte header + N * 64-byte zone descriptors
Zone Descriptor:
  Byte 0:  Zone Type (bits 3-0) + Zone Condition (bits 7-4, actually bits 7-4)
  Byte 1:  bits: [Reset=7, Non-seq=6, ...]
  Byte 2-7: Reserved
  Byte 8-15:  Zone Length (sectors, big-endian uint64)
  Byte 16-23: Zone Start LBA (big-endian uint64)
  Byte 24-31: Write Pointer LBA (big-endian uint64)
  Byte 32-63: Reserved
```

#### 2.4.3 ZONE ACTION CDB (ZBC opcode 0x9F)
```
Byte 0:  0x9F (ZONE ACTION command)
Byte 1:  Action (0x01=CLOSE, 0x02=FINISH, 0x03=OPEN, 0x04=RESET)
Byte 2-9: Zone Start LBA (big-endian uint64)
Byte 10-13: Reserved
Byte 14: ALL bit (0x01 = apply to all zones)
Byte 15: Control
```

### 2.5 Write-Ahead Journal Format

#### 2.5.1 Record Binary Layout
```
Offset  Size  Field
0       4     Magic: 0xADA50001
4       4     CRC32C of bytes 8..end
8       1     RecordType (0x01=L2PUpdate, 0x02=ZoneOpen, 0x03=ZoneClose, 0x04=ZoneReset, 0xFF=Checkpoint)
9       8     LSN (Log Sequence Number, monotonically increasing uint64)
17      8     SegmentID (for L2PUpdate; ZoneID for zone operations)
25      8     OldPhysAddr
33      8     NewPhysAddr
41      8     Timestamp (Unix nanoseconds)
--- Total L2PUpdate record: 49 bytes ---

Checkpoint record additionally appends:
49      8     L2PTableLen (number of entries)
57      N*8   L2PTable entries (PhysAddr)
```

#### 2.5.2 Recovery Flow
```
startup:
  1. Open WAL file
  2. Scan for most recent Checkpoint record (validated by CRC32)
  3. Load L2P table from checkpoint
  4. Read remaining records after checkpoint LSN
  5. For each L2PUpdate: L2P.Set(segID, newPhys) if LSN > checkpoint.LSN
  6. For each ZoneReset: ZoneManager.MarkEmpty(zoneID) if LSN > checkpoint.LSN
  7. Call device.ReportZones() to reconcile write pointers
  8. Update ZoneManager state from ReportZones response
  9. Log "Recovery complete" + LSN replayed count
```

### 2.6 REST API Design

#### 2.6.1 Router Structure (Chi)
```go
r := chi.NewRouter()
r.Use(middleware.Logger)
r.Use(middleware.Recoverer)
r.Use(middleware.RequestID)

r.Get("/api/v1/zones", h.ListZones)
r.Get("/api/v1/stats", h.GetStats)
r.Get("/api/v1/health", h.Health)
r.Post("/api/v1/gc/trigger", h.TriggerGC)
r.Get("/api/v1/config", h.GetConfig)
r.Handle("/metrics", promhttp.Handler())

// Serve embedded React app
r.Handle("/*", http.FileServer(http.FS(webFS)))
```

#### 2.6.2 Prometheus Metrics
```
# iSCSI
zns_iscsi_read_bytes_total counter
zns_iscsi_write_bytes_total counter
zns_iscsi_read_operations_total counter
zns_iscsi_write_operations_total counter
zns_iscsi_read_latency_seconds histogram (buckets: 1ms, 5ms, 10ms, 50ms, 100ms, 500ms)
zns_iscsi_write_latency_seconds histogram
zns_iscsi_active_sessions gauge

# ZTL / GC
zns_ztl_free_zones gauge
zns_ztl_open_zones gauge
zns_ztl_total_zones gauge
zns_gc_reclaimed_zones_total counter
zns_gc_migrated_bytes_total counter
zns_gc_running gauge (0 or 1)
zns_buffer_dirty_bytes gauge
zns_buffer_pending_flushes gauge

# Journal
zns_journal_lsn gauge
zns_journal_size_bytes gauge
zns_journal_checkpoint_lsn gauge
```

---

## 3. Data Flow Diagrams

### 3.1 Write Path (Happy Path)
```
Windows writes 4KB block at LBA 0x1000:
  SCSI WRITE(10) PDU arrives at target
  |
  +--> iSCSI session: CmdSN validated, Data-Out received and reassembled
  |
  +--> SCSI WRITE handler: extract LBA=0x1000, length=8 sectors (4KB)
  |
  +--> ZTL.Write(lba=0x1000, data=4096 bytes):
        |
        +--> segmentID = 0x1000 / 16 = 256 (for 8KB segments, 16 sectors each)
        +--> ZoneManager.GetOrAlloc(segmentID) -> ZoneID=5
        +--> WritePointer = Zone5.WritePointer = 0x50000
        +--> newPhys = PhysAddr{ZoneID:5, Offset:0x50000}
        +--> WriteBuffer.Add(zoneID=5, data)
        +--> WAL.LogUpdate(segID=256, old=0, new=newPhys) -> LSN=1042
        +--> L2P[256].Store(newPhys) [atomic]
        +--> WAL.GroupCommit() [waits up to 10ms, batches with other writes]
        +--> return nil
  |
  +--> SCSI WRITE handler: send SCSI Response PDU (status=GOOD)
  |
  +--> (async) WriteBuffer.FlushTrigger:
        if Buffer[Zone5].Size >= ZoneSize (256MB): flush to device
        if Buffer[Zone5].Age >= 5s: flush to device
```

### 3.2 Read Path (Cached vs Device)
```
Windows reads 4KB at LBA 0x1000:
  SCSI READ(10) PDU arrives
  |
  +--> ZTL.Read(lba=0x1000, count=8):
        |
        +--> segmentID = 256
        +--> physAddr = L2P[256].Load() = PhysAddr{ZoneID:5, Offset:0x50000}
        +--> WriteBuffer.Lookup(zoneID=5, offset=0x50000):
              if HIT: return buffer data  [fast path, no device I/O]
              if MISS: device.ReadSectors(Zone5.StartLBA + 0x50000, 16 sectors)
        +--> return data
  |
  +--> iSCSI: send Data-In PDUs with read data
```

---

## 4. Module Dependency Graph

```
cmd/zns-iscsi
    |
    +-> internal/config         (no deps)
    +-> internal/backend/emulator (no deps)
    +-> internal/backend/smr    (pkg/zbc, syscall/unix [linux only])
    +-> internal/journal        (no deps beyond stdlib)
    +-> internal/ztl            (internal/backend, internal/journal)
    +-> internal/scsi           (internal/ztl)
    +-> internal/iscsi          (internal/scsi)
    +-> internal/api            (internal/ztl, internal/iscsi, prometheus)
```

---

## 5. Key Go Packages and External Dependencies

| Package | Purpose | Source |
|---------|---------|--------|
| `go-chi/chi/v5` | REST router | github.com/go-chi/chi/v5 |
| `prometheus/client_golang` | Prometheus metrics | github.com/prometheus/client_golang |
| `gopkg.in/yaml.v3` | Config parsing | gopkg.in/yaml.v3 |
| `stretchr/testify` | Test assertions | github.com/stretchr/testify |
| `golang.org/x/sys/unix` | SG_IO ioctl (Linux) | golang.org/x/sys/unix |
| Standard `sync/atomic` | L2P lock-free access | stdlib |
| Standard `encoding/binary` | WAL record encoding | stdlib |
| Standard `crypto/sha1` | CHAP challenge | stdlib |
| Standard `embed` | Serve React app | stdlib |

No CGO dependencies. SG_IO is accessed via pure Go `syscall.Syscall6`.

---

## 6. Testing Strategy

### 6.1 Unit Tests
- `internal/ztl/l2p_test.go`: L2P set/get, CAS, boundary conditions
- `internal/ztl/buffer_test.go`: write buffer accumulation, flush triggers
- `internal/ztl/gc_test.go`: victim selection, migration logic with mock device
- `internal/journal/recovery_test.go`: WAL write/read, checkpoint, crash recovery simulation
- `internal/backend/emulator/emulator_test.go`: sequential write enforcement, zone state machine
- `internal/iscsi/login_test.go`: parameter negotiation
- `internal/scsi/handler_test.go`: INQUIRY, READ CAPACITY response correctness

### 6.2 Integration Tests
- `tests/integration/ztl_test.go`: ZTL + emulator backend full write/read/GC cycle
- `tests/integration/iscsi_test.go`: Full iSCSI session with in-process initiator

### 6.3 Emulator-based End-to-End
- Write 100 MB of random-pattern data, read back and verify
- Trigger GC, verify all data is intact after GC
- Simulate crash mid-write (kill goroutine at WAL point), restart, verify recovery

---

## 7. Configuration Reference

```yaml
target:
  iqn: "iqn.2026-02.io.zns:target0"
  portal: "0.0.0.0:3260"
  max_sessions: 4
  auth:
    enabled: false
    chap_user: ""
    chap_secret: ""

device:
  backend: "emulator"      # "emulator" | "smr"
  path: "/dev/sdb"         # for smr backend only
  emulator:
    zone_count: 64
    zone_size_mb: 256
    max_open_zones: 14

ztl:
  segment_size_kb: 8
  buffer_size_mb: 512
  buffer_flush_age_sec: 5
  gc_trigger_free_ratio: 0.20
  gc_emergency_free_zones: 3

journal:
  path: "/var/lib/zns-iscsi/journal.wal"
  checkpoint_interval_sec: 60
  sync_period_ms: 10
  max_size_mb: 1024

api:
  listen: "0.0.0.0:8080"

log:
  level: "info"
  format: "json"
```

---

## 8. Docker Architecture

### 8.1 Multi-stage Dockerfile
```dockerfile
# Stage 1: Build Go binary
FROM golang:1.23-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/zns-iscsi ./cmd/zns-iscsi

# Stage 2: Build React dashboard
FROM node:20-alpine AS web-builder
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# Stage 3: Runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=go-builder /out/zns-iscsi /usr/local/bin/
COPY --from=web-builder /web/dist /var/lib/zns-iscsi/web/
EXPOSE 3260 8080
ENTRYPOINT ["/usr/local/bin/zns-iscsi"]
```

### 8.2 docker-compose.yml Structure
```yaml
services:
  target:
    build: .
    ports:
      - "3260:3260"   # iSCSI
      - "8080:8080"   # REST API + dashboard
    volumes:
      - ./config.yaml:/etc/zns-iscsi/config.yaml:ro
      - journal_data:/var/lib/zns-iscsi
    privileged: true  # required for SG_IO device access
    devices:
      - /dev/sdb:/dev/sdb  # SMR HDD passthrough (comment out for emulator mode)

volumes:
  journal_data:
```
