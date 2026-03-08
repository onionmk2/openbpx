package edit

import (
	"encoding/binary"
	"fmt"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func patchAssetRegistryDependencyOffset(raw, out []byte, asset *uasset.Asset, translate func(int64) int64, fieldOverwritten bool) error {
	if asset == nil || len(raw) == 0 {
		return nil
	}
	if fieldOverwritten {
		return nil
	}

	fieldPos := int64(asset.Summary.AssetRegistryDataOffset)
	if fieldPos <= 0 {
		return nil
	}
	if fieldPos+8 > int64(len(raw)) {
		return fmt.Errorf("asset registry dependency offset field out of bounds: %d", fieldPos)
	}

	var order binary.ByteOrder = binary.LittleEndian
	if asset.Summary.UsesByteSwappedSerialization() {
		order = binary.BigEndian
	}

	oldDepOffset, err := readInt64At(raw, int(fieldPos), order)
	if err != nil {
		return fmt.Errorf("read asset registry dependency offset: %w", err)
	}
	if oldDepOffset <= 0 {
		return nil
	}

	writePos := translate(fieldPos)
	if writePos < 0 || writePos+8 > int64(len(out)) {
		return fmt.Errorf("asset registry dependency offset write out of bounds: %d", writePos)
	}
	newDepOffset := translate(oldDepOffset)
	if err := writeInt64At(out, int(writePos), newDepOffset, order); err != nil {
		return fmt.Errorf("patch asset registry dependency offset: %w", err)
	}
	return nil
}
