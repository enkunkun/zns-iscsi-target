package iscsi

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/enkunkun/zns-iscsi-target/internal/scsi"
)

// SCSICmdFlags bit positions in BHS byte 1.
const (
	SCSICmdFlagFinal    = 0x80 // F bit: final PDU
	SCSICmdFlagRead     = 0x40 // R bit: read command
	SCSICmdFlagWrite    = 0x20 // W bit: write command
	SCSICmdAttrMask     = 0x07 // ATTR bits 2-0
)

// handleSCSICmd processes a SCSI Command PDU.
// For reads: executes the SCSI command and sends Data-In + SCSI Response.
// For writes: sends R2T (or uses ImmediateData), waits for Data-Out, then executes.
func handleSCSICmd(
	conn net.Conn,
	req *PDU,
	scsiHandler *scsi.Handler,
	reassembly *ReassemblyMap,
	params Params,
	statSN, expCmdSN, maxCmdSN uint32,
) error {
	flags := req.BHS[1]
	isRead := (flags & SCSICmdFlagRead) != 0
	isWrite := (flags & SCSICmdFlagWrite) != 0
	finalBit := (flags & SCSICmdFlagFinal) != 0

	// Extract CDB (16 bytes at BHS bytes 32-47)
	cdb := make([]byte, 16)
	copy(cdb, req.BHS[32:48])

	// ITT and ExpectedDataTransferLength
	itt := req.InitiatorTaskTag()
	expectedLen := binary.BigEndian.Uint32(req.BHS[20:24])
	lun := req.LUN()

	var writeData []byte

	if isWrite {
		// Collect write data
		var immediateData []byte
		if len(req.DataSegment) > 0 {
			immediateData = req.DataSegment
		}

		if finalBit || !params.InitialR2T {
			// Immediate data only (no R2T), or all data already present
			writeData = immediateData

			if !finalBit && expectedLen > uint32(len(immediateData)) {
				// Need to receive more Data-Out PDUs
				reassembly.initBuffer(itt, expectedLen)
				// Pre-fill with immediate data
				if len(immediateData) > 0 {
					fakePDU := &PDU{}
					fakePDU.BHS[1] = 0x00 // not final
					binary.BigEndian.PutUint32(fakePDU.BHS[36:40], 0) // DataSN=0
					binary.BigEndian.PutUint32(fakePDU.BHS[40:44], 0) // offset=0
					fakePDU.SetInitiatorTaskTag(itt)
					fakePDU.DataSegment = immediateData
					_, _, _ = reassembly.addDataOut(fakePDU)
				}
				// Send R2T for remaining data
				return sendR2T(conn, req, itt, uint32(len(immediateData)), expectedLen-uint32(len(immediateData)), statSN, expCmdSN, maxCmdSN)
			}
		} else {
			// InitialR2T=Yes: send R2T for all data
			reassembly.initBuffer(itt, expectedLen)
			if len(immediateData) > 0 {
				// Store immediate data in buffer
				fakePDU := &PDU{}
				fakePDU.BHS[1] = 0x00
				binary.BigEndian.PutUint32(fakePDU.BHS[36:40], 0)
				binary.BigEndian.PutUint32(fakePDU.BHS[40:44], 0)
				fakePDU.SetInitiatorTaskTag(itt)
				fakePDU.DataSegment = immediateData
				_, _, _ = reassembly.addDataOut(fakePDU)
				return sendR2T(conn, req, itt, uint32(len(immediateData)), expectedLen-uint32(len(immediateData)), statSN, expCmdSN, maxCmdSN)
			}
			return sendR2T(conn, req, itt, 0, expectedLen, statSN, expCmdSN, maxCmdSN)
		}
	}

	// Execute SCSI command
	dataIn, scsiStatus, senseData, err := scsiHandler.Execute(cdb, writeData)
	if err != nil {
		return fmt.Errorf("SCSI Execute: %w", err)
	}

	// Send response
	if isRead && len(dataIn) > 0 && scsiStatus == scsi.StatusGood {
		// Send Data-In PDUs
		maxRecv := params.MaxRecvDataSegmentLength
		if maxRecv <= 0 {
			maxRecv = DefaultMaxRecvDataSegmentLength
		}
		if err := sendDataIn(conn, itt, lun, dataIn, statSN, expCmdSN, maxCmdSN, maxRecv); err != nil {
			return fmt.Errorf("sending Data-In: %w", err)
		}
	}

	return sendSCSIResponse(conn, req, scsiStatus, senseData, uint32(len(dataIn)), statSN, expCmdSN, maxCmdSN)
}

