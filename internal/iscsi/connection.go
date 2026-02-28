package iscsi

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/enkunkun/zns-iscsi-target/internal/scsi"
)

// Connection represents a single iSCSI TCP connection.
type Connection struct {
	conn        net.Conn
	session     *Session
	params      Params
	scsiHandler *scsi.Handler
	reassembly  *ReassemblyMap

	// Sequence numbers
	statSN   uint32
	expCmdSN uint32
	maxCmdSN uint32

	// Pending SCSI command state (for Data-Out reassembly)
	pendingCDB   map[uint32][]byte // ITT -> CDB
	pendingReq   map[uint32]*PDU  // ITT -> original SCSI CMD PDU

	mu sync.Mutex

	// Write serialization
	writeMu sync.Mutex

	closed bool
}

// newConnection creates a new Connection instance.
func newConnection(conn net.Conn, session *Session, params Params, handler *scsi.Handler) *Connection {
	return &Connection{
		conn:        conn,
		session:     session,
		params:      params,
		scsiHandler: handler,
		reassembly:  newReassemblyMap(),
		pendingCDB:  make(map[uint32][]byte),
		pendingReq:  make(map[uint32]*PDU),
		maxCmdSN:    32, // allow 32 outstanding commands
	}
}

// writeConn serializes writes to the connection.
func (c *Connection) writeConn(pdu *PDU) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WritePDU(c.conn, pdu)
}

// nextStatSN returns the next StatSN and increments.
func (c *Connection) nextStatSN() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statSN++
	return c.statSN
}

// Run is the main read loop for this connection.
// It reads PDUs and dispatches them by opcode.
func (c *Connection) Run() {
	defer c.cleanup()

	for {
		pdu, err := ReadPDU(c.conn)
		if err != nil {
			if err != io.EOF && !c.isClosed() {
				log.Printf("connection read error: %v", err)
			}
			return
		}

		if err := c.dispatch(pdu); err != nil {
			if !c.isClosed() {
				log.Printf("dispatch error: %v", err)
			}
			return
		}
	}
}

// dispatch routes an incoming PDU to the appropriate handler.
func (c *Connection) dispatch(pdu *PDU) error {
	opcode := pdu.Opcode()

	// Update expected CmdSN
	if opcode != OpcodeDataOut {
		c.mu.Lock()
		c.expCmdSN = pdu.CmdSN() + 1
		c.maxCmdSN = c.expCmdSN + 31
		c.mu.Unlock()
	}

	c.mu.Lock()
	statSN := c.statSN
	expCmdSN := c.expCmdSN
	maxCmdSN := c.maxCmdSN
	c.mu.Unlock()

	switch opcode {
	case OpcodeSCSICmd:
		return c.handleSCSICommand(pdu, statSN, expCmdSN, maxCmdSN)

	case OpcodeDataOut:
		return c.handleDataOut(pdu, statSN, expCmdSN, maxCmdSN)

	case OpcodeNopOut:
		c.mu.Lock()
		c.statSN++
		newStatSN := c.statSN
		c.mu.Unlock()
		c.writeMu.Lock()
		err := handleNopOut(c.conn, pdu, newStatSN, expCmdSN, maxCmdSN)
		c.writeMu.Unlock()
		return err

	case OpcodeTMFReq:
		c.mu.Lock()
		c.statSN++
		newStatSN := c.statSN
		c.mu.Unlock()
		c.writeMu.Lock()
		err := handleTMFRequest(c.conn, pdu, newStatSN, expCmdSN, maxCmdSN)
		c.writeMu.Unlock()
		return err

	case OpcodeLogoutReq:
		c.mu.Lock()
		c.statSN++
		newStatSN := c.statSN
		c.mu.Unlock()
		c.writeMu.Lock()
		err := handleLogout(c.conn, pdu, newStatSN, expCmdSN, maxCmdSN)
		c.writeMu.Unlock()
		if err != nil {
			return err
		}
		c.close()
		return fmt.Errorf("logout requested")

	case OpcodeTextReq:
		return c.handleTextRequest(pdu, statSN, expCmdSN, maxCmdSN)

	default:
		log.Printf("unsupported PDU opcode: 0x%02X", opcode)
		return nil
	}
}

