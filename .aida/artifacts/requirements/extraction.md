# Phase 1: Extraction & Architecture

## Session
- Session ID: aida-zns-iscsi-20260228-135900
- Phase: 1 - Extraction
- Date: 2026-02-28

---

## 1. Core Problem Statement

Windows (and any OS with an iSCSI Initiator) expects a block device that accepts random read/write at any LBA. A SATA SMR (Shingled Magnetic Recording) HDD with Host Managed zones enforces **sequential write only** per zone. These constraints are fundamentally incompatible.

The ZNS/SMR iSCSI Target bridges this gap by:
1. Presenting a conventional block device to the iSCSI Initiator (Windows).
2. Internally maintaining a Zone Translation Layer (ZTL) that maps logical (conventional) addresses to physical (sequential) zone addresses.
3. Managing zone state, garbage collection, crash recovery, and monitoring.

---

## 2. Extracted Features

### 2.1 iSCSI Target (Transport Layer)
- F-01: iSCSI Target server implementing RFC 7143 subset
- F-02: Login/logout PDU handling (OperationalText negotiation)
- F-03: SCSI Command PDU dispatch (Read6/10/16, Write6/10/16, Inquiry, Read Capacity, Test Unit Ready)
- F-04: Data-In / Data-Out PDU pipeline (immediate data, unsolicited data)
- F-05: Error recovery Level 0 (connection drop reconnection)
- F-06: CHAP authentication (optional, single-level)
- F-07: Multiple sessions per target (configurable max)
- F-08: Windows iSCSI Initiator compatibility (MSDSM, initiator_name format)
- F-09: Configurable target IQN and LUN

### 2.2 Zone Translation Layer (ZTL)
- F-10: Logical-to-Physical (L2P) address mapping table (in-memory, persisted to journal)
- F-11: Physical-to-Logical (P2L) reverse mapping for GC
- F-12: Write buffer with coalescing (aggregate small writes before zone flush)
- F-13: Sequential write enforcement (buffer accumulates until zone boundary or flush trigger)
- F-14: Zone state machine: EMPTY -> OPEN -> FULL -> CLOSED
- F-15: Multi-zone open tracking (respect SMR device max open zones limit)
- F-16: Zone capacity vs zone size distinction
- F-17: Write pointer tracking per zone

### 2.3 Garbage Collector (GC)
- F-18: Background GC goroutine with configurable trigger threshold (% valid blocks)
- F-19: Victim zone selection (lowest valid block ratio)
- F-20: Live data migration: read valid blocks from victim, re-write to fresh zone
- F-21: Zone reclamation: reset victim zone after migration
- F-22: GC pause/resume controls
- F-23: GC statistics exposure (zones reclaimed, bytes migrated, throughput)
- F-24: Emergency GC mode when free zones drop below critical threshold

### 2.4 Zoned Device Backend
- F-25: In-memory emulator for development and testing
  - Configurable: zone count, zone size, max open zones
  - Simulates sequential write enforcement, write pointer, zone state
- F-26: Real SATA SMR HDD backend via Linux SG_IO ioctl
  - ZBC Report Zones (opcode 0x95)
  - Zone Open (0x9F/0x03)
  - Zone Close (0x9F/0x01)
  - Zone Finish (0x9F/0x02)
  - Zone Reset (0x9F/0x04)
  - Read (0x28 / 0x88) and Write (0x2A / 0x8A) via passthrough
- F-27: Optional libzbd Go binding wrapper as alternative backend
- F-28: Backend interface abstraction (swap emulator for real device without ZTL changes)

### 2.5 Write-Ahead Journal (Crash Consistency)
- F-29: WAL journal file for L2P updates before they are applied
- F-30: Checkpoint mechanism: flush in-memory L2P to stable storage periodically
- F-31: Crash recovery: replay journal entries after unclean shutdown
- F-32: Journal compaction to bound journal file size

### 2.6 REST Monitoring API
- F-33: GET /api/v1/zones - list all zones with state, write pointer, valid blocks
- F-34: GET /api/v1/stats - iSCSI I/O stats, GC stats, buffer stats
- F-35: GET /api/v1/health - liveness/readiness probe
- F-36: POST /api/v1/gc/trigger - manually trigger GC run
- F-37: GET /api/v1/config - read current configuration
- F-38: Prometheus metrics endpoint (/metrics)
- F-39: Chi router, JSON responses

### 2.7 React Dashboard (Frontend)
- F-40: Zone visualization: heat map of zones (color = state/valid ratio)
- F-41: Real-time GC statistics panel
- F-42: iSCSI connection panel (active sessions, I/O counters)
- F-43: Write buffer occupancy indicator
- F-44: Device info panel (zone count, zone size, capacity)
- F-45: Auto-refresh via polling or SSE

### 2.8 Configuration & Operations
- F-46: YAML/TOML config file (target IQN, portal, device path, GC thresholds, buffer size)
- F-47: CLI flags for override
- F-48: Docker multi-stage build (Go builder + runtime image)
- F-49: docker-compose for development (target + dashboard)

