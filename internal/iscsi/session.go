package iscsi

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Session represents an iSCSI session identified by an ISID+TSIH pair.
type Session struct {
	ISID        [6]byte
	TSIH        uint16
	Params      Params
	connections []*Connection
	mu          sync.Mutex
}

// AddConnection adds a connection to the session.
func (s *Session) AddConnection(c *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections = append(s.connections, c)
}

// RemoveConnection removes a connection from the session.
func (s *Session) RemoveConnection(c *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, conn := range s.connections {
		if conn == c {
			s.connections = append(s.connections[:i], s.connections[i+1:]...)
			return
		}
	}
}

// ConnectionCount returns the number of active connections.
func (s *Session) ConnectionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.connections)
}

// SessionManager manages all iSCSI sessions for a target.
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[uint16]*Session
	maxSessions int
	nextTSIH    atomic.Uint32
}

// NewSessionManager creates a new SessionManager with the given max sessions limit.
func NewSessionManager(maxSessions int) *SessionManager {
	return &SessionManager{
		sessions:    make(map[uint16]*Session),
		maxSessions: maxSessions,
	}
}

// CreateSession creates a new session with a unique TSIH.
func (m *SessionManager) CreateSession(isid [6]byte, params Params) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.sessions) >= m.maxSessions {
		return nil, fmt.Errorf("maximum sessions (%d) reached", m.maxSessions)
	}

	// Assign a TSIH (1..65535, wrap around)
	tsih := uint16(m.nextTSIH.Add(1) & 0xFFFF)
	if tsih == 0 {
		tsih = uint16(m.nextTSIH.Add(1) & 0xFFFF)
	}

	s := &Session{
		ISID:   isid,
		TSIH:   tsih,
		Params: params,
	}
	m.sessions[tsih] = s
	return s, nil
}

// GetSession retrieves a session by TSIH.
func (m *SessionManager) GetSession(tsih uint16) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[tsih]
	return s, ok
}

// RemoveSession removes a session.
func (m *SessionManager) RemoveSession(tsih uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tsih)
}

// SessionCount returns the number of active sessions.
func (m *SessionManager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