// handleSCSICommand handles a SCSI Command PDU.
func (c *Connection) handleSCSICommand(pdu *PDU, statSN, expCmdSN, maxCmdSN uint32) error {
	// Extract CDB (BHS bytes 32-47)
	cdb := make([]byte, 16)
	copy(cdb, pdu.BHS[32:48])

	flags := pdu.BHS[1]
	isWrite := (flags & SCSICmdFlagWrite) != 0
	expectedLen := binary.BigEndian.Uint32(pdu.BHS[20:24])
	itt := pdu.InitiatorTaskTag()

	// Check if this is a write that needs Data-Out PDUs
	if isWrite && expectedLen > uint32(len(pdu.DataSegment)) {
		// Store pending command state
		c.mu.Lock()
		c.pendingCDB[itt] = cdb
		c.pendingReq[itt] = pdu
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.statSN++
	newStatSN := c.statSN
	c.mu.Unlock()

	c.writeMu.Lock()
	err := handleSCSICmd(c.conn, pdu, c.scsiHandler, c.reassembly, c.params, newStatSN, expCmdSN, maxCmdSN)
	c.writeMu.Unlock()
	return err
}

// handleDataOut handles a Data-Out PDU for an in-progress write command.
func (c *Connection) handleDataOut(pdu *PDU, statSN, expCmdSN, maxCmdSN uint32) error {
	itt := pdu.InitiatorTaskTag()

	assembled, final, err := c.reassembly.addDataOut(pdu)
	if err != nil {
		log.Printf("Data-Out reassembly error for ITT 0x%08X: %v", itt, err)
		return nil
	}

	if !final {
		return nil
	}

	// Get pending CDB and request PDU
	c.mu.Lock()
	cdb, hasCDB := c.pendingCDB[itt]
	origReq, hasReq := c.pendingReq[itt]
	if hasCDB {
		delete(c.pendingCDB, itt)
	}
	if hasReq {
		delete(c.pendingReq, itt)
	}
	c.statSN++
	newStatSN := c.statSN
	c.mu.Unlock()

	if !hasCDB || !hasReq {
		log.Printf("no pending command for ITT 0x%08X", itt)
		return nil
	}

	c.writeMu.Lock()
	err = completeWriteCommand(c.conn, origReq, cdb, assembled, c.scsiHandler, newStatSN, expCmdSN, maxCmdSN)
	c.writeMu.Unlock()
	return err
}

// handleTextRequest handles a Text Request PDU (used for SendTargets discovery).
func (c *Connection) handleTextRequest(pdu *PDU, statSN, expCmdSN, maxCmdSN uint32) error {
	c.mu.Lock()
	c.statSN++
	newStatSN := c.statSN
	c.mu.Unlock()

	// Parse text request
	kvIn := ParseKeyValuePairs(pdu.DataSegment)
	_ = kvIn // Text requests typically contain "SendTargets=All" or "SendTargets=<iqn>"

	// Build text response with target information
	targetInfo := ""
	if c.session != nil {
		targetInfo = "TargetName=" + c.params.TargetName + "\x00" +
			"TargetAddress=" + c.conn.LocalAddr().String() + ",1\x00"
	}

	rsp := &PDU{}
	rsp.BHS[0] = OpcodeTextRsp
	rsp.BHS[1] = 0x80 // F bit

	rsp.SetInitiatorTaskTag(pdu.InitiatorTaskTag())
	binary.BigEndian.PutUint32(rsp.BHS[20:24], 0xFFFFFFFF) // TTT

	c.writeMu.Lock()
	rsp.SetStatSN(newStatSN)
	rsp.SetExpCmdSN(expCmdSN)
	rsp.SetMaxCmdSN(maxCmdSN)

	if targetInfo != "" {
		rsp.DataSegment = []byte(targetInfo)
	}

	err := WritePDU(c.conn, rsp)
	c.writeMu.Unlock()
	return err
}

// close marks the connection as closed.
func (c *Connection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

// isClosed returns whether the connection has been marked closed.
func (c *Connection) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// cleanup is called when the connection's read loop exits.
func (c *Connection) cleanup() {
	c.conn.Close()
	if c.session != nil {
		c.session.RemoveConnection(c)
	}
}
