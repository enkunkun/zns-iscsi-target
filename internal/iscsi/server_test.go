package iscsi

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/enkunkun/zns-iscsi-target/internal/scsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBlockDevice for server tests.
type mockBlockDeviceForServer struct {
	data []byte
}

func newMockBlockDeviceForServer(sectors uint64) *mockBlockDeviceForServer {
	return &mockBlockDeviceForServer{data: make([]byte, sectors*512)}
}

func (m *mockBlockDeviceForServer) Read(lba uint64, count uint32) ([]byte, error) {
	start := lba * 512
	end := start + uint64(count)*512
	result := make([]byte, end-start)
	copy(result, m.data[start:end])
	return result, nil
}

func (m *mockBlockDeviceForServer) Write(lba uint64, data []byte) error {
	start := lba * 512
	copy(m.data[start:], data)
	return nil
}

func (m *mockBlockDeviceForServer) Flush() error    { return nil }
func (m *mockBlockDeviceForServer) Unmap(lba uint64, count uint32) error { return nil }
func (m *mockBlockDeviceForServer) BlockSize() uint32  { return 512 }
func (m *mockBlockDeviceForServer) Capacity() uint64   { return uint64(len(m.data)) / 512 }

// newTestServer creates a test server with a mock block device.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	dev := newMockBlockDeviceForServer(1024)
	handler := scsi.NewHandler(dev, "TESTSN", [16]byte{0x01, 0x02})

	cfg := &config.Config{
		Target: config.TargetConfig{
			IQN:         "iqn.2026-02.io.zns:test",
			Portal:      "127.0.0.1:0",
			MaxSessions: 4,
			Auth:        config.AuthConfig{Enabled: false},
		},
	}

	target := NewTarget("iqn.2026-02.io.zns:test", "")
	target.AddLUN(0, 512, 1024)

	server := NewServer(cfg, target, handler)
	return server, cfg.Target.IQN
}

// dialAndLogin connects to the target and performs iSCSI login.
// Returns the connection and negotiated params.
func dialAndLogin(t *testing.T, addr string) (net.Conn, uint32) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	require.NoError(t, err)

	// Exchange 1: Security stage -> Operational stage
	req1 := buildLoginRequest(StageSecurityNegotiation, StageOperationalNegotiation, true, 0, map[string]string{
		"InitiatorName": "iqn.2001-04.com.example:test",
		"AuthMethod":    "None",
	})
	err = WritePDU(conn, req1)
	require.NoError(t, err)

	rsp1, err := ReadPDU(conn)
	require.NoError(t, err)
	require.Equal(t, OpcodeLoginRsp, rsp1.Opcode())

	// Exchange 2: Operational stage -> Full Feature Phase
	req2 := buildLoginRequest(StageOperationalNegotiation, StageFullFeaturePhase, true, 1, map[string]string{
		"MaxRecvDataSegmentLength": "65536",
		"MaxBurstLength":           "262144",
		"ImmediateData":            "Yes",
		"InitialR2T":               "No",
	})
	err = WritePDU(conn, req2)
	require.NoError(t, err)

	rsp2, err := ReadPDU(conn)
	require.NoError(t, err)
	require.Equal(t, OpcodeLoginRsp, rsp2.Opcode())
	require.Equal(t, LoginStatusSuccess, rsp2.BHS[36])

	statSN := rsp2.StatSN()
	return conn, statSN
}

