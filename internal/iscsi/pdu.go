// Package iscsi implements the iSCSI protocol (RFC 7143) for the ZNS target.
package iscsi

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// iSCSI PDU opcode constants (RFC 7143 Section 10).
const (
	// Initiator opcodes
	OpcodeNopOut    byte = 0x00
	OpcodeSCSICmd   byte = 0x01
	OpcodeTMFReq    byte = 0x02
	OpcodeLoginReq  byte = 0x03
	OpcodeTextReq   byte = 0x04
	OpcodeDataOut   byte = 0x05
	OpcodeLogoutReq byte = 0x06

	// Target opcodes
	OpcodeNopIn     byte = 0x20
	OpcodeSCSIRsp   byte = 0x21
	OpcodeTMFRsp    byte = 0x22
	OpcodeLoginRsp  byte = 0x23
	OpcodeTextRsp   byte = 0x24
	OpcodeDataIn    byte = 0x25
	OpcodeLogoutRsp byte = 0x26
	OpcodeR2T       byte = 0x31
	OpcodeReject    byte = 0x3F
)

// BHSSize is the fixed size of the Basic Header Segment in bytes.
const BHSSize = 48

// PDU represents a complete iSCSI PDU: BHS + AHS + DataSegment.
type PDU struct {
	// BHS bytes (48 bytes)
	BHS [BHSSize]byte

	// AHS is the Additional Header Segment (variable length, may be nil)
	AHS []byte

	// DataSegment is the data payload (variable length, may be nil)
	DataSegment []byte
}

// Opcode returns the PDU opcode (lower 6 bits of byte 0 of BHS).
func (p *PDU) Opcode() byte {
	return p.BHS[0] & 0x3F
}

// SetOpcode sets the opcode in byte 0 of BHS (preserving the I-bit in bit 6).
func (p *PDU) SetOpcode(opcode byte) {
	p.BHS[0] = (p.BHS[0] & 0xC0) | (opcode & 0x3F)
}

// Flags returns byte 1 of BHS.
func (p *PDU) Flags() byte {
	return p.BHS[1]
}

// SetFlags sets byte 1 of BHS.
func (p *PDU) SetFlags(flags byte) {
	p.BHS[1] = flags
}

// TotalAHSLength returns the total AHS length in 4-byte units (BHS byte 4).
func (p *PDU) TotalAHSLength() byte {
	return p.BHS[4]
}

// DataSegmentLength returns the 3-byte big-endian data segment length from BHS bytes 5-7.
func (p *PDU) DataSegmentLength() uint32 {
	return uint32(p.BHS[5])<<16 | uint32(p.BHS[6])<<8 | uint32(p.BHS[7])
}

// SetDataSegmentLength sets the 3-byte data segment length in BHS bytes 5-7.
func (p *PDU) SetDataSegmentLength(n uint32) {
	p.BHS[5] = byte(n >> 16)
	p.BHS[6] = byte(n >> 8)
	p.BHS[7] = byte(n)
}

// LUN returns the 8-byte LUN field from BHS bytes 8-15.
func (p *PDU) LUN() uint64 {
	return binary.BigEndian.Uint64(p.BHS[8:16])
}

// SetLUN sets the 8-byte LUN in BHS bytes 8-15.
func (p *PDU) SetLUN(lun uint64) {
	binary.BigEndian.PutUint64(p.BHS[8:16], lun)
}

// InitiatorTaskTag returns the 4-byte ITT from BHS bytes 16-19.
func (p *PDU) InitiatorTaskTag() uint32 {
	return binary.BigEndian.Uint32(p.BHS[16:20])
}

// SetInitiatorTaskTag sets the 4-byte ITT in BHS bytes 16-19.
func (p *PDU) SetInitiatorTaskTag(itt uint32) {
	binary.BigEndian.PutUint32(p.BHS[16:20], itt)
}

// CmdSN returns the 4-byte CmdSN from BHS bytes 24-27 (for command PDUs).
func (p *PDU) CmdSN() uint32 {
	return binary.BigEndian.Uint32(p.BHS[24:28])
}

// SetCmdSN sets the 4-byte CmdSN in BHS bytes 24-28.
func (p *PDU) SetCmdSN(sn uint32) {
	binary.BigEndian.PutUint32(p.BHS[24:28], sn)
}

