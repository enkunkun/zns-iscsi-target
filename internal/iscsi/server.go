package iscsi

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/enkunkun/zns-iscsi-target/internal/scsi"
)

// Server is the iSCSI target TCP server.
type Server struct {
	cfg            *config.Config
	target         *Target
	scsiHandler    *scsi.Handler
	sessionManager *SessionManager

	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	stopped  bool
}

// NewServer creates a new iSCSI target Server.
func NewServer(cfg *config.Config, target *Target, scsiHandler *scsi.Handler) *Server {
	return &Server{
		cfg:            cfg,
		target:         target,
		scsiHandler:    scsiHandler,
		sessionManager: NewSessionManager(cfg.Target.MaxSessions),
	}
}

// Listen starts the TCP listener and accept loop.
// This function blocks until the server is stopped or an error occurs.
func (s *Server) Listen(ctx context.Context) error {
	addr := s.cfg.Target.Portal
	if addr == "" {
		addr = "0.0.0.0:3260"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	log.Printf("iSCSI target listening on %s", addr)

	// Close listener when context is done
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			stopped := s.stopped
			s.mu.Unlock()
			if stopped {
				return nil
			}
			return fmt.Errorf("accepting connection: %w", err)
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(conn)
		}()
	}
}

// Shutdown gracefully shuts down the server.
// It closes the listener and waits for all connections to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	ln := s.listener
	s.mu.Unlock()

	if ln != nil {
		ln.Close()
	}

	// Wait for connections with context cancellation
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handleConnection handles a newly accepted TCP connection.
// It performs login negotiation, then hands off to the full-feature read loop.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("new connection from %s", remoteAddr)

	// Perform login
	auth := AuthConfig{
		Enabled:    s.cfg.Target.Auth.Enabled,
		CHAPUser:   s.cfg.Target.Auth.CHAPUser,
		CHAPSecret: s.cfg.Target.Auth.CHAPSecret,
	}
	loginH := newLoginHandler(conn, auth, s.target.IQN, 0)
	params, cmdSN, err := loginH.Run()
	if err != nil {
		log.Printf("login failed from %s: %v", remoteAddr, err)
		return
	}

	params.TargetName = s.target.IQN

	// Create session
	var isid [6]byte
	copy(isid[:], loginH.isid[:])

	session, err := s.sessionManager.CreateSession(isid, params)
	if err != nil {
		log.Printf("session creation failed: %v", err)
		return
	}
	defer s.sessionManager.RemoveSession(session.TSIH)

	// Create connection handler
	c := newConnection(conn, session, params, s.scsiHandler)
	c.statSN = loginH.statSN
	c.expCmdSN = cmdSN + 1
	c.maxCmdSN = cmdSN + 32

	session.AddConnection(c)

	log.Printf("session %d established with %s (initiator: %s)",
		session.TSIH, remoteAddr, params.InitiatorName)

	// Run the full-feature phase read loop
	c.Run()

	log.Printf("connection from %s closed (session %d)", remoteAddr, session.TSIH)
}

// SessionCount returns the number of active sessions.
func (s *Server) SessionCount() int {
	return s.sessionManager.SessionCount()
}
