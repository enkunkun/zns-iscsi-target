package scsi

import (
	"log"
)

// Handler is the SCSI command dispatcher. It routes incoming CDBs to the
// appropriate handler function and returns the response data, status, and
// sense data.
type Handler struct {
	dev          BlockDevice
	serialNumber string
	devID        [16]byte
}

// NewHandler creates a new SCSI handler backed by the given BlockDevice.
// serialNumber is used for VPD page 0x80; devID is the 16-byte NAA identifier
// used for VPD page 0x83.
func NewHandler(dev BlockDevice, serialNumber string, devID [16]byte) *Handler {
	return &Handler{
		dev:          dev,
		serialNumber: serialNumber,
		devID:        devID,
	}
}

// Execute dispatches a SCSI command CDB and optional write data.
// Returns:
//   - dataIn: data to send back to the initiator (for reads and inquiry)
//   - status: SCSI status byte
//   - senseData: sense data (non-nil on CHECK CONDITION)
//   - err: fatal error (usually nil; sense data is the normal error path)
func (h *Handler) Execute(cdb []byte, dataOut []byte) (dataIn []byte, status byte, senseData []byte, err error) {
	if len(cdb) == 0 {
		return nil, StatusCheckCondition, SenseIllegalRequest, nil
	}

	opcode := cdb[0]

	switch opcode {
	case OpcodeTestUnitReady:
		data, st, sense := handleTestUnitReady()
		return data, st, sense, nil

	case OpcodeInquiry:
		data, e := handleInquiry(cdb, h.serialNumber, h.devID)
		if e != nil {
			return nil, StatusCheckCondition, SenseMediumError, nil
		}
		if data == nil {
			// Unsupported VPD page
			return nil, StatusCheckCondition, SenseIllegalRequestInvalidField, nil
		}
		return data, StatusGood, nil, nil

	case OpcodeRequestSense:
		return handleRequestSense(), StatusGood, nil, nil

	case OpcodeStartStopUnit:
		data, st, sense := handleStartStopUnit()
		return data, st, sense, nil

	case OpcodeReadCapacity10:
		data := handleReadCapacity10(h.dev.Capacity(), h.dev.BlockSize())
		return data, StatusGood, nil, nil

	case OpcodeReadCapacity16:
		// Opcode 0x9E is SERVICE ACTION IN; service action must be 0x10
		if len(cdb) < 2 || (cdb[1]&0x1F) != ServiceActionReadCap16 {
			return nil, StatusCheckCondition, SenseIllegalRequest, nil
		}
		data := handleReadCapacity16(h.dev.Capacity(), h.dev.BlockSize())
		return data, StatusGood, nil, nil

	case OpcodeRead6:
		data, e := handleRead6(cdb, h.dev)
		if e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return data, StatusGood, nil, nil

	case OpcodeRead10:
		data, e := handleRead10(cdb, h.dev)
		if e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return data, StatusGood, nil, nil

	case OpcodeRead16:
		data, e := handleRead16(cdb, h.dev)
		if e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return data, StatusGood, nil, nil

	case OpcodeWrite6:
		if e := handleWrite6(cdb, dataOut, h.dev); e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return []byte{}, StatusGood, nil, nil

	case OpcodeWrite10:
		if e := handleWrite10(cdb, dataOut, h.dev); e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return []byte{}, StatusGood, nil, nil

	case OpcodeWrite16:
		if e := handleWrite16(cdb, dataOut, h.dev); e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return []byte{}, StatusGood, nil, nil

	case OpcodeSyncCache10:
		if e := handleSyncCache10(cdb, h.dev); e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return []byte{}, StatusGood, nil, nil

	case OpcodeSyncCache16:
		if e := handleSyncCache16(cdb, h.dev); e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return []byte{}, StatusGood, nil, nil

	case OpcodeReportLUNs:
		return handleReportLUNs(), StatusGood, nil, nil

	case OpcodeModeSense10:
		return handleModeSense10(), StatusGood, nil, nil

	case OpcodeModeSense6:
		return handleModeSense6(), StatusGood, nil, nil

	case OpcodePersistentReserveIn:
		data, st, sense := handlePersistentReserveIn()
		return data, st, sense, nil

	case OpcodePersistentReserveOut:
		data, st, sense := handlePersistentReserveOut()
		return data, st, sense, nil

	case OpcodeUnmap:
		if e := handleUnmap(cdb, dataOut, h.dev); e != nil {
			return nil, StatusCheckCondition, buildSenseFromError(e), nil
		}
		return []byte{}, StatusGood, nil, nil

	default:
		// Unknown/unsupported opcode: return CHECK CONDITION + ILLEGAL REQUEST
		sense := BuildSense(SenseKeyIllegalRequest, ASCInvalidCommandOpCode, 0x00)
		return nil, StatusCheckCondition, sense, nil
	}
}

// buildSenseFromError converts a generic Go error into sense data.
// Uses MEDIUM ERROR as the default sense key for I/O errors.
func buildSenseFromError(err error) []byte {
	log.Printf("SCSI error: %v", err)
	return BuildSense(SenseKeyMediumError, ASCUnrecoveredReadError, 0x00)
}
