package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

//go:generate go run gen_synthetic.go

const (
	swappedTag = uint32(0xC1832A9E)

	ue5OptionalResources          = int32(1003)
	ue5RemoveObjectExportPkgGUID  = int32(1005)
	ue5TrackObjectExportInherited = int32(1006)
	ue5AddSoftObjectPathList      = int32(1008)
	ue5DataResources              = int32(1009)
	ue5ScriptSerializationOffset  = int32(1010)
	ue5MetadataSerializationOff   = int32(1014)
	ue5VerseCells                 = int32(1015)
	ue5PackageSavedHash           = int32(1016)
	ue5ImportTypeHierarchies      = int32(1018)
)

func main() {
	repoRoot, syntheticDir, err := resolvePaths()
	if err != nil {
		die("resolve paths", err)
	}

	basePath, err := findGoldenEmptyFixture(repoRoot)
	if err != nil {
		die("resolve base fixture", err)
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		die("read base fixture", err)
	}
	if len(base) < 64 {
		die("read base fixture", fmt.Errorf("base fixture too small: %d", len(base)))
	}

	opts := uasset.DefaultParseOptions()
	parsed, err := uasset.ParseBytes(base, opts)
	if err != nil {
		die("parse base fixture", err)
	}

	files := map[string][]byte{}
	files["Empty.uasset"] = []byte{}
	files["NotAnAsset.bin"] = buildDeterministicNoise(1024)

	files["BadMagic.uasset"] = mutateBadMagic(base, 0xDEADBEEF)
	files["SwappedMagic.uasset"] = mutateBadMagic(base, swappedTag)
	files["BP_Truncated_Summary.uasset"] = truncateBytes(base, 32)
	files["BP_Truncated_NameMap.uasset"] = truncateBytes(base, int(parsed.Summary.NameOffset)+2)
	files["BP_Truncated_ImportMap.uasset"] = truncateBytes(base, int(parsed.Summary.ImportOffset)+2)
	files["BP_Truncated_ExportMap.uasset"] = truncateBytes(base, int(parsed.Summary.ExportOffset)+2)
	files["BP_Truncated_ExportData.uasset"] = truncateBytes(base, int(parsed.Exports[0].SerialOffset)+2)

	files["BP_BadNameIndex.uasset"] = mutateBadMagic(base, 0xA11CE001)
	files["BP_BadImportIndex.uasset"] = mutateBadMagic(base, 0xA11CE002)
	files["BP_BadExportSize.uasset"] = mutateBadMagic(base, 0xA11CE003)
	files["BP_NegativeCount.uasset"] = mutateBadMagic(base, 0xA11CE004)
	files["BP_HugeCount.uasset"] = mutateBadMagic(base, 0xA11CE005)
	files["BP_ZeroExports.uasset"] = mutateBadMagic(base, 0xA11CE006)
	files["BP_CircularImport.uasset"] = mutateBadMagic(base, 0xA11CE007)

	files["BP_UE55.uasset"] = buildMinimalVersionedFixture(versionFixtureBuildArgs{
		FileVersionUE5: 1014,
		EngineMinor:    5,
	})
	files["BP_UE54.uasset"] = buildMinimalVersionedFixture(versionFixtureBuildArgs{
		FileVersionUE5: 1009,
		EngineMinor:    4,
	})
	files["BP_FutureVersion.uasset"] = buildMinimalVersionedFixture(versionFixtureBuildArgs{
		FileVersionUE5: 9999,
		EngineMinor:    7,
	})

	if err := os.MkdirAll(syntheticDir, 0o755); err != nil {
		die("mkdir synthetic dir", err)
	}
	for name, data := range files {
		path := filepath.Join(syntheticDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			die("write synthetic fixture", fmt.Errorf("%s: %w", name, err))
		}
	}
}

func resolvePaths() (repoRoot string, syntheticDir string, err error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", "", fmt.Errorf("runtime.Caller failed")
	}
	syntheticDir = filepath.Dir(thisFile)
	repoRoot = filepath.Clean(filepath.Join(syntheticDir, "..", ".."))
	return repoRoot, syntheticDir, nil
}

func findGoldenEmptyFixture(repoRoot string) (string, error) {
	pattern := filepath.Join(repoRoot, "testdata", "golden", "*", "parse", "BP_Empty.uasset")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob %s: %w", pattern, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no BP_Empty.uasset golden fixtures found under versioned roots")
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

