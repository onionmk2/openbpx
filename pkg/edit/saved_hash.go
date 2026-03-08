package edit

import (
	"encoding/binary"
	"fmt"

	"github.com/zeebo/blake3"
)

const savedHashSize = 20

// FinalizePackageBytes updates the UE5 saved hash field when the package format
// includes it. The hash is computed over the header+exports bytes with the
// saved-hash field zeroed, matching UE SavePackage behavior.
func FinalizePackageBytes(data []byte, unversionedFileUE5 int32) error {
	if len(data) == 0 {
		return fmt.Errorf("package bytes are empty")
	}

	savedHashPos, order, hasSavedHash, err := scanSavedHashField(data, unversionedFileUE5)
	if err != nil {
		return err
	}
	if !hasSavedHash {
		return nil
	}
	if savedHashPos < 0 || savedHashPos+savedHashSize > len(data) {
		return fmt.Errorf("saved hash position out of range: %d", savedHashPos)
	}

	hashEnd := int64(len(data))
	summaryFields, err := scanSummaryOffsetFields(data, unversionedFileUE5)
	if err != nil {
		return fmt.Errorf("scan summary offsets: %w", err)
	}
	for _, field := range summaryFields {
		if field.name != "PayloadTOCOffset" || field.size != 8 {
			continue
		}
		payloadTOCOffset, err := readInt64At(data, field.pos, order)
		if err != nil {
			return fmt.Errorf("read summary %s: %w", field.name, err)
		}
		if payloadTOCOffset > 0 {
			if payloadTOCOffset > int64(len(data)) {
				return fmt.Errorf("payload TOC offset out of range: %d", payloadTOCOffset)
			}
			hashEnd = payloadTOCOffset
		}
		break
	}

	zero := [savedHashSize]byte{}
	copy(data[savedHashPos:savedHashPos+savedHashSize], zero[:])
	sum := blake3.Sum256(data[:hashEnd])
	copy(data[savedHashPos:savedHashPos+savedHashSize], sum[:savedHashSize])
	return nil
}

func scanSavedHashField(data []byte, unversionedFileUE5 int32) (pos int, order binary.ByteOrder, hasSavedHash bool, err error) {
	if len(data) < 4 {
		return 0, nil, false, fmt.Errorf("file too small")
	}

	tagLE := binary.LittleEndian.Uint32(data[:4])
	order = binary.LittleEndian
	switch tagLE {
	case packageFileTag:
		order = binary.LittleEndian
	case packageFileTagSwapped:
		order = binary.BigEndian
	default:
		return 0, nil, false, fmt.Errorf("invalid package tag: 0x%x", tagLE)
	}

	r := newByteCodec(data, order)
	if _, err := r.readInt32(); err != nil { // tag
		return 0, nil, false, err
	}
	legacy, err := r.readInt32()
	if err != nil {
		return 0, nil, false, err
	}
	if legacy != -4 {
		if _, err := r.readInt32(); err != nil {
			return 0, nil, false, err
		}
	}
	fileUE4, err := r.readInt32()
	if err != nil {
		return 0, nil, false, err
	}
	fileUE5, err := r.readInt32()
	if err != nil {
		return 0, nil, false, err
	}
	fileLicensee, err := r.readInt32()
	if err != nil {
		return 0, nil, false, err
	}
	if fileUE4 == 0 && fileUE5 == 0 && fileLicensee == 0 {
		fileUE4 = ue4VersionUE56
		if unversionedFileUE5 >= ue5MinimumKnown {
			fileUE5 = unversionedFileUE5
		} else {
			fileUE5 = ue5ImportTypeHierarchies
		}
	}
	if fileUE5 < ue5PackageSavedHash {
		return 0, order, false, nil
	}
	return r.off, order, true, nil
}
