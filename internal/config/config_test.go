package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "iqn.2026-02.io.zns:target0", cfg.Target.IQN)
	assert.Equal(t, 4, cfg.Target.MaxSessions)
	assert.Equal(t, "emulator", cfg.Device.Backend)
	assert.Equal(t, 64, cfg.Device.Emulator.ZoneCount)
	assert.Equal(t, 256, cfg.Device.Emulator.ZoneSizeMB)
	assert.Equal(t, 14, cfg.Device.Emulator.MaxOpenZones)
	assert.Equal(t, 8, cfg.ZTL.SegmentSizeKB)
	assert.InDelta(t, 0.20, cfg.ZTL.GCTriggerFreeRatio, 0.001)
	assert.Equal(t, 10, cfg.Journal.SyncPeriodMs)
}

func TestLoadValidConfig(t *testing.T) {
	content := `
target:
  iqn: "iqn.2026-02.io.test:target1"
  portal: "127.0.0.1:3260"
  max_sessions: 2
  auth:
    enabled: false

device:
  backend: "emulator"
  emulator:
    zone_count: 32
    zone_size_mb: 128
    max_open_zones: 8

ztl:
  segment_size_kb: 8
  buffer_size_mb: 256
  buffer_flush_age_sec: 10
  gc_trigger_free_ratio: 0.30
  gc_emergency_free_zones: 2

journal:
  path: "/tmp/test.wal"
  checkpoint_interval_sec: 30
  sync_period_ms: 5
  max_size_mb: 512

api:
  listen: "127.0.0.1:8080"

log:
  level: "debug"
  format: "text"
`
	path := writeConfigFile(t, content)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "iqn.2026-02.io.test:target1", cfg.Target.IQN)
	assert.Equal(t, "127.0.0.1:3260", cfg.Target.Portal)
	assert.Equal(t, 2, cfg.Target.MaxSessions)
	assert.Equal(t, "emulator", cfg.Device.Backend)
	assert.Equal(t, 32, cfg.Device.Emulator.ZoneCount)
	assert.Equal(t, 128, cfg.Device.Emulator.ZoneSizeMB)
	assert.Equal(t, 8, cfg.Device.Emulator.MaxOpenZones)
	assert.Equal(t, 8, cfg.ZTL.SegmentSizeKB)
	assert.InDelta(t, 0.30, cfg.ZTL.GCTriggerFreeRatio, 0.001)
	assert.Equal(t, "/tmp/test.wal", cfg.Journal.Path)
	assert.Equal(t, 5, cfg.Journal.SyncPeriodMs)
}

func TestLoadMissingOptionalFields(t *testing.T) {
	// Only required fields
	content := `
target:
  iqn: "iqn.2026-02.io.test:target0"
  portal: "0.0.0.0:3260"
  max_sessions: 4

device:
  backend: "emulator"
  emulator:
    zone_count: 64
    zone_size_mb: 256
    max_open_zones: 14

ztl:
  segment_size_kb: 8
  gc_trigger_free_ratio: 0.20

journal:
  path: "/tmp/test.wal"
  sync_period_ms: 10
`
	path := writeConfigFile(t, content)
	cfg, err := Load(path)
	require.NoError(t, err)
	// Optional fields should have defaults or zero values
	assert.Equal(t, "iqn.2026-02.io.test:target0", cfg.Target.IQN)
	assert.Equal(t, 64, cfg.Device.Emulator.ZoneCount)
}

func TestLoadInvalidYAML(t *testing.T) {
	content := `this: is: invalid: yaml: [`
	path := writeConfigFile(t, content)
	_, err := Load(path)
	assert.Error(t, err)
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
}

func TestValidateInvalidBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.Backend = "invalid"
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "device.backend")
}

func TestValidateSMRRequiresPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.Backend = "smr"
	cfg.Device.Path = ""
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "device.path")
}

func TestValidateInvalidGCRatio(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ZTL.GCTriggerFreeRatio = 1.5
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestSectorsPerSegment(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ZTL.SegmentSizeKB = 8
	// 8KB / 512 = 16 sectors
	assert.Equal(t, uint64(16), cfg.SectorsPerSegment())
}

func TestZoneSizeSectors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.Emulator.ZoneSizeMB = 256
	// 256MB / 512 = 524288 sectors
	assert.Equal(t, uint64(524288), cfg.ZoneSizeSectors())
}
