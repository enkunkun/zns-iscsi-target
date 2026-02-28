package iscsi

import (
	"encoding/binary"
	"net"
)

// sendDataIn sends read data back to the initiator as one or more Data-In PDUs.
// If data fits in one PDU (data <= maxRecvLen), sends a single Data-In with
// the F (Final) bit and optional SCSI status embedded.
// Otherwise, splits into multiple PDUs.
func sendDataIn(conn net.Conn, itt uint32, lun uint64, data []byte, statSN, expCmdSN, maxCmdSN uint32, maxRecvLen int) error {
	if maxRecvLen <= 0 {
		maxRecvLen = DefaultMaxRecvDataSegmentLength
	}

	totalLen := len(data)
	offset := 0
	dataSN := uint32(0)

	for offset < totalLen || totalLen == 0 {
		end := offset + maxRecvLen
		if end > totalLen {
			end = totalLen
		}

		chunk := data[offset:end]
		isFinal := end == totalLen

		pdu := &PDU{}
		pdu.SetOpcode(OpcodeDataIn)

		// Flags: F bit (0x80) if final; A bit (0x40) for status
		flags := byte(0)
		if isFinal {
			flags |= 0x80 // F bit: final Data-In PDU
		}
		pdu.SetFlags(flags)

		pdu.SetLUN(lun)
		pdu.SetInitiatorTaskTag(itt)

		// TargetTransferTag = 0xFFFFFFFF (not used for Data-In)
		binary.BigEndian.PutUint32(pdu.BHS[20:24], 0xFFFFFFFF)

		// StatSN, ExpCmdSN, MaxCmdSN
		pdu.SetStatSN(statSN)
		pdu.SetExpCmdSN(expCmdSN)
		pdu.SetMaxCmdSN(maxCmdSN)

		// DataSN (bytes 36-39)
		binary.BigEndian.PutUint32(pdu.BHS[36:40], dataSN)

		// Buffer offset (bytes 40-43)
		binary.BigEndian.PutUint32(pdu.BHS[40:44], uint32(offset))

		// Residual count (bytes 44-47)
		binary.BigEndian.PutUint32(pdu.BHS[44:48], 0)

		pdu.DataSegment = chunk

		if err := WritePDU(conn, pdu); err != nil {
			return err
		}

		offset = end
		dataSN++

		if totalLen == 0 {
			break // handle zero-length case
		}
	}

	return nil
}
