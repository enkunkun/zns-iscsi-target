package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

// startTestServer creates and starts a Server on a random port.
// It returns the server and its base URL (e.g. "http://127.0.0.1:12345").
func startTestServer(t *testing.T) (*Server, string) {
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
				ZoneCount:    4,
				ZoneSizeMB:   1,
				MaxOpenZones: 2,
			},
		},
		ZTL: config.ZTLConfig{
			SegmentSizeKB:        8,
			BufferSizeMB:         64,
			BufferFlushAgeSec:    5,
			GCTriggerFreeRatio:   0.20,
			GCEmergencyFreeZones: 1,
		},
		Journal: config.JournalConfig{SyncPeriodMs: 10},
		API:     config.APIConfig{Listen: "127.0.0.1:0"},
	}

	dev, err := emulator.New(emulator.Config{
		ZoneCount:    cfg.Device.Emulator.ZoneCount,
		ZoneSizeMB:   cfg.Device.Emulator.ZoneSizeMB,
		MaxOpenZones: cfg.Device.Emulator.MaxOpenZones,
	})
	require.NoError(t, err)

	z, err := ztl.New(cfg, dev, nil)
	require.NoError(t, err)

	// Pick a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	srv := New(ServerConfig{
		ListenAddr:           addr,
		ZTL:                  &testZTL{z: z},
		Config:               cfg,
		PrometheusRegisterer: reg,
	})

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			// Ignore: expected when Shutdown is called.
		}
	}()

	// Wait for the server to be ready (poll health endpoint).
	baseURL := "http://" + addr
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/v1/health") //nolint:noctx
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = z.Close()
	})

	return srv, baseURL
}

func TestServerHealthEndToEnd(t *testing.T) {
	_, baseURL := startTestServer(t)

	resp, err := http.Get(baseURL + "/api/v1/health") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var body HealthResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body.Status)
}

func TestServerZonesEndToEnd(t *testing.T) {
	_, baseURL := startTestServer(t)

	resp, err := http.Get(baseURL + "/api/v1/zones") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body ZoneListResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 4, body.Total)
}

func TestServerStatsEndToEnd(t *testing.T) {
	_, baseURL := startTestServer(t)

	resp, err := http.Get(baseURL + "/api/v1/stats") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body StatsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "emulator", body.Device.Backend)
}

func TestServerGCTriggerEndToEnd(t *testing.T) {
	_, baseURL := startTestServer(t)

	resp, err := http.Post(baseURL+"/api/v1/gc/trigger", "application/json", nil) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func TestServerConfigEndToEnd(t *testing.T) {
	_, baseURL := startTestServer(t)

	resp, err := http.Get(baseURL + "/api/v1/config") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// CHAP secret must not appear in the response
	assert.NotContains(t, string(body), "supersecret")

	var cfgResp ConfigResponse
	require.NoError(t, json.Unmarshal(body, &cfgResp))
	assert.Equal(t, "iqn.2026-02.io.zns:target0", cfgResp.Target.IQN)
}

func TestServerMetricsEndToEnd(t *testing.T) {
	_, baseURL := startTestServer(t)

	resp, err := http.Get(baseURL + "/metrics") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.Contains(t, bodyStr, "zns_iscsi_active_sessions")
	assert.Contains(t, bodyStr, "zns_ztl_free_zones")
	assert.Contains(t, bodyStr, "zns_gc_running")
}

func TestPrometheusMetricsRegistration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	assert.NotNil(t, m)

	// Set some gauge values
	m.ISCSIActiveSessions.Set(3)
	m.ZTLFreeZones.Set(10)
	m.GCRunning.Set(0)
	m.BufferDirtyBytes.Set(1024)
	m.JournalLSN.Set(42)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	found := map[string]bool{}
	for _, mf := range mfs {
		found[mf.GetName()] = true
	}

	expectedMetrics := []string{
		"zns_iscsi_read_bytes_total",
		"zns_iscsi_write_bytes_total",
		"zns_iscsi_read_operations_total",
		"zns_iscsi_write_operations_total",
		"zns_iscsi_read_latency_seconds",
		"zns_iscsi_write_latency_seconds",
		"zns_iscsi_active_sessions",
		"zns_ztl_free_zones",
		"zns_ztl_open_zones",
		"zns_ztl_total_zones",
		"zns_gc_reclaimed_zones_total",
		"zns_gc_migrated_bytes_total",
		"zns_gc_running",
		"zns_buffer_dirty_bytes",
		"zns_buffer_pending_flushes",
		"zns_journal_lsn",
		"zns_journal_size_bytes",
		"zns_journal_checkpoint_lsn",
	}

	for _, name := range expectedMetrics {
		assert.True(t, found[name], "expected metric %q to be registered", name)
	}
}

func TestMetricsUpdateFromZTLIntegration(t *testing.T) {
	cfg := &config.Config{
		ZTL: config.ZTLConfig{
			SegmentSizeKB:        8,
			BufferSizeMB:         64,
			BufferFlushAgeSec:    5,
			GCTriggerFreeRatio:   0.20,
			GCEmergencyFreeZones: 1,
		},
		Journal: config.JournalConfig{SyncPeriodMs: 10},
	}

	dev, err := emulator.New(emulator.Config{ZoneCount: 6, ZoneSizeMB: 1, MaxOpenZones: 3})
	require.NoError(t, err)

	z, err := ztl.New(cfg, dev, nil)
	require.NoError(t, err)
	defer func() { _ = z.Close() }()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	provider := &testZTL{z: z}
	m.UpdateFromZTL(provider)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	getGauge := func(name string) float64 {
		for _, mf := range mfs {
			if mf.GetName() == name {
				return mf.Metric[0].GetGauge().GetValue()
			}
		}
		return -1
	}

	assert.Equal(t, float64(6), getGauge("zns_ztl_free_zones"),
		"all 6 zones should be free initially")
	assert.Equal(t, float64(6), getGauge("zns_ztl_total_zones"))
	assert.Equal(t, float64(0), getGauge("zns_gc_running"))
	assert.Equal(t, float64(0), getGauge("zns_buffer_dirty_bytes"))
}