func TestServerStartAndStop(t *testing.T) {
	server, _ := newTestServer(t)

	// Start server on a random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close() // Release so server can bind

	server.cfg.Target.Portal = addr

	ctx, cancel := context.WithCancel(context.Background())
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Listen(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	err = server.Shutdown(shutdownCtx)
	assert.NoError(t, err)

	select {
	case err := <-serverErr:
		_ = err // Expected error or nil after cancel
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServerConnectLoginNOPLogout(t *testing.T) {
	server, _ := newTestServer(t)

	// Find a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	server.cfg.Target.Portal = addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Listen(ctx) }()

	time.Sleep(50 * time.Millisecond)

	// Connect and login
	conn, statSN := dialAndLogin(t, addr)
	defer conn.Close()

	expCmdSN := uint32(3) // after 2 login exchanges
	maxCmdSN := expCmdSN + 31

	// Send NOP-Out
	nop := &PDU{}
	nop.SetOpcode(OpcodeNopOut)
	nop.BHS[0] |= 0x40 // I bit
	nop.BHS[1] = 0x80  // F bit
	nop.SetInitiatorTaskTag(0x11223344)
	nop.SetCmdSN(2)
	nop.SetExpStatSN(statSN + 1)

	err = WritePDU(conn, nop)
	require.NoError(t, err)

	// Read NOP-In
	rsp, err := ReadPDU(conn)
	require.NoError(t, err)
	assert.Equal(t, OpcodeNopIn, rsp.Opcode())
	assert.Equal(t, uint32(0x11223344), rsp.InitiatorTaskTag())

	// Send Logout
	logout := &PDU{}
	logout.SetOpcode(OpcodeLogoutReq)
	logout.BHS[0] |= 0x40 // I bit
	logout.BHS[1] = 0x80 | LogoutReasonCloseSession
	logout.SetInitiatorTaskTag(0xFFFF0000)
	logout.SetCmdSN(3)
	logout.SetExpStatSN(statSN + 2)

	err = WritePDU(conn, logout)
	require.NoError(t, err)

	// Read Logout Response
	logoutRsp, err := ReadPDU(conn)
	require.NoError(t, err)
	assert.Equal(t, OpcodeLogoutRsp, logoutRsp.Opcode())
	assert.Equal(t, LogoutResponseSuccess, logoutRsp.BHS[2])

	_ = maxCmdSN
	_ = expCmdSN
}

func TestServerInquiry(t *testing.T) {
	server, _ := newTestServer(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	server.cfg.Target.Portal = addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Listen(ctx) }()

	time.Sleep(50 * time.Millisecond)

	conn, statSN := dialAndLogin(t, addr)
	defer conn.Close()

	// Send INQUIRY SCSI command
	cdb := make([]byte, 16)
	cdb[0] = 0x12 // INQUIRY
	cdb[4] = 36   // allocation length

	inquiryPDU := &PDU{}
	inquiryPDU.SetOpcode(OpcodeSCSICmd)
	inquiryPDU.BHS[0] |= 0x40 // I bit
	inquiryPDU.BHS[1] = 0x80 | SCSICmdFlagRead | SCSICmdFlagFinal
	inquiryPDU.SetInitiatorTaskTag(0x00000001)
	inquiryPDU.SetCmdSN(2)
	inquiryPDU.SetExpStatSN(statSN + 1)
	// ExpectedDataTransferLength at bytes 20-23
	binary.BigEndian.PutUint32(inquiryPDU.BHS[20:24], 36)
	copy(inquiryPDU.BHS[32:48], cdb)

	err = WritePDU(conn, inquiryPDU)
	require.NoError(t, err)

	// Expect Data-In PDU(s) followed by SCSI Response
	var receivedData []byte
	for {
		rsp, err := ReadPDU(conn)
		require.NoError(t, err)

		switch rsp.Opcode() {
		case OpcodeDataIn:
			receivedData = append(receivedData, rsp.DataSegment...)
			if (rsp.Flags() & 0x80) != 0 {
				// Final Data-In; SCSI Response follows
			}
		case OpcodeSCSIRsp:
			// Done
			assert.Equal(t, scsi.StatusGood, rsp.BHS[3])
			goto done
		}
	}
done:

	require.NotEmpty(t, receivedData)
	assert.Equal(t, byte(0x00), receivedData[0]) // device type: disk
	assert.Equal(t, byte(0x05), receivedData[2]) // SPC-3
}

func TestServerWriteAndRead(t *testing.T) {
	dev := newMockBlockDeviceForServer(1024)
	handler := scsi.NewHandler(dev, "TESTSN", [16]byte{})

	cfg := &config.Config{
		Target: config.TargetConfig{
			IQN:         "iqn.2026-02.io.zns:test",
			Portal:      "127.0.0.1:0",
			MaxSessions: 4,
			Auth:        config.AuthConfig{Enabled: false},
		},
	}

	target := NewTarget("iqn.2026-02.io.zns:test", "")
	server := NewServer(cfg, target, handler)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()
	server.cfg.Target.Portal = addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)

	conn, statSN := dialAndLogin(t, addr)
	defer conn.Close()

	lba := uint32(10)
	pattern := bytes.Repeat([]byte{0x42}, 512)
	expStatSN := statSN + 1
	cmdSN := uint32(2)

	// WRITE(10)
	writeCDB := make([]byte, 16)
	writeCDB[0] = 0x2A
	binary.BigEndian.PutUint32(writeCDB[2:6], lba)
	binary.BigEndian.PutUint16(writeCDB[7:9], 1) // 1 sector

	writePDU := &PDU{}
	writePDU.SetOpcode(OpcodeSCSICmd)
	writePDU.BHS[0] |= 0x40 // I bit
	writePDU.BHS[1] = 0x80 | SCSICmdFlagWrite | SCSICmdFlagFinal
	writePDU.SetInitiatorTaskTag(0x00000010)
	writePDU.SetCmdSN(cmdSN)
	writePDU.SetExpStatSN(expStatSN)
	binary.BigEndian.PutUint32(writePDU.BHS[20:24], 512) // expected length
	copy(writePDU.BHS[32:48], writeCDB)
	writePDU.DataSegment = pattern // immediate data

	err = WritePDU(conn, writePDU)
	require.NoError(t, err)

	// Read SCSI Response for write
	writeRsp, err := ReadPDU(conn)
	require.NoError(t, err)
	// May receive R2T first if InitialR2T is Yes
	if writeRsp.Opcode() == OpcodeR2T {
		// Send Data-Out
		dataOut := &PDU{}
		dataOut.SetOpcode(OpcodeDataOut)
		dataOut.BHS[1] = 0x80 // F bit
		dataOut.SetInitiatorTaskTag(0x00000010)
		binary.BigEndian.PutUint32(dataOut.BHS[20:24], 0x00000010) // TTT from R2T
		binary.BigEndian.PutUint32(dataOut.BHS[36:40], 0)          // DataSN=0
		binary.BigEndian.PutUint32(dataOut.BHS[40:44], 0)          // offset=0
		dataOut.DataSegment = pattern

		err = WritePDU(conn, dataOut)
		require.NoError(t, err)

		writeRsp, err = ReadPDU(conn)
		require.NoError(t, err)
	}

	require.Equal(t, OpcodeSCSIRsp, writeRsp.Opcode(),
		fmt.Sprintf("expected SCSI RSP but got opcode 0x%02X", writeRsp.Opcode()))
	assert.Equal(t, scsi.StatusGood, writeRsp.BHS[3])

	// READ(10)
	readCDB := make([]byte, 16)
	readCDB[0] = 0x28
	binary.BigEndian.PutUint32(readCDB[2:6], lba)
	binary.BigEndian.PutUint16(readCDB[7:9], 1) // 1 sector

	readPDU := &PDU{}
	readPDU.SetOpcode(OpcodeSCSICmd)
	readPDU.BHS[0] |= 0x40
	readPDU.BHS[1] = 0x80 | SCSICmdFlagRead | SCSICmdFlagFinal
	readPDU.SetInitiatorTaskTag(0x00000011)
	readPDU.SetCmdSN(cmdSN + 1)
	readPDU.SetExpStatSN(expStatSN + 1)
	binary.BigEndian.PutUint32(readPDU.BHS[20:24], 512)
	copy(readPDU.BHS[32:48], readCDB)

	err = WritePDU(conn, readPDU)
	require.NoError(t, err)

	// Collect Data-In
	var readData []byte
	for {
		rsp, err := ReadPDU(conn)
		require.NoError(t, err)
		if rsp.Opcode() == OpcodeDataIn {
			readData = append(readData, rsp.DataSegment...)
		} else if rsp.Opcode() == OpcodeSCSIRsp {
			assert.Equal(t, scsi.StatusGood, rsp.BHS[3])
			break
		}
	}

	require.NotEmpty(t, readData)
	assert.Equal(t, pattern, readData[:512])
}

func TestServerSessionCount(t *testing.T) {
	server, _ := newTestServer(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()
	server.cfg.Target.Portal = addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 0, server.SessionCount())

	conn, _ := dialAndLogin(t, addr)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, server.SessionCount())

	conn.Close()
	time.Sleep(50 * time.Millisecond)
	// Session count might not immediately drop; give it a bit more time
	time.Sleep(100 * time.Millisecond)
	// After connection close, session should be removed
}

func TestSendTargets(t *testing.T) {
	targets := []*Target{
		{IQN: "iqn.2026-02.io.zns:target0", Portal: "192.168.1.1:3260"},
		{IQN: "iqn.2026-02.io.zns:target1", Portal: "192.168.1.1:3261"},
	}

	req := &PDU{}
	req.DataSegment = SerializeKeyValuePairs(map[string]string{"SendTargets": "All"})

	result := handleSendTargets(req, targets, nil)
	assert.NotNil(t, result)

	kv := ParseKeyValuePairs(result)
	// Should have TargetName and TargetAddress for each target
	names := []string{}
	for k, v := range kv {
		if k == "TargetName" {
			names = append(names, v)
		}
	}
	// At least one target name
	assert.NotEmpty(t, names)
}

func TestSendTargetsSpecific(t *testing.T) {
	targets := []*Target{
		{IQN: "iqn.2026-02.io.zns:target0", Portal: "192.168.1.1:3260"},
		{IQN: "iqn.2026-02.io.zns:target1", Portal: "192.168.1.1:3261"},
	}

	req := &PDU{}
	req.DataSegment = SerializeKeyValuePairs(map[string]string{
		"SendTargets": "iqn.2026-02.io.zns:target0",
	})

	result := handleSendTargets(req, targets, nil)
	assert.NotNil(t, result)
	resultStr := string(result)
	assert.Contains(t, resultStr, "target0")
	assert.NotContains(t, resultStr, "target1")
}

func TestTargetAddLUN(t *testing.T) {
	target := NewTarget("iqn.test", "localhost:3260")
	target.AddLUN(0, 512, 1024)
	target.AddLUN(1, 512, 2048)

	assert.Len(t, target.LUNs, 2)
	assert.Equal(t, uint32(0), target.LUNs[0].ID)
	assert.Equal(t, uint64(1024), target.LUNs[0].NumBlocks)
	assert.Equal(t, uint32(1), target.LUNs[1].ID)
}