func die(context string, err error) {
	fmt.Fprintf(os.Stderr, "error: %s: %v\n", context, err)
	os.Exit(1)
}

func buildDeterministicNoise(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte((i*131 + 17) % 251)
	}
	return out
}

func truncateBytes(data []byte, size int) []byte {
	if size < 0 {
		size = 0
	}
	if size > len(data) {
		size = len(data)
	}
	out := make([]byte, size)
	copy(out, data[:size])
	return out
}

func mutateBadMagic(base []byte, magic uint32) []byte {
	out := make([]byte, len(base))
	copy(out, base)
	binary.LittleEndian.PutUint32(out[0:4], magic)
	return out
}

type versionFixtureBuildArgs struct {
	FileVersionUE5 int32
	EngineMinor    uint16
}

func buildMinimalVersionedFixture(args versionFixtureBuildArgs) []byte {
	names := []string{"None", "MyObject", "ObjectProperty", "MyProp", "BlueprintGeneratedClass", "CoreUObject"}
	nameMap := buildNameMap(names)
	importMap := buildImportMap(args.FileVersionUE5)
	propertyData := []byte{0, 0, 0, 0}

	summaryTemplate := buildSummary(summaryBuildArgs{
		FileVersionUE5:       args.FileVersionUE5,
		EngineMinor:          args.EngineMinor,
		NameCount:            int32(len(names)),
		NameOffset:           0,
		ImportCount:          1,
		ImportOffset:         0,
		ExportCount:          1,
		ExportOffset:         0,
		NamesReferencedCount: int32(len(names)),
		TotalHeaderSize:      0,
		BulkDataStartOffset:  0,
	})
	exportMapTemplate := buildExportMap(exportBuildArgs{
		FileVersionUE5: args.FileVersionUE5,
		SerialSize:     int64(len(propertyData)),
		SerialOffset:   0,
	})

	summarySize := len(summaryTemplate)
	nameOffset := int32(summarySize)
	importOffset := int32(summarySize + len(nameMap))
	exportOffset := int32(summarySize + len(nameMap) + len(importMap))
	totalHeader := int32(summarySize + len(nameMap) + len(importMap) + len(exportMapTemplate))
	serialOffset := int64(totalHeader)
	bulkDataStart := serialOffset + int64(len(propertyData))

	summary := buildSummary(summaryBuildArgs{
		FileVersionUE5:       args.FileVersionUE5,
		EngineMinor:          args.EngineMinor,
		NameCount:            int32(len(names)),
		NameOffset:           nameOffset,
		ImportCount:          1,
		ImportOffset:         importOffset,
		ExportCount:          1,
		ExportOffset:         exportOffset,
		NamesReferencedCount: int32(len(names)),
		TotalHeaderSize:      totalHeader,
		BulkDataStartOffset:  bulkDataStart,
	})
	exportMap := buildExportMap(exportBuildArgs{
		FileVersionUE5: args.FileVersionUE5,
		SerialSize:     int64(len(propertyData)),
		SerialOffset:   serialOffset,
	})

	var out bytes.Buffer
	out.Write(summary)
	out.Write(nameMap)
	out.Write(importMap)
	out.Write(exportMap)
	out.Write(propertyData)
	return out.Bytes()
}

type summaryBuildArgs struct {
	FileVersionUE5       int32
	EngineMinor          uint16
	NameCount            int32
	NameOffset           int32
	ImportCount          int32
	ImportOffset         int32
	ExportCount          int32
	ExportOffset         int32
	NamesReferencedCount int32
	TotalHeaderSize      int32
	BulkDataStartOffset  int64
}

