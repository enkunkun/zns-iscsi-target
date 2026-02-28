package config

// DefaultConfig returns a Config with all default values applied.
func DefaultConfig() *Config {
	return &Config{
		Target: TargetConfig{
			IQN:         "iqn.2026-02.io.zns:target0",
			Portal:      "0.0.0.0:3260",
			MaxSessions: 4,
			Auth: AuthConfig{
				Enabled: false,
			},
		},
		Device: DeviceConfig{
			Backend: "emulator",
			Emulator: EmulatorConfig{
				ZoneCount:    64,
				ZoneSizeMB:   256,
				MaxOpenZones: 14,
			},
		},
		ZTL: ZTLConfig{
			SegmentSizeKB:        8,
			BufferSizeMB:         512,
			BufferFlushAgeSec:    5,
			GCTriggerFreeRatio:   0.20,
			GCEmergencyFreeZones: 3,
		},
		Journal: JournalConfig{
			Path:                  "/var/lib/zns-iscsi/journal.wal",
			CheckpointIntervalSec: 60,
			SyncPeriodMs:          10,
			MaxSizeMB:             1024,
		},
		API: APIConfig{
			Listen: "0.0.0.0:8080",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}