---

## 3. Constraints

### 3.1 Device Constraints
- C-01: SATA SMR HDD zone size ~256 MB (may vary by vendor, must be detected at runtime)
- C-02: Host Managed SMR requires zone reset before re-write (no partial overwrite)
- C-03: Max Open Zones limit enforced by device (typically 8-16)
- C-04: Write pointer can only advance forward within a zone
- C-05: Zone sequence numbers (ZSN) must be maintained for ZBC compliance

### 3.2 iSCSI Protocol Constraints
- C-06: Windows iSCSI Initiator sends 512-byte logical block size commands (INQUIRY response must match)
- C-07: Must handle MaxRecvDataSegmentLength negotiation
- C-08: CmdSN/ExpCmdSN sequence must be maintained for correct ordering
- C-09: Immediate data and unsolicited data first burst negotiation required

### 3.3 Performance Constraints
- C-10: Write buffer must be zone-size aligned to avoid partial zone writes
- C-11: L2P table must be kept in memory for low read latency (DRAM requirement scales with capacity)
- C-12: GC must not block foreground I/O beyond configurable latency target
- C-13: GC read-migrate-write must saturate zone bandwidth while not starving I/O

### 3.4 Reliability Constraints
- C-14: L2P table is the single source of truth; must survive crash without corruption
- C-15: Journal must be fsynced before acknowledging writes to iSCSI initiator
- C-16: Zone state must be recoverable from device + journal after crash

---

## 4. High-Level Architecture

```
Windows iSCSI Initiator (Initiator)
        |
        | TCP/IP (port 3260)
        v
+---------------------------------------+
|         iSCSI Target Server           |
|   PDU Parser / Session Manager        |
|   SCSI Command Dispatcher             |
+------------------+--------------------+
                   |
                   | Block Read/Write (LBA, length)
                   v
+---------------------------------------+
|     Zone Translation Layer (ZTL)      |
|                                       |
|  +----------------+  +-----------+   |
|  |  Write Buffer  |  | L2P Table |   |
|  |  (coalescing)  |  | (in-mem)  |   |
|  +-------+--------+  +-----+-----+   |
|          |                 |          |
|  +-------v---------+       |          |
|  | GC Engine       |<------+          |
|  | (background)    |                  |
|  +-------+---------+                  |
|          |                            |
|  +-------v---------+                  |
|  | Write Journal   |                  |
|  | (WAL / fsync)   |                  |
|  +-------+---------+                  |
+----------|---------+------------------+
           |
           | Zone Commands (Open/Write/Finish/Reset/Report)
           v
+---------------------------------------+
|      Zoned Device Backend             |
|                                       |
|  [In-Memory Emulator]                 |
|       OR                              |
|  [SATA SMR via SG_IO ioctl]           |
|   ZBC/ZAC commands                    |
+---------------------------------------+
           |
           v (physical)
    SATA SMR HDD /dev/sdX
```

### Component Boundaries
- iSCSI Layer: Stateless PDU processor; calls ZTL Read/Write interface
- ZTL Layer: Stateful; owns L2P, buffer, GC, journal
- Backend Layer: Stateless zone command executor; abstracts device specifics
- Monitoring Layer: Read-only observer of ZTL and iSCSI state

---

## 5. Key Design Decisions

### DD-01: L2P Table Granularity
- Granularity: 512-byte logical block (matches iSCSI block size)
- For a 4 TB device: 4TB / 512B = 8 billion entries
- At 8 bytes per entry (physical address): ~64 GB RAM - impractical
- Solution: Use 4 KB page granularity (LBA aligned to 4K pages)
  - 4TB / 4KB = 1 billion entries; at 8 bytes = ~8 GB - still large
  - Use 8 KB segment granularity initially, tunable
  - Or use indirect mapping with compressed sparse table

### DD-02: Write Buffer Strategy
- Accumulate writes in a write buffer keyed by zone
- Flush when buffer reaches zone_capacity for a zone (full zone write)
- Also flush on: timeout (dirty data age threshold), GC pressure
- Buffer is page-cache-like with LRU eviction to bound memory use

### DD-03: GC Trigger Strategy
- Free zone ratio trigger: GC starts when free_zones / total_zones < 0.2
- Emergency mode: when free_zones < 3 (configurable), GC runs synchronously
- Victim selection: choose zone with lowest valid_blocks / zone_capacity ratio

### DD-04: Journal Strategy
- WAL entries: [MAGIC][CRC32][EntryType][LBA][OldPhys][NewPhys][Timestamp]
- Checkpoint: full L2P snapshot written at checkpoint intervals
- Recovery: load latest checkpoint, replay WAL entries after checkpoint LSN

### DD-05: iSCSI Minimal Implementation
- Implement only features needed for Windows compatibility
- Use gotgt library as reference or fork; or implement from scratch with RFC 7143
- Priority: Normal Operational Text parameters (MaxConnections, HeaderDigest, DataDigest, ImmediateData, MaxBurstLength, FirstBurstLength, MaxOutstandingR2T)
