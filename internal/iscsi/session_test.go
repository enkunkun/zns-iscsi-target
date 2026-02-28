package iscsi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManagerCreateAndGet(t *testing.T) {
	sm := NewSessionManager(4)

	var isid [6]byte
	isid[0] = 0x00
	isid[1] = 0x02

	params := DefaultTargetParams()
	params.InitiatorName = "iqn.test.initiator"

	session, err := sm.CreateSession(isid, params)
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.Equal(t, isid, session.ISID)
	assert.Equal(t, params.InitiatorName, session.Params.InitiatorName)
	assert.NotEqual(t, uint16(0), session.TSIH)

	// Get it back
	got, ok := sm.GetSession(session.TSIH)
	require.True(t, ok)
	assert.Equal(t, session, got)
}

func TestSessionManagerMaxSessions(t *testing.T) {
	sm := NewSessionManager(2)

	var isid [6]byte

	for i := 0; i < 2; i++ {
		isid[0] = byte(i)
		_, err := sm.CreateSession(isid, DefaultTargetParams())
		require.NoError(t, err)
	}

	// Third session should fail
	isid[0] = 0xFF
	_, err := sm.CreateSession(isid, DefaultTargetParams())
	assert.Error(t, err, "should fail when max sessions reached")
}

func TestSessionManagerRemove(t *testing.T) {
	sm := NewSessionManager(4)
	var isid [6]byte

	s, err := sm.CreateSession(isid, DefaultTargetParams())
	require.NoError(t, err)
	assert.Equal(t, 1, sm.SessionCount())

	sm.RemoveSession(s.TSIH)
	assert.Equal(t, 0, sm.SessionCount())

	_, ok := sm.GetSession(s.TSIH)
	assert.False(t, ok)
}

func TestSessionManagerGetNonExistent(t *testing.T) {
	sm := NewSessionManager(4)
	_, ok := sm.GetSession(9999)
	assert.False(t, ok)
}

func TestSessionConnectionManagement(t *testing.T) {
	s := &Session{TSIH: 1}

	assert.Equal(t, 0, s.ConnectionCount())

	c1 := &Connection{}
	c2 := &Connection{}

	s.AddConnection(c1)
	assert.Equal(t, 1, s.ConnectionCount())

	s.AddConnection(c2)
	assert.Equal(t, 2, s.ConnectionCount())

	s.RemoveConnection(c1)
	assert.Equal(t, 1, s.ConnectionCount())

	s.RemoveConnection(c2)
	assert.Equal(t, 0, s.ConnectionCount())
}

func TestSessionManagerSessionCount(t *testing.T) {
	sm := NewSessionManager(10)
	assert.Equal(t, 0, sm.SessionCount())

	var isid [6]byte
	s1, _ := sm.CreateSession(isid, DefaultTargetParams())
	assert.Equal(t, 1, sm.SessionCount())

	isid[0] = 1
	s2, _ := sm.CreateSession(isid, DefaultTargetParams())
	assert.Equal(t, 2, sm.SessionCount())

	sm.RemoveSession(s1.TSIH)
	assert.Equal(t, 1, sm.SessionCount())

	sm.RemoveSession(s2.TSIH)
	assert.Equal(t, 0, sm.SessionCount())
}

func TestSessionManagerUniqueISIDs(t *testing.T) {
	sm := NewSessionManager(10)

	// Create two sessions with different ISIDs - both should succeed
	var isid1, isid2 [6]byte
	isid1[0] = 0x01
	isid2[0] = 0x02

	s1, err := sm.CreateSession(isid1, DefaultTargetParams())
	require.NoError(t, err)

	s2, err := sm.CreateSession(isid2, DefaultTargetParams())
	require.NoError(t, err)

	// TSIHs should be different
	assert.NotEqual(t, s1.TSIH, s2.TSIH)
}