// StatSN returns the 4-byte StatSN from BHS bytes 24-27 (for response PDUs).
func (p *PDU) StatSN() uint32 {
	return binary.BigEndian.Uint32(p.BHS[24:28])
}

// SetStatSN sets the 4-byte StatSN in BHS bytes 24-28.
func (p *PDU) SetStatSN(sn uint32) {
	binary.BigEndian.PutUint32(p.BHS[24:28], sn)
}

// ExpStatSN returns the 4-byte ExpStatSN from BHS bytes 28-31 (initiator acks target StatSN).
func (p *PDU) ExpStatSN() uint32 {
	return binary.BigEndian.Uint32(p.BHS[28:32])
}

// SetExpStatSN sets the 4-byte ExpStatSN in BHS bytes 28-32.
func (p *PDU) SetExpStatSN(sn uint32) {
	binary.BigEndian.PutUint32(p.BHS[28:32], sn)
}

// ExpCmdSN returns the 4-byte ExpCmdSN from BHS bytes 28-31 (for response PDUs).
func (p *PDU) ExpCmdSN() uint32 {
	return binary.BigEndian.Uint32(p.BHS[28:32])
}

// SetExpCmdSN sets the ExpCmdSN in BHS bytes 28-32.
func (p *PDU) SetExpCmdSN(sn uint32) {
	binary.BigEndian.PutUint32(p.BHS[28:32], sn)
}

// MaxCmdSN returns the 4-byte MaxCmdSN from BHS bytes 32-35 (for response PDUs).
func (p *PDU) MaxCmdSN() uint32 {
	return binary.BigEndian.Uint32(p.BHS[32:36])
}

// SetMaxCmdSN sets the MaxCmdSN in BHS bytes 32-36.
func (p *PDU) SetMaxCmdSN(sn uint32) {
	binary.BigEndian.PutUint32(p.BHS[32:36], sn)
}

// ReadPDU reads a complete iSCSI PDU from conn.
// It reads the 48-byte BHS, then the AHS (if any), then the DataSegment.
// DataSegment is padded to a 4-byte boundary on the wire.
func ReadPDU(conn net.Conn) (*PDU, error) {
	pdu := &PDU{}

	// Read BHS (48 bytes)
	if _, err := io.ReadFull(conn, pdu.BHS[:]); err != nil {
		return nil, fmt.Errorf("reading BHS: %w", err)
	}

	// Read AHS if present (TotalAHSLength in 4-byte units, byte 4 of BHS)
	ahsLen := int(pdu.BHS[4]) * 4
	if ahsLen > 0 {
		pdu.AHS = make([]byte, ahsLen)
		if _, err := io.ReadFull(conn, pdu.AHS); err != nil {
			return nil, fmt.Errorf("reading AHS: %w", err)
		}
	}

	// Read DataSegment if present
	dsLen := pdu.DataSegmentLength()
	if dsLen > 0 {
		// Padded to 4-byte boundary
		paddedLen := (dsLen + 3) & ^uint32(3)
		buf := make([]byte, paddedLen)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, fmt.Errorf("reading DataSegment: %w", err)
		}
		pdu.DataSegment = buf[:dsLen] // trim padding
	}

	return pdu, nil
}

// WritePDU writes a complete iSCSI PDU to conn.
// The DataSegment length field in BHS is automatically set.
// DataSegment is padded to a 4-byte boundary on the wire.
func WritePDU(conn net.Conn, pdu *PDU) error {
	dsLen := uint32(len(pdu.DataSegment))
	pdu.SetDataSegmentLength(dsLen)

	// Write BHS
	if _, err := conn.Write(pdu.BHS[:]); err != nil {
		return fmt.Errorf("writing BHS: %w", err)
	}

	// Write AHS if present
	if len(pdu.AHS) > 0 {
		if _, err := conn.Write(pdu.AHS); err != nil {
			return fmt.Errorf("writing AHS: %w", err)
		}
	}

	// Write DataSegment with padding
	if dsLen > 0 {
		if _, err := conn.Write(pdu.DataSegment); err != nil {
			return fmt.Errorf("writing DataSegment: %w", err)
		}
		// Pad to 4-byte boundary
		padBytes := (4 - dsLen%4) % 4
		if padBytes > 0 {
			pad := make([]byte, padBytes)
			if _, err := conn.Write(pad); err != nil {
				return fmt.Errorf("writing DataSegment padding: %w", err)
			}
		}
	}

	return nil
}
