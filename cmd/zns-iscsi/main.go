// Package main is the entry point for the ZNS/SMR iSCSI Target server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/api"
	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/enkunkun/zns-iscsi-target/internal/iscsi"
	"github.com/enkunkun/zns-iscsi-target/internal/journal"
	"github.com/enkunkun/zns-iscsi-target/internal/scsi"
	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

func main() {
	// 1. Parse flags
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// 2. Load config (with defaults)
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// 3. Setup structured logging
	logger := setupLogger(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(logger)

	slog.Info("starting ZNS iSCSI target",
		"backend", cfg.Device.Backend,
		"iqn", cfg.Target.IQN,
		"portal", cfg.Target.Portal,
		"api", cfg.API.Listen,
	)

	// 4. Create backend
	dev, err := createBackend(cfg)
	if err != nil {
		slog.Error("failed to create backend", "error", err)
		os.Exit(1)
	}
	defer dev.Close()

	slog.Info("backend initialized",
		"zones", dev.ZoneCount(),
		"zone_size_sectors", dev.ZoneSize(),
		"capacity_sectors", dev.Capacity(),
	)

	// 5. Ensure journal directory exists
	journalDir := journalDirOf(cfg.Journal.Path)
	if journalDir != "" {
		if err := os.MkdirAll(journalDir, 0755); err != nil {
			slog.Error("failed to create journal directory", "path", journalDir, "error", err)
			os.Exit(1)
		}
	}

	// Open journal
	j, err := journal.Open(cfg.Journal.Path)
	if err != nil {
		slog.Error("failed to open journal", "path", cfg.Journal.Path, "error", err)
		os.Exit(1)
	}
	defer j.Close()

	slog.Info("journal opened", "path", cfg.Journal.Path, "lsn", j.CurrentLSN())

	// 6. Create ZTL
	z, err := ztl.New(cfg, dev, j)
	if err != nil {
		slog.Error("failed to create ZTL", "error", err)
		os.Exit(1)
	}
	defer z.Close()

	slog.Info("ZTL initialized")

	// 7. Run crash recovery (after ZTL is created so L2P and ZM are ready)
	if err := runRecovery(j, z); err != nil {
		slog.Error("crash recovery failed", "error", err)
		os.Exit(1)
	}

	// Start journal group commit loop
	commitCtx, commitCancel := context.WithCancel(context.Background())
	defer commitCancel()
	j.StartGroupCommitLoop(commitCtx, cfg.Journal.SyncPeriodMs)

	// 8. Create SCSI handler with ZTL as BlockDevice
	var serialNumber string = "ZNS-ISCSI-001"
	var devID [16]byte
	copy(devID[:], []byte("zns-iscsi-target"))
	scsiHandler := scsi.NewHandler(z, serialNumber, devID)

	// 9. Create iSCSI target and server
	target := iscsi.NewTarget(cfg.Target.IQN, cfg.Target.Portal)
	target.AddLUN(0, z.BlockSize(), z.Capacity())
	iscsiServer := iscsi.NewServer(cfg, target, scsiHandler)

	// 10. Create API server
	iscsiStats := &iscsiStatsAdapter{server: iscsiServer}
	journalStats := &journalStatsAdapter{journal: j}
	apiServer := api.New(api.ServerConfig{
		ListenAddr: cfg.API.Listen,
		ZTL:        z,
		Config:     cfg,
		WebDir:     "/var/lib/zns-iscsi/web",
		HandlerConfig: api.HandlerConfig{
			ISCSI:   iscsiStats,
			Journal: journalStats,
		},
	})

	// 11. Start servers in goroutines
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	errCh := make(chan error, 2)

	go func() {
		slog.Info("iSCSI server starting", "addr", cfg.Target.Portal)
		if err := iscsiServer.Listen(serverCtx); err != nil {
			errCh <- fmt.Errorf("iSCSI server: %w", err)
		}
	}()

	go func() {
		slog.Info("API server starting", "addr", cfg.API.Listen)
		if err := apiServer.Start(); err != nil {
			errCh <- fmt.Errorf("API server: %w", err)
		}
	}()

	// 12. Wait for SIGTERM/SIGINT or server error
	sigCh := make(chan os.Signal, 1)
	notifySignals(sigCh)

	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
	case err := <-errCh:
		slog.Error("server error", "error", err)
	}

	// 13. Graceful shutdown
	slog.Info("initiating graceful shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop iSCSI server
	serverCancel()
	if err := iscsiServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("iSCSI server shutdown error", "error", err)
	}

	// Stop API server
	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("API server shutdown error", "error", err)
	}

	// Stop journal group commit loop and flush ZTL
	commitCancel()

	if err := z.Flush(); err != nil {
		slog.Warn("ZTL flush error", "error", err)
	}

	slog.Info("shutdown complete")
}

