package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// testZTL wraps *ztl.ZTL to satisfy ZTLProvider.
// *ztl.ZTL already has ZoneManager(), GCStats(), WriteBuffer(), TriggerGC() methods.
type testZTL struct {
	z *ztl.ZTL
}

func (t *testZTL) ZoneManager() *ztl.ZoneManager { return t.z.ZoneManager() }
func (t *testZTL) GCStats() *ztl.GCStats         { return t.z.GCStats() }
func (t *testZTL) WriteBuffer() *ztl.WriteBuffer  { return t.z.WriteBuffer() }
func (t *testZTL) TriggerGC()                     { t.z.TriggerGC() }

// newTestHandler creates a Handler wired to a minimal in-memory ZTL + emulator.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	cfg := &config.Config{
		Target: config.TargetConfig{
			IQN:         "iqn.2026-02.io.zns:target0",
			Portal:      "0.0.0.0:3260",
			MaxSessions: 4,
			Auth: config.AuthConfig{
				Enabled:    true,
				CHAPUser:   "admin",
				CHAPSecret: "supersecret",
			},
		},
		Device: config.DeviceConfig{
			Backend: "emulator",
			Emulator: config.EmulatorConfig{
				ZoneCount:    8,
				ZoneSizeMB:   1,
				MaxOpenZones: 4,
			},
		},
		ZTL: config.ZTLConfig{
			SegmentSizeKB:        8,
			BufferSizeMB:         64,
			BufferFlushAgeSec:    5,
			GCTriggerFreeRatio:   0.20,
			GCEmergencyFreeZones: 2,
		},
		Journal: config.JournalConfig{
			SyncPeriodMs: 10,
		},
		API: config.APIConfig{Listen: "127.0.0.1:8080"},
	}

	dev, err := emulator.New(emulator.Config{
		ZoneCount:    cfg.Device.Emulator.ZoneCount,
		ZoneSizeMB:   cfg.Device.Emulator.ZoneSizeMB,
		MaxOpenZones: cfg.Device.Emulator.MaxOpenZones,
	})
	require.NoError(t, err)

	z, err := ztl.New(cfg, dev, nil)
	require.NoError(t, err)

	t.Cleanup(func() { _ = z.Close() })

	return NewHandler(&testZTL{z: z}, cfg, HandlerConfig{})
}

// ---- Health ----------------------------------------------------------------

func TestHealth(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
}

// ---- ListZones -------------------------------------------------------------

func TestListZones(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/zones", nil)
	rec := httptest.NewRecorder()

	h.ListZones(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ZoneListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Emulator created 8 zones
	assert.Equal(t, 8, resp.Total)
	assert.Len(t, resp.Zones, 8)

	// All zones start empty
	for _, z := range resp.Zones {
		assert.Equal(t, "empty", z.State)
		assert.Equal(t, "sequential", z.Type)
		assert.Equal(t, uint32(0), z.ValidSegments)
	}
}

// ---- GetStats --------------------------------------------------------------

func TestGetStats(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rec := httptest.NewRecorder()

	h.GetStats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp StatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Device stats should reflect emulator config
	assert.Equal(t, "emulator", resp.Device.Backend)
	assert.Equal(t, 8, resp.Device.ZoneCount)
	assert.Equal(t, 4, resp.Device.MaxOpenZones)

	// GC not running initially
	assert.False(t, resp.GC.Running)
	assert.Equal(t, uint64(0), resp.GC.ZonesReclaimed)

	// Buffer empty initially
	assert.Equal(t, uint64(0), resp.Buffer.DirtyBytes)

	// iSCSI zeros (noop provider)
	assert.Equal(t, 0, resp.ISCSI.ActiveSessions)
}

// ---- TriggerGC -------------------------------------------------------------

func TestTriggerGC(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/gc/trigger", nil)
	rec := httptest.NewRecorder()

	h.TriggerGC(rec, req)

	// Should return 202 Accepted
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

// ---- GetConfig -------------------------------------------------------------

func TestGetConfig(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	h.GetConfig(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Target fields
	assert.Equal(t, "iqn.2026-02.io.zns:target0", resp.Target.IQN)
	assert.Equal(t, "0.0.0.0:3260", resp.Target.Portal)
	assert.True(t, resp.Target.AuthEnabled)
	assert.Equal(t, "admin", resp.Target.CHAPUser)

	// Raw JSON must NOT contain the CHAP secret
	rawJSON := rec.Body.String()
	assert.NotContains(t, rawJSON, "supersecret", "CHAP secret must be redacted")

	// ZTL fields
	assert.Equal(t, 8, resp.ZTL.SegmentSizeKB)
	assert.Equal(t, 64, resp.ZTL.BufferSizeMB)

	// Device fields
	assert.Equal(t, "emulator", resp.Device.Backend)
	assert.Equal(t, 8, resp.Device.ZoneCount)
}

// ---- content-type ----------------------------------------------------------

func TestContentTypeJSON(t *testing.T) {
	h := newTestHandler(t)

	endpoints := []struct {
		method string
		path   string
		fn     http.HandlerFunc
	}{
		{http.MethodGet, "/api/v1/health", h.Health},
		{http.MethodGet, "/api/v1/zones", h.ListZones},
		{http.MethodGet, "/api/v1/stats", h.GetStats},
		{http.MethodGet, "/api/v1/config", h.GetConfig},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			ep.fn(rec, req)
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		})
	}
}

// ---- ZoneStateName ---------------------------------------------------------

func TestZoneStateName(t *testing.T) {
	cases := []struct {
		state    zbc.ZoneState
		expected string
	}{
		{zbc.ZoneStateEmpty, "empty"},
		{zbc.ZoneStateImplicitOpen, "implicit_open"},
		{zbc.ZoneStateExplicitOpen, "explicit_open"},
		{zbc.ZoneStateClosed, "closed"},
		{zbc.ZoneStateFull, "full"},
		{zbc.ZoneStateReadOnly, "read_only"},
		{zbc.ZoneStateOffline, "offline"},
		{zbc.ZoneState(0xFF), "unknown"},
	}

	for _, c := range cases {
		name := zoneStateName(c.state)
		assert.Equal(t, c.expected, name, "state=%v", c.state)
	}
}
