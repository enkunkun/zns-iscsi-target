// Package config provides configuration loading and management.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration struct.
type Config struct {
	Target  TargetConfig  `yaml:"target"`
	Device  DeviceConfig  `yaml:"device"`
	ZTL     ZTLConfig     `yaml:"ztl"`
	Journal JournalConfig `yaml:"journal"`
	API     APIConfig     `yaml:"api"`
	Log     LogConfig     `yaml:"log"`
}

// TargetConfig holds iSCSI target configuration.
type TargetConfig struct {
	IQN         string     `yaml:"iqn"`
	Portal      string     `yaml:"portal"`
	MaxSessions int        `yaml:"max_sessions"`
	Auth        AuthConfig `yaml:"auth"`
}

// AuthConfig holds CHAP authentication configuration.
type AuthConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CHAPUser   string `yaml:"chap_user"`
	CHAPSecret string `yaml:"chap_secret"`
}

// DeviceConfig holds device backend configuration.
type DeviceConfig struct {
	Backend  string          `yaml:"backend"`
	Path     string          `yaml:"path"`
	Emulator EmulatorConfig  `yaml:"emulator"`
}

// EmulatorConfig holds in-memory emulator configuration.
type EmulatorConfig struct {
	ZoneCount    int `yaml:"zone_count"`
	ZoneSizeMB   int `yaml:"zone_size_mb"`
	MaxOpenZones int `yaml:"max_open_zones"`
}

// ZTLConfig holds Zone Translation Layer configuration.
type ZTLConfig struct {
	SegmentSizeKB         int     `yaml:"segment_size_kb"`
	BufferSizeMB          int     `yaml:"buffer_size_mb"`
	BufferFlushAgeSec     int     `yaml:"buffer_flush_age_sec"`
	GCTriggerFreeRatio    float64 `yaml:"gc_trigger_free_ratio"`
	GCEmergencyFreeZones  int     `yaml:"gc_emergency_free_zones"`
}

// JournalConfig holds write-ahead journal configuration.
type JournalConfig struct {
	Path                  string `yaml:"path"`
	CheckpointIntervalSec int    `yaml:"checkpoint_interval_sec"`
	SyncPeriodMs          int    `yaml:"sync_period_ms"`
	MaxSizeMB             int    `yaml:"max_size_mb"`
}

// APIConfig holds REST API configuration.
type APIConfig struct {
	Listen string `yaml:"listen"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads configuration from a YAML file and applies defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Target.IQN == "" {
		return fmt.Errorf("target.iqn is required")
	}
	if c.Target.MaxSessions <= 0 {
		return fmt.Errorf("target.max_sessions must be positive")
	}
	if c.Device.Backend != "emulator" && c.Device.Backend != "smr" {
		return fmt.Errorf("device.backend must be 'emulator' or 'smr', got %q", c.Device.Backend)
	}
	if c.Device.Backend == "smr" && c.Device.Path == "" {
		return fmt.Errorf("device.path is required when backend is 'smr'")
	}
	if c.Device.Emulator.ZoneCount <= 0 {
		return fmt.Errorf("device.emulator.zone_count must be positive")
	}
	if c.Device.Emulator.ZoneSizeMB <= 0 {
		return fmt.Errorf("device.emulator.zone_size_mb must be positive")
	}
	if c.Device.Emulator.MaxOpenZones <= 0 {
		return fmt.Errorf("device.emulator.max_open_zones must be positive")
	}
	if c.ZTL.SegmentSizeKB <= 0 {
		return fmt.Errorf("ztl.segment_size_kb must be positive")
	}
	if c.ZTL.GCTriggerFreeRatio <= 0 || c.ZTL.GCTriggerFreeRatio >= 1 {
		return fmt.Errorf("ztl.gc_trigger_free_ratio must be between 0 and 1")
	}
	if c.Journal.SyncPeriodMs <= 0 {
		return fmt.Errorf("journal.sync_period_ms must be positive")
	}
	return nil
}

// SectorsPerSegment returns the number of 512-byte sectors per segment.
func (c *Config) SectorsPerSegment() uint64 {
	return uint64(c.ZTL.SegmentSizeKB) * 1024 / 512
}

// ZoneSizeSectors returns the number of 512-byte sectors per zone.
func (c *Config) ZoneSizeSectors() uint64 {
	return uint64(c.Device.Emulator.ZoneSizeMB) * 1024 * 1024 / 512
}