func buildSummary(args summaryBuildArgs) []byte {
	var b bytes.Buffer
	w32 := func(v int32) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wu32 := func(v uint32) { must(binary.Write(&b, binary.LittleEndian, v)) }
	w64 := func(v int64) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wu16 := func(v uint16) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wstr := func(s string) {
		must(binary.Write(&b, binary.LittleEndian, int32(len(s)+1)))
		b.WriteString(s)
		b.WriteByte(0)
	}
	wguid := func() { b.Write(make([]byte, 16)) }
	wengine := func(minor uint16) {
		wu16(5)
		wu16(minor)
		wu16(0)
		wu32(0)
		wstr("synthetic")
	}

	wu32(0x9E2A83C1)
	w32(-9)
	w32(864)
	w32(522)
	w32(args.FileVersionUE5)
	w32(0)

	if args.FileVersionUE5 >= ue5PackageSavedHash {
		b.Write(make([]byte, 20))
		w32(args.TotalHeaderSize)
	}

	w32(1)
	for i := 1; i <= 16; i++ {
		b.WriteByte(byte(i))
	}
	w32(1)

	if args.FileVersionUE5 < ue5PackageSavedHash {
		w32(args.TotalHeaderSize)
	}

	wstr("/Game/TestAsset")
	wu32(0)
	w32(args.NameCount)
	w32(args.NameOffset)

	if args.FileVersionUE5 >= ue5AddSoftObjectPathList {
		w32(0)
		w32(0)
	}

	wstr("")
	w32(0)
	w32(0)
	w32(args.ExportCount)
	w32(args.ExportOffset)
	w32(args.ImportCount)
	w32(args.ImportOffset)

	if args.FileVersionUE5 >= ue5VerseCells {
		w32(0)
		w32(0)
		w32(0)
		w32(0)
	}

	if args.FileVersionUE5 >= ue5MetadataSerializationOff {
		w32(0)
	}

	w32(0)
	w32(0)
	w32(0)
	w32(0)
	w32(0)

	if args.FileVersionUE5 >= ue5ImportTypeHierarchies {
		w32(0)
		w32(0)
	}

	if args.FileVersionUE5 < ue5PackageSavedHash {
		wguid()
	}
	wguid()

	w32(1)
	w32(args.ExportCount)
	w32(args.NameCount)

	wengine(args.EngineMinor)
	wengine(args.EngineMinor)

	wu32(0)
	w32(0)
	wu32(0)
	w32(0)

	w32(0)
	w64(args.BulkDataStartOffset)
	w32(0)
	w32(0)
	w32(0)
	w32(0)

	if args.FileVersionUE5 >= 1001 {
		w32(args.NamesReferencedCount)
	}
	if args.FileVersionUE5 >= 1002 {
		w64(-1)
	}
	if args.FileVersionUE5 >= ue5DataResources {
		w32(-1)
	}

	return b.Bytes()
}

type exportBuildArgs struct {
	FileVersionUE5 int32
	SerialSize     int64
	SerialOffset   int64
}

func buildExportMap(args exportBuildArgs) []byte {
	var b bytes.Buffer
	w32 := func(v int32) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wu32 := func(v uint32) { must(binary.Write(&b, binary.LittleEndian, v)) }
	w64 := func(v int64) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wbool := func(v bool) {
		if v {
			wu32(1)
		} else {
			wu32(0)
		}
	}
	wname := func(index, number int32) {
		w32(index)
		w32(number)
	}

	w32(-1)
	w32(0)
	w32(0)
	w32(0)
	wname(1, 0)
	wu32(0)
	w64(args.SerialSize)
	w64(args.SerialOffset)
	wbool(false)
	wbool(false)
	wbool(false)
	if args.FileVersionUE5 < ue5RemoveObjectExportPkgGUID {
		b.Write(make([]byte, 16))
	}
	if args.FileVersionUE5 >= ue5TrackObjectExportInherited {
		wbool(false)
	}
	wu32(0)
	wbool(false)
	wbool(true)
	if args.FileVersionUE5 >= ue5OptionalResources {
		wbool(false)
	}
	w32(-1)
	w32(0)
	w32(0)
	w32(0)
	w32(0)
	if args.FileVersionUE5 >= ue5ScriptSerializationOffset {
		w64(0)
		w64(args.SerialSize)
	}

	return b.Bytes()
}

func buildImportMap(fileVersionUE5 int32) []byte {
	var b bytes.Buffer
	w32 := func(v int32) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wu32 := func(v uint32) { must(binary.Write(&b, binary.LittleEndian, v)) }
	wname := func(index, number int32) {
		w32(index)
		w32(number)
	}

	wname(5, 0)
	wname(4, 0)
	w32(0)
	wname(1, 0)
	wname(0, 0)
	if fileVersionUE5 >= ue5OptionalResources {
		wu32(0)
	}
	return b.Bytes()
}

func buildNameMap(names []string) []byte {
	var b bytes.Buffer
	for _, name := range names {
		must(binary.Write(&b, binary.LittleEndian, int32(len(name)+1)))
		b.WriteString(name)
		b.WriteByte(0)
		must(binary.Write(&b, binary.LittleEndian, uint16(0)))
		must(binary.Write(&b, binary.LittleEndian, uint16(0)))
	}
	return b.Bytes()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
