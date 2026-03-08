package cli

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func TestTopDirFromRelative(t *testing.T) {
	tests := []struct {
		rel  string
		want string
	}{
		{rel: "foo/bar/BP_A.uasset", want: "foo"},
		{rel: "foo.uasset", want: "."},
		{rel: ".", want: "."},
		{rel: "./root/sub/file.uasset", want: "root"},
	}
	for _, tc := range tests {
		if got := topDirFromRelative(tc.rel); got != tc.want {
			t.Fatalf("topDirFromRelative(%q): got %q want %q", tc.rel, got, tc.want)
		}
	}
}

func TestPackageRoot(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "/Game/Lyra/Blueprint/BP_Test", want: "/Game/Lyra"},
		{input: "/Script/Engine", want: "/Script/Engine"},
		{input: "/Game/Lyra/BP.BP_C", want: "/Game/Lyra"},
		{input: "None", want: "<none>"},
	}
	for _, tc := range tests {
		if got := packageRoot(tc.input); got != tc.want {
			t.Fatalf("packageRoot(%q): got %q want %q", tc.input, got, tc.want)
		}
	}
}

func TestAssetHasAnyClass(t *testing.T) {
	asset := &uasset.Asset{
		Names: []uasset.NameEntry{
			{Value: "Blueprint"},
			{Value: "DataTable"},
		},
		Imports: []uasset.ImportEntry{
			{ObjectName: uasset.NameRef{Index: 0, Number: 0}}, // Blueprint
			{ObjectName: uasset.NameRef{Index: 1, Number: 0}}, // DataTable
		},
		Exports: []uasset.ExportEntry{
			{ClassIndex: uasset.PackageIndex(-1)}, // import 1 => Blueprint
		},
	}

	if !assetHasAnyClass(asset, []string{"blueprint"}) {
		t.Fatalf("expected blueprint class to be found")
	}
	if assetHasAnyClass(asset, []string{"datatable"}) {
		t.Fatalf("did not expect datatable class to be found in exports")
	}
}

func TestBuildDataTableReadResponseRejectsBadExportIndex(t *testing.T) {
	asset := &uasset.Asset{}
	_, err := buildDataTableReadResponse("x.uasset", asset, 1, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "export index out of range") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveImportTargetPathPrefersOuterResolvedPath(t *testing.T) {
	asset := &uasset.Asset{
		Names: []uasset.NameEntry{
			{Value: "/Game/Lyra/Blueprint/BP_Target"},
			{Value: "SomeImport"},
			{Value: "None"},
		},
		Imports: []uasset.ImportEntry{
			{ObjectName: uasset.NameRef{Index: 0, Number: 0}},
		},
	}
	imp := uasset.ImportEntry{
		ObjectName:  uasset.NameRef{Index: 1, Number: 0},
		PackageName: uasset.NameRef{Index: 2, Number: 0},
		OuterIndex:  uasset.PackageIndex(-1),
	}
	got := resolveImportTargetPath(asset, imp)
	if got != "/Game/Lyra/Blueprint/BP_Target" {
		t.Fatalf("resolveImportTargetPath: got %q", got)
	}
}

func TestExtractResolvedObjectPath(t *testing.T) {
	if got := extractResolvedObjectPath("import:311:/Game/Lyra/Path/BP_Asset"); got != "/Game/Lyra/Path/BP_Asset" {
		t.Fatalf("extractResolvedObjectPath: got %q", got)
	}
	if got := extractResolvedObjectPath("bad-format"); got != "" {
		t.Fatalf("expected empty path for malformed resolved index, got %q", got)
	}
}

func TestParseSoftObjectPathEntriesWarnsWhenSummaryListUnsupported(t *testing.T) {
	asset := &uasset.Asset{
		Summary: uasset.PackageSummary{
			FileVersionUE5: 1007,
		},
	}
	entries, warnings := parseSoftObjectPathEntries(asset, []byte{0, 0, 0, 0}, 1)
	if len(entries) != 0 {
		t.Fatalf("expected no parsed entries, got %d", len(entries))
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "soft object path list is unavailable") {
		t.Fatalf("expected summary list unavailable warning, got: %v", warnings)
	}
}

func TestParseSoftObjectPathEntriesSupportsUTF8SubPath(t *testing.T) {
	asset := &uasset.Asset{
		Summary: uasset.PackageSummary{
			FileVersionUE5: 1017,
		},
		Names: []uasset.NameEntry{
			{Value: "None"},
			{Value: "/Game/Test"},
			{Value: "BP_Test"},
		},
	}

	data := make([]byte, 0, 32)
	appendInt32 := func(v int32) {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(v))
		data = append(data, b...)
	}
	appendInt32(1) // package name index
	appendInt32(0)
	appendInt32(2) // asset name index
	appendInt32(0)
	appendInt32(4) // utf8 sub path byte length
	data = append(data, []byte("Root")...)

	entries, warnings := parseSoftObjectPathEntries(asset, data, 1)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if got := len(entries); got != 1 {
		t.Fatalf("entry count: got %d want 1", got)
	}
	if got := entries[0]["subPath"]; got != "Root" {
		t.Fatalf("subPath: got %v", got)
	}
}