// sendR2T sends a Ready-to-Transfer PDU.
func sendR2T(conn net.Conn, req *PDU, itt, bufferOffset, desiredLen uint32, statSN, expCmdSN, maxCmdSN uint32) error {
	rsp := &PDU{}
	rsp.BHS[0] = OpcodeR2T
	rsp.BHS[1] = 0x80 // F bit

	// Copy LUN
	copy(rsp.BHS[8:16], req.BHS[8:16])

	rsp.SetInitiatorTaskTag(itt)

	// TargetTransferTag (TTT): unique ID for this R2T
	// For simplicity, use ITT as TTT
	binary.BigEndian.PutUint32(rsp.BHS[20:24], itt)

	rsp.SetStatSN(statSN)
	rsp.SetExpCmdSN(expCmdSN)
	rsp.SetMaxCmdSN(maxCmdSN)

	// R2TSN (bytes 36-39) = 0 for first R2T
	binary.BigEndian.PutUint32(rsp.BHS[36:40], 0)

	// Buffer Offset (bytes 40-43)
	binary.BigEndian.PutUint32(rsp.BHS[40:44], bufferOffset)

	// Desired Data Transfer Length (bytes 44-47)
	binary.BigEndian.PutUint32(rsp.BHS[44:48], desiredLen)

	return WritePDU(conn, rsp)
}

// sendSCSIResponse sends a SCSI Response PDU.
func sendSCSIResponse(
	conn net.Conn,
	req *PDU,
	scsiStatus byte,
	senseData []byte,
	residualCount uint32,
	statSN, expCmdSN, maxCmdSN uint32,
) error {
	rsp := &PDU{}
	rsp.BHS[0] = OpcodeSCSIRsp
	rsp.BHS[1] = 0x80 // F bit always set

	// Response code: 0x00 = Command Completed at Target
	rsp.BHS[2] = 0x00

	// SCSI Status byte
	rsp.BHS[3] = scsiStatus

	// Copy ITT from request
	rsp.SetInitiatorTaskTag(req.InitiatorTaskTag())

	// StatSN, ExpCmdSN, MaxCmdSN
	rsp.SetStatSN(statSN)
	rsp.SetExpCmdSN(expCmdSN)
	rsp.SetMaxCmdSN(maxCmdSN)

	// Sense data, if any, goes in the data segment
	if len(senseData) > 0 && scsiStatus == scsi.StatusCheckCondition {
		// Sense data is prefixed with a 2-byte length field
		senseLen := uint16(len(senseData))
		segData := make([]byte, 2+len(senseData))
		segData[0] = byte(senseLen >> 8)
		segData[1] = byte(senseLen)
		copy(segData[2:], senseData)
		rsp.DataSegment = segData
	}

	return WritePDU(conn, rsp)
}

// completeWriteCommand is called when all Data-Out PDUs have been received.
// It executes the SCSI command with the assembled write data and sends a response.
func completeWriteCommand(
	conn net.Conn,
	req *PDU,
	cdb []byte,
	writeData []byte,
	scsiHandler *scsi.Handler,
	statSN, expCmdSN, maxCmdSN uint32,
) error {
	_, scsiStatus, senseData, err := scsiHandler.Execute(cdb, writeData)
	if err != nil {
		return fmt.Errorf("SCSI Execute (write): %w", err)
	}
	return sendSCSIResponse(conn, req, scsiStatus, senseData, 0, statSN, expCmdSN, maxCmdSN)
}