// loadConfig loads configuration from a file, falling back to defaults if the file does not exist.
func loadConfig(path string) (*config.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Info("config file not found, using defaults", "path", path)
		return config.DefaultConfig(), nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading config from %q: %w", path, err)
	}
	return cfg, nil
}

// setupLogger creates an slog.Logger based on the configured level and format.
func setupLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(h)
}

// createBackend creates the appropriate backend based on configuration.
func createBackend(cfg *config.Config) (backend.ZonedDevice, error) {
	switch cfg.Device.Backend {
	case "emulator":
		em, err := emulator.New(emulator.Config{
			ZoneCount:    cfg.Device.Emulator.ZoneCount,
			ZoneSizeMB:   cfg.Device.Emulator.ZoneSizeMB,
			MaxOpenZones: cfg.Device.Emulator.MaxOpenZones,
		})
		if err != nil {
			return nil, fmt.Errorf("creating emulator backend: %w", err)
		}
		return em, nil
	case "smr":
		return openSMRBackend(cfg.Device.Path)
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Device.Backend)
	}
}

// runRecovery performs crash recovery using the journal.
func runRecovery(j *journal.Journal, z *ztl.ZTL) error {
	slog.Info("running crash recovery")
	if err := journal.Recover(j, z.L2PTable(), z.ZoneManager(), z.Device()); err != nil {
		return fmt.Errorf("crash recovery: %w", err)
	}
	slog.Info("crash recovery complete")
	return nil
}

// journalDirOf returns the directory portion of the journal path, or "" if none.
func journalDirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

// iscsiStatsAdapter wraps the iSCSI server to satisfy api.ISCSIStatsProvider.
type iscsiStatsAdapter struct {
	server *iscsi.Server
}

func (a *iscsiStatsAdapter) ActiveSessions() int        { return a.server.SessionCount() }
func (a *iscsiStatsAdapter) ReadBytesTotal() uint64     { return 0 }
func (a *iscsiStatsAdapter) WriteBytesTotal() uint64    { return 0 }
func (a *iscsiStatsAdapter) ReadOpsTotal() uint64       { return 0 }
func (a *iscsiStatsAdapter) WriteOpsTotal() uint64      { return 0 }
func (a *iscsiStatsAdapter) ReadLatencyP99Ms() float64  { return 0 }
func (a *iscsiStatsAdapter) WriteLatencyP99Ms() float64 { return 0 }

// journalStatsAdapter wraps the journal to satisfy api.JournalStatsProvider.
type journalStatsAdapter struct {
	journal *journal.Journal
}

func (a *journalStatsAdapter) CurrentLSN() uint64    { return a.journal.CurrentLSN() }
func (a *journalStatsAdapter) SizeBytes() uint64     { return journalSizeBytes(a.journal.Path()) }
func (a *journalStatsAdapter) CheckpointLSN() uint64 { return 0 }

// journalSizeBytes returns the size of the journal file in bytes.
func journalSizeBytes(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return uint64(info.Size())
}
