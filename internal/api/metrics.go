package api

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the ZNS iSCSI target.
type Metrics struct {
	// iSCSI metrics
	ISCSIReadBytesTotal   prometheus.Counter
	ISCSIWriteBytesTotal  prometheus.Counter
	ISCSIReadOpsTotal     prometheus.Counter
	ISCSIWriteOpsTotal    prometheus.Counter
	ISCSIReadLatency      prometheus.Histogram
	ISCSIWriteLatency     prometheus.Histogram
	ISCSIActiveSessions   prometheus.Gauge

	// ZTL / GC metrics
	ZTLFreeZones          prometheus.Gauge
	ZTLOpenZones          prometheus.Gauge
	ZTLTotalZones         prometheus.Gauge
	GCReclaimedZonesTotal prometheus.Counter
	GCMigratedBytesTotal  prometheus.Counter
	GCRunning             prometheus.Gauge

	// Buffer metrics
	BufferDirtyBytes     prometheus.Gauge
	BufferPendingFlushes prometheus.Gauge

	// Journal metrics
	JournalLSN           prometheus.Gauge
	JournalSizeBytes     prometheus.Gauge
	JournalCheckpointLSN prometheus.Gauge
}

// NewMetrics creates and registers all Prometheus metrics with the given registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)

	latencyBuckets := []float64{0.001, 0.005, 0.010, 0.050, 0.100, 0.500}

	return &Metrics{
		// iSCSI
		ISCSIReadBytesTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "zns_iscsi_read_bytes_total",
			Help: "Total bytes read via iSCSI.",
		}),
		ISCSIWriteBytesTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "zns_iscsi_write_bytes_total",
			Help: "Total bytes written via iSCSI.",
		}),
		ISCSIReadOpsTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "zns_iscsi_read_operations_total",
			Help: "Total read operations via iSCSI.",
		}),
		ISCSIWriteOpsTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "zns_iscsi_write_operations_total",
			Help: "Total write operations via iSCSI.",
		}),
		ISCSIReadLatency: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "zns_iscsi_read_latency_seconds",
			Help:    "Read latency distribution in seconds.",
			Buckets: latencyBuckets,
		}),
		ISCSIWriteLatency: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "zns_iscsi_write_latency_seconds",
			Help:    "Write latency distribution in seconds.",
			Buckets: latencyBuckets,
		}),
		ISCSIActiveSessions: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_iscsi_active_sessions",
			Help: "Number of currently active iSCSI sessions.",
		}),

		// ZTL / GC
		ZTLFreeZones: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_ztl_free_zones",
			Help: "Number of free (empty) zones.",
		}),
		ZTLOpenZones: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_ztl_open_zones",
			Help: "Number of currently open zones.",
		}),
		ZTLTotalZones: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_ztl_total_zones",
			Help: "Total number of zones on the device.",
		}),
		GCReclaimedZonesTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "zns_gc_reclaimed_zones_total",
			Help: "Total zones reclaimed by garbage collection.",
		}),
		GCMigratedBytesTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "zns_gc_migrated_bytes_total",
			Help: "Total bytes migrated by garbage collection.",
		}),
		GCRunning: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_gc_running",
			Help: "1 if garbage collection is currently running, 0 otherwise.",
		}),

		// Buffer
		BufferDirtyBytes: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_buffer_dirty_bytes",
			Help: "Bytes currently buffered (not yet flushed to device).",
		}),
		BufferPendingFlushes: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_buffer_pending_flushes",
			Help: "Number of zones with pending buffer flushes.",
		}),

		// Journal
		JournalLSN: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_journal_lsn",
			Help: "Current Log Sequence Number of the WAL journal.",
		}),
		JournalSizeBytes: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_journal_size_bytes",
			Help: "Current size of the WAL journal file in bytes.",
		}),
		JournalCheckpointLSN: factory.NewGauge(prometheus.GaugeOpts{
			Name: "zns_journal_checkpoint_lsn",
			Help: "LSN of the last checkpoint.",
		}),
	}
}

// UpdateFromZTL refreshes gauge metrics from ZTL state.
func (m *Metrics) UpdateFromZTL(ztlProv ZTLProvider) {
	zm := ztlProv.ZoneManager()
	m.ZTLFreeZones.Set(float64(zm.FreeZoneCount()))
	m.ZTLOpenZones.Set(float64(zm.OpenZoneCount()))
	m.ZTLTotalZones.Set(float64(zm.TotalZoneCount()))

	gcSnap := ztlProv.GCStats().Snapshot()
	if gcSnap.Running {
		m.GCRunning.Set(1)
	} else {
		m.GCRunning.Set(0)
	}

	buf := ztlProv.WriteBuffer()
	m.BufferDirtyBytes.Set(float64(buf.DirtyBytes()))
	m.BufferPendingFlushes.Set(float64(buf.ZoneCount()))
}

// UpdateFromISCSI refreshes iSCSI gauge metrics.
func (m *Metrics) UpdateFromISCSI(iscsi ISCSIStatsProvider) {
	m.ISCSIActiveSessions.Set(float64(iscsi.ActiveSessions()))
}
