package api

import (
	"encoding/json"
	"net/http"

	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// Handler holds the dependencies for all HTTP handlers.
type Handler struct {
	ztl     ZTLProvider
	iscsi   ISCSIStatsProvider
	journal JournalStatsProvider
	cfg     *config.Config
}

// HandlerConfig holds optional dependencies for Handler.
type HandlerConfig struct {
	// ISCSI is the iSCSI stats provider. If nil, a no-op provider is used.
	ISCSI ISCSIStatsProvider
	// Journal is the journal stats provider. If nil, a no-op provider is used.
	Journal JournalStatsProvider
}

// NewHandler creates a new Handler.
func NewHandler(ztlProv ZTLProvider, cfg *config.Config, hcfg HandlerConfig) *Handler {
	h := &Handler{
		ztl: ztlProv,
		cfg: cfg,
	}
	if hcfg.ISCSI != nil {
		h.iscsi = hcfg.ISCSI
	} else {
		h.iscsi = &noopISCSIStats{}
	}
	if hcfg.Journal != nil {
		h.journal = hcfg.Journal
	} else {
		h.journal = &noopJournalStats{}
	}
	return h
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Health returns {"status":"ok"}.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// ListZones returns a list of all zones with their current state.
func (h *Handler) ListZones(w http.ResponseWriter, r *http.Request) {
	zm := h.ztl.ZoneManager()
	zoneInfos := zm.AllZones()

	details := make([]ZoneDetail, 0, len(zoneInfos))
	for _, zi := range zoneInfos {
		details = append(details, ZoneDetail{
			ID:              zi.ID,
			State:           zoneStateName(zi.State),
			Type:            "sequential",
			WritePointerLBA: zi.WritePointer,
			ZoneStartLBA:    zi.StartLBA,
			ZoneLengthLBA:   zi.SizeSectors,
			ValidSegments:   zi.ValidSegs,
			TotalSegments:   zi.TotalSegs,
			GCScore:         zi.GCScore(),
		})
	}

	writeJSON(w, http.StatusOK, ZoneListResponse{
		Zones: details,
		Total: len(details),
	})
}

// GetStats returns aggregate statistics from all subsystems.
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	zm := h.ztl.ZoneManager()
	gcSnap := h.ztl.GCStats().Snapshot()
	buf := h.ztl.WriteBuffer()

	// ZTL / GC
	gcResp := GCStatsResponse{
		Running:        gcSnap.Running,
		ZonesReclaimed: uint64(gcSnap.ZonesReclaimed),
		BytesMigrated:  uint64(gcSnap.BytesMigrated),
		RunCount:       uint64(gcSnap.RunCount),
	}

	bufResp := BufferStats{
		DirtyBytes:     uint64(buf.DirtyBytes()),
		MaxBytes:       uint64(h.cfg.ZTL.BufferSizeMB) * 1024 * 1024,
		PendingFlushes: buf.ZoneCount(),
	}

	// Journal
	journalResp := JournalStats{
		CurrentLSN:    h.journal.CurrentLSN(),
		CheckpointLSN: h.journal.CheckpointLSN(),
		SizeBytes:     h.journal.SizeBytes(),
	}

	// Device
	deviceResp := DeviceStatsResponse{
		Backend:       h.cfg.Device.Backend,
		Path:          h.cfg.Device.Path,
		ZoneCount:     zm.TotalZoneCount(),
		ZoneSizeMB:    h.cfg.Device.Emulator.ZoneSizeMB,
		TotalCapacity: uint64(zm.TotalZoneCount()) * uint64(h.cfg.Device.Emulator.ZoneSizeMB) * 1024 * 1024,
		MaxOpenZones:  h.cfg.Device.Emulator.MaxOpenZones,
	}

	// iSCSI
	iscsiResp := ISCSIStats{
		ActiveSessions:  h.iscsi.ActiveSessions(),
		ReadBytesTotal:  h.iscsi.ReadBytesTotal(),
		WriteBytesTotal: h.iscsi.WriteBytesTotal(),
		ReadOpsTotal:    h.iscsi.ReadOpsTotal(),
		WriteOpsTotal:   h.iscsi.WriteOpsTotal(),
		ReadLatencyP99:  h.iscsi.ReadLatencyP99Ms(),
		WriteLatencyP99: h.iscsi.WriteLatencyP99Ms(),
	}

	writeJSON(w, http.StatusOK, StatsResponse{
		ISCSI:   iscsiResp,
		GC:      gcResp,
		Buffer:  bufResp,
		Journal: journalResp,
		Device:  deviceResp,
	})
}

// TriggerGC triggers a manual GC cycle and returns 202 Accepted.
func (h *Handler) TriggerGC(w http.ResponseWriter, r *http.Request) {
	h.ztl.TriggerGC()
	w.WriteHeader(http.StatusAccepted)
}

// GetConfig returns the current configuration with secrets redacted.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfg

	resp := ConfigResponse{
		Target: ConfigTargetResponse{
			IQN:         cfg.Target.IQN,
			Portal:      cfg.Target.Portal,
			MaxSessions: cfg.Target.MaxSessions,
			AuthEnabled: cfg.Target.Auth.Enabled,
			CHAPUser:    cfg.Target.Auth.CHAPUser,
			// CHAPSecret intentionally omitted
		},
		Device: ConfigDeviceResponse{
			Backend:      cfg.Device.Backend,
			Path:         cfg.Device.Path,
			ZoneCount:    cfg.Device.Emulator.ZoneCount,
			ZoneSizeMB:   cfg.Device.Emulator.ZoneSizeMB,
			MaxOpenZones: cfg.Device.Emulator.MaxOpenZones,
		},
		ZTL: ConfigZTLResponse{
			SegmentSizeKB:        cfg.ZTL.SegmentSizeKB,
			BufferSizeMB:         cfg.ZTL.BufferSizeMB,
			BufferFlushAgeSec:    cfg.ZTL.BufferFlushAgeSec,
			GCTriggerFreeRatio:   cfg.ZTL.GCTriggerFreeRatio,
			GCEmergencyFreeZones: cfg.ZTL.GCEmergencyFreeZones,
		},
		APIListen: cfg.API.Listen,
	}

	writeJSON(w, http.StatusOK, resp)
}

// zoneStateName converts a ZoneState to a human-readable string.
func zoneStateName(state zbc.ZoneState) string {
	switch state {
	case zbc.ZoneStateEmpty:
		return "empty"
	case zbc.ZoneStateImplicitOpen:
		return "implicit_open"
	case zbc.ZoneStateExplicitOpen:
		return "explicit_open"
	case zbc.ZoneStateClosed:
		return "closed"
	case zbc.ZoneStateFull:
		return "full"
	case zbc.ZoneStateReadOnly:
		return "read_only"
	case zbc.ZoneStateOffline:
		return "offline"
	default:
		return "unknown"
	}
}
