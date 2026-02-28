// Package api implements the REST API server for the ZNS iSCSI target.
package api

import (
	"context"
	"embed"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/enkunkun/zns-iscsi-target/internal/config"
)

//go:embed web
var embeddedWebFS embed.FS

// Server is the REST API HTTP server.
type Server struct {
	httpServer *http.Server
	handler    *Handler
	metrics    *Metrics
}

// ServerConfig holds the configuration for the API server.
type ServerConfig struct {
	// ListenAddr is the TCP address to listen on (e.g. "0.0.0.0:8080").
	ListenAddr string
	// ZTL is the ZTL provider. Required.
	ZTL ZTLProvider
	// Config is the application config. Required.
	Config *config.Config
	// Handler config (optional deps).
	HandlerConfig HandlerConfig
	// WebDir is the path to a directory containing the built React app.
	// If set and the directory exists, it takes priority over the embedded FS.
	WebDir string
	// PrometheusRegisterer allows injecting a custom registry (useful in tests).
	// If nil, prometheus.DefaultRegisterer is used.
	PrometheusRegisterer prometheus.Registerer
}

// New creates a new API Server with a configured Chi router.
func New(cfg ServerConfig) *Server {
	reg := cfg.PrometheusRegisterer
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	metrics := NewMetrics(reg)
	handler := NewHandler(cfg.ZTL, cfg.Config, cfg.HandlerConfig)

	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// API routes
	r.Get("/api/v1/health", handler.Health)
	r.Get("/api/v1/zones", handler.ListZones)
	r.Get("/api/v1/stats", handler.GetStats)
	r.Post("/api/v1/gc/trigger", handler.TriggerGC)
	r.Get("/api/v1/config", handler.GetConfig)

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.HandlerFor(
		prometheusGathererFrom(reg),
		promhttp.HandlerOpts{},
	))

	// Serve React app: prefer runtime WebDir, fall back to embedded FS
	var webFS http.FileSystem
	if cfg.WebDir != "" {
		if info, err := os.Stat(cfg.WebDir); err == nil && info.IsDir() {
			webFS = http.Dir(cfg.WebDir)
		}
	}
	if webFS == nil {
		if sub, err := fs.Sub(embeddedWebFS, "web"); err == nil {
			webFS = http.FS(sub)
		}
	}
	if webFS != nil {
		r.Handle("/*", http.FileServer(webFS))
	} else {
		r.Handle("/*", http.NotFoundHandler())
	}

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = cfg.Config.API.Listen
	}

	s := &Server{
		handler: handler,
		metrics: metrics,
	}
	s.httpServer = &http.Server{
		Addr:              listenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s
}

// prometheusGathererFrom returns a prometheus.Gatherer from a Registerer.
// If the Registerer also implements Gatherer, it is used directly.
// Otherwise falls back to prometheus.DefaultGatherer.
func prometheusGathererFrom(reg prometheus.Registerer) prometheus.Gatherer {
	if g, ok := reg.(prometheus.Gatherer); ok {
		return g
	}
	return prometheus.DefaultGatherer
}

// Start begins listening and serving HTTP requests. It blocks until the server
// is stopped or encounters a fatal error.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the server with the given context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Handler returns the underlying Handler (for testing).
func (s *Server) Handler() *Handler {
	return s.handler
}

// Metrics returns the Prometheus metrics collector.
func (s *Server) Metrics() *Metrics {
	return s.metrics
}

// Addr returns the configured listen address.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}