func TestNormalizePackageSectionName(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "soft-object-paths", want: "soft-object-paths", ok: true},
		{input: "import-type-hierarchies", want: "import-type-hierarchies", ok: true},
		{input: "softObjectPaths", want: "", ok: false},
		{input: "asset_registry", want: "", ok: false},
		{input: "bulk-data", want: "bulk-data", ok: true},
		{input: "unknown", want: "", ok: false},
	}
	for _, tc := range tests {
		got, ok := normalizePackageSectionName(tc.input)
		if ok != tc.ok {
			t.Fatalf("normalizePackageSectionName(%q): ok=%v want %v", tc.input, ok, tc.ok)
		}
		if got != tc.want {
			t.Fatalf("normalizePackageSectionName(%q): got %q want %q", tc.input, got, tc.want)
		}
	}
}

func TestRunPackageResolveIndexRejectsOutOfInt32Range(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	index := strconv.FormatInt(int64(math.MaxInt32)+1, 10)
	code := runPackageResolveIndex([]string{"/tmp/nonexistent.uasset", "--index", index}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code: got %d want 1", code)
	}
	if !strings.Contains(stderr.String(), "index out of int32 range") {
		t.Fatalf("expected int32 range error, got: %s", stderr.String())
	}
}

func TestBuildReverseDependsMap(t *testing.T) {
	depends := []map[string]any{
		{
			"export":     1,
			"exportName": "A",
			"dependencies": []map[string]any{
				{"index": 2},
				{"index": 3},
				{"index": 3},
			},
		},
		{
			"export":     2,
			"exportName": "B",
			"dependencies": []map[string]any{
				{"index": 3},
			},
		},
		{
			"export":       3,
			"exportName":   "C",
			"dependencies": []map[string]any{},
		},
	}

	reverse := buildReverseDependsMap(depends)
	if len(reverse) != 3 {
		t.Fatalf("reverse entry count: got %d want 3", len(reverse))
	}

	entry := reverse[2]
	if got := entry["export"]; got != 3 {
		t.Fatalf("third reverse export: got %v want 3", got)
	}
	if got := entry["dependentCount"]; got != 2 {
		t.Fatalf("dependentCount: got %v want 2", got)
	}

	dependents, ok := entry["dependents"].([]map[string]any)
	if !ok {
		t.Fatalf("dependents type: got %T", entry["dependents"])
	}
	if len(dependents) != 2 {
		t.Fatalf("dependent length: got %d want 2", len(dependents))
	}
	if got := dependents[0]["export"]; got != 1 {
		t.Fatalf("first dependent export: got %v want 1", got)
	}
	if got := dependents[0]["referenceCount"]; got != 2 {
		t.Fatalf("first dependent referenceCount: got %v want 2", got)
	}
	if got := dependents[1]["export"]; got != 2 {
		t.Fatalf("second dependent export: got %v want 2", got)
	}
	if got := dependents[1]["referenceCount"]; got != 1 {
		t.Fatalf("second dependent referenceCount: got %v want 1", got)
	}
}
