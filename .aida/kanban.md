# Project Kanban - zns-iscsi-target

## Status: COMPLETED

## Spec Phase - COMPLETE
- [x] Phase 1: Extraction & Architecture
- [x] Phase 2: Structure & Schema
- [x] Phase 3: Alignment
- [x] Phase 4: Verification

## Impl Phase - COMPLETE

### Phase A: Foundation
- [x] A-01: Project Scaffolding
- [x] A-02: Configuration Package
- [x] A-03: ZBC Types

### Phase B: Zoned Device Backend
- [x] B-01: Backend Interface
- [x] B-02: In-Memory Emulator
- [x] B-03: SMR Backend (Linux only)

### Phase C: Zone Translation Layer
- [x] C-01 to C-08: L2P, P2L, ZoneManager, WriteBuffer, GC, ZTL Orchestrator

### Phase D: Write-Ahead Journal
- [x] D-01 to D-04: WAL encoding, journal, checkpoint, crash recovery

### Phase E: SCSI Command Layer
- [x] E-01 to E-07: Sense, INQUIRY, READ CAPACITY, READ, WRITE, stubs, dispatcher

### Phase F: iSCSI Protocol
- [x] F-01 to F-07: PDU, params, login, sessions, Data-Out reassembly, server

### Phase G: REST API
- [x] G-01 to G-04: Types, Prometheus metrics, handlers, Chi server

### Phase H: Main Entry Point
- [x] H-01: Component wiring + config.yaml.example

### Phase I: React Dashboard
- [x] I-01 to I-05: Vite setup, API client, ZoneMap heatmap, stats panels, layout

### Phase J: Docker
- [x] J-01: Dockerfile (multi-stage)
- [x] J-02: docker-compose.yml
- [x] J-03: .dockerignore

## Quality Gates - ALL PASSED
- [x] Gate 1: Backend Build
- [x] Gate 2: Backend Tests (8 packages pass)
- [x] Gate 3: Frontend Build (dist/ generated)
- [x] Gate 4: Frontend Tests (39 tests pass)
- [x] Gate 5: Race Detector (no races)
