package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func TestFiBDataRoundTripFixture(t *testing.T) {
	t.Parallel()

	paths := []string{
		"../../testdata/golden/ue5.6/operations/ref_rewrite_single/before.uasset",
		"../../testdata/golden/ue5.6/operations/var_rename_simple/before.uasset",
		"../../testdata/golden/ue5.6/operations/localization_rekey/before.uasset",
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			asset, err := uasset.ParseFile(path, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse asset: %v", err)
			}

			section, _, _, err := parseAssetRegistrySection(asset)
			if err != nil {
				t.Fatalf("parse asset registry: %v", err)
			}
			if section == nil {
				t.Fatalf("asset registry section missing")
			}

			found := 0
			for _, obj := range section.Objects {
				for _, tag := range obj.Tags {
					if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) &&
						!strings.EqualFold(tag.Key, assetRegistryTagUnversionedFindInBlueprintsData) {
						continue
					}
					found++
					parsed, err := parseFiBData(asset, tag.Value)
					if err != nil {
						t.Fatalf("parse FiBData %s/%s: %v", obj.ObjectPath, tag.Key, err)
					}
					encoded, err := encodeFiBData(parsed)
					if err != nil {
						t.Fatalf("encode FiBData %s/%s: %v", obj.ObjectPath, tag.Key, err)
					}
					if encoded != tag.Value {
						firstDiff := -1
						limit := len(encoded)
						if len(tag.Value) < limit {
							limit = len(tag.Value)
						}
						for i := 0; i < limit; i++ {
							if encoded[i] != tag.Value[i] {
								firstDiff = i
								break
							}
						}
						if firstDiff < 0 && len(encoded) != len(tag.Value) {
							firstDiff = limit
						}
						entryNotes := make([]string, 0, len(parsed.Lookup))
						for i, entry := range parsed.Lookup {
							entryEncoded, err := encodeFiBLookupText(entry.Text)
							if err != nil {
								entryNotes = append(entryNotes, fmt.Sprintf("#%d:%s encodeErr=%v", i, fibHistoryTypeName(entry.Text.HistoryTypeCode), err))
								continue
							}
							if len(entryEncoded) != len(entry.Text.Raw) || string(entryEncoded) != string(entry.Text.Raw) {
								if len(entryNotes) == 0 {
									entryNotes = append(entryNotes, fmt.Sprintf("firstRaw=%x firstEnc=%x", entry.Text.Raw, entryEncoded))
								}
								entryNotes = append(
									entryNotes,
									fmt.Sprintf(
										"#%d:%s raw=%d enc=%d",
										i,
										fibHistoryTypeName(entry.Text.HistoryTypeCode),
										len(entry.Text.Raw),
										len(entryEncoded),
									),
								)
							}
						}
						t.Fatalf(
							"FiBData round-trip mismatch for %s/%s: len(got)=%d len(want)=%d firstDiff=%d entryDiffs=%v",
							obj.ObjectPath,
							tag.Key,
							len(encoded),
							len(tag.Value),
							firstDiff,
							entryNotes,
						)
					}
				}
			}
			if found == 0 {
				t.Fatalf("no FiBData tags found")
			}
		})
	}
}

func TestSetLookupTextValuePreservesWhitespaceChanges(t *testing.T) {
	t.Parallel()

	editor := newFiBLookupEditor([]fibLookupEntry{
		{
			Key: 22,
			Text: fibLookupText{
				HistoryTypeCode: 0,
				SourceString:    "value",
			},
		},
	}, nil)

	changed, err := editor.setLookupTextValue(22, " value ")
	if err != nil {
		t.Fatalf("setLookupTextValue: %v", err)
	}
	if !changed {
		t.Fatalf("setLookupTextValue reported unchanged for whitespace-sensitive update")
	}
	if got := editor.Lookup[0].Text.SourceString; got != " value " {
		t.Fatalf("source string: got %q want %q", got, " value ")
	}
}

func TestVarRenameWithRefsFiBLeafsMatchFixture(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	workPath := filepath.Join(tmpDir, "work.uasset")
	beforePath := "../../testdata/golden/ue5.6/operations/var_rename_with_refs/before.uasset"
	expectedPath := "../../testdata/golden/ue5.6/operations/var_rename_with_refs/after.uasset"
	data, err := os.ReadFile(beforePath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	if err := os.WriteFile(workPath, data, 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}

	var stdout, stderr strings.Builder
	if code := Run([]string{"var", "rename", workPath, "--from", "UsedVar", "--to", "RenamedVar"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run var rename: code=%d stderr=%s", code, stderr.String())
	}

	actualAsset, err := uasset.ParseFile(workPath, uasset.DefaultParseOptions())
	if err != nil {
		t.Fatalf("parse actual: %v", err)
	}
	expectedAsset, err := uasset.ParseFile(expectedPath, uasset.DefaultParseOptions())
	if err != nil {
		t.Fatalf("parse expected: %v", err)
	}

	actualLeafs := collectFiBLeafSummaries(t, actualAsset)
	expectedLeafs := collectFiBLeafSummaries(t, expectedAsset)
	if !slices.Equal(actualLeafs, expectedLeafs) {
		diffs := make([]string, 0, 8)
		limit := min(len(actualLeafs), len(expectedLeafs))
		for i := 0; i < limit && len(diffs) < 8; i++ {
			if actualLeafs[i] == expectedLeafs[i] {
				continue
			}
			diffs = append(diffs, fmt.Sprintf("%d actual=%q expected=%q", i, actualLeafs[i], expectedLeafs[i]))
		}
		if len(actualLeafs) != len(expectedLeafs) {
			diffs = append(diffs, fmt.Sprintf("len actual=%d expected=%d", len(actualLeafs), len(expectedLeafs)))
		}
		t.Fatalf("FiB leaf mismatches: %v", diffs)
	}
}

func TestVarRenameFixtureFiBRawMatchesFixture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		before   string
		expected string
		args     []string
	}{
		{
			name:     "with_refs",
			before:   "../../testdata/golden/ue5.6/operations/var_rename_with_refs/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/var_rename_with_refs/after.uasset",
			args:     []string{"--from", "UsedVar", "--to", "RenamedVar"},
		},
		{
			name:     "unicode",
			before:   "../../testdata/golden/ue5.6/operations/var_rename_unicode/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/var_rename_unicode/after.uasset",
			args:     []string{"--from", "体力", "--to", "HP"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			workPath := filepath.Join(tmpDir, "work.uasset")
			data, err := os.ReadFile(tc.before)
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			if err := os.WriteFile(workPath, data, 0o644); err != nil {
				t.Fatalf("write work file: %v", err)
			}

			argv := append([]string{"var", "rename", workPath}, tc.args...)
			var stdout, stderr strings.Builder
			if code := Run(argv, &stdout, &stderr); code != 0 {
				t.Fatalf("run var rename: code=%d stderr=%s", code, stderr.String())
			}

			actualAsset, err := uasset.ParseFile(workPath, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse actual: %v", err)
			}
			expectedAsset, err := uasset.ParseFile(tc.expected, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse expected: %v", err)
			}

			actualSection, _, _, err := parseAssetRegistrySection(actualAsset)
			if err != nil {
				t.Fatalf("parse actual asset registry: %v", err)
			}
			expectedSection, _, _, err := parseAssetRegistrySection(expectedAsset)
			if err != nil {
				t.Fatalf("parse expected asset registry: %v", err)
			}
			if actualSection == nil || expectedSection == nil {
				t.Fatalf("asset registry section missing")
			}
			if len(actualSection.Objects) != len(expectedSection.Objects) {
				t.Fatalf("asset registry object count: got %d want %d", len(actualSection.Objects), len(expectedSection.Objects))
			}

			for objIdx := range actualSection.Objects {
				actualObj := actualSection.Objects[objIdx]
				expectedObj := expectedSection.Objects[objIdx]
				if actualObj.ObjectPath != expectedObj.ObjectPath || actualObj.ObjectClass != expectedObj.ObjectClass {
					t.Fatalf("asset registry object[%d] mismatch: actual=%s/%s expected=%s/%s", objIdx, actualObj.ObjectPath, actualObj.ObjectClass, expectedObj.ObjectPath, expectedObj.ObjectClass)
				}
				if len(actualObj.Tags) != len(expectedObj.Tags) {
					t.Fatalf("asset registry object[%d] tag count: got %d want %d", objIdx, len(actualObj.Tags), len(expectedObj.Tags))
				}

				for tagIdx := range actualObj.Tags {
					actualTag := actualObj.Tags[tagIdx]
					expectedTag := expectedObj.Tags[tagIdx]
					if actualTag.Key != expectedTag.Key {
						t.Fatalf("asset registry tag[%d] key mismatch: got %s want %s", tagIdx, actualTag.Key, expectedTag.Key)
					}
					if actualTag.Value == expectedTag.Value {
						continue
					}
					if !strings.EqualFold(actualTag.Key, assetRegistryTagFindInBlueprintsData) &&
						!strings.EqualFold(actualTag.Key, assetRegistryTagUnversionedFindInBlueprintsData) {
						firstDiff := firstStringDiff(actualTag.Value, expectedTag.Value)
						t.Fatalf("asset registry tag[%d] value mismatch at char %d", tagIdx, firstDiff)
					}

					actualFiB, err := parseFiBData(actualAsset, actualTag.Value)
					if err != nil {
						t.Fatalf("parse actual FiB tag[%d]: %v", tagIdx, err)
					}
					expectedFiB, err := parseFiBData(expectedAsset, expectedTag.Value)
					if err != nil {
						t.Fatalf("parse expected FiB tag[%d]: %v", tagIdx, err)
					}
					if len(actualFiB.Lookup) != len(expectedFiB.Lookup) {
						t.Fatalf("FiB lookup count tag[%d]: got %d want %d", tagIdx, len(actualFiB.Lookup), len(expectedFiB.Lookup))
					}

					for entryIdx := range actualFiB.Lookup {
						actualEntry := actualFiB.Lookup[entryIdx]
						expectedEntry := expectedFiB.Lookup[entryIdx]
						if actualEntry.Key != expectedEntry.Key {
							t.Fatalf("FiB lookup key[%d]: got %d want %d", entryIdx, actualEntry.Key, expectedEntry.Key)
						}
						if bytes.Equal(actualEntry.Text.Raw, expectedEntry.Text.Raw) {
							continue
						}
						firstDiff := firstByteDiff(actualEntry.Text.Raw, expectedEntry.Text.Raw)
						t.Fatalf(
							"FiB raw mismatch tag=%s entry=%d type=%s actualLen=%d expectedLen=%d firstDiff=%d actual=%x expected=%x",
							actualTag.Key,
							entryIdx,
							fibHistoryTypeName(actualEntry.Text.HistoryTypeCode),
							len(actualEntry.Text.Raw),
							len(expectedEntry.Text.Raw),
							firstDiff,
							actualEntry.Text.Raw[max(0, firstDiff-12):min(len(actualEntry.Text.Raw), firstDiff+24)],
							expectedEntry.Text.Raw[max(0, firstDiff-12):min(len(expectedEntry.Text.Raw), firstDiff+24)],
						)
					}
				}
			}
		})
	}
}

func TestOperationFixtureFiBLeafsMatchFixture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		before   string
		expected string
		argv     []string
	}{
		{
			name:     "var_set_default_vector",
			before:   "../../testdata/golden/ue5.6/operations/var_set_default_vector/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/var_set_default_vector/after.uasset",
			argv:     []string{"var", "set-default", "--name", "VVector", "--value", `{"x":1,"y":2,"z":3}`},
		},
		{
			name:     "prop_set_color",
			before:   "../../testdata/golden/ue5.6/operations/prop_set_color/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/prop_set_color/after.uasset",
			argv:     []string{"prop", "set", "--export", "3", "--path", "VColor", "--value", `{"structType":"LinearColor","value":{"r":0.25,"g":0.5,"b":0.75,"a":1}}`},
		},
		{
			name:     "prop_set_nested_struct",
			before:   "../../testdata/golden/ue5.6/operations/prop_set_nested_struct/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/prop_set_nested_struct/after.uasset",
			argv:     []string{"prop", "set", "--export", "3", "--path", "VTransform.Translation", "--value", `{"x":1,"y":2,"z":3}`},
		},
		{
			name:     "prop_set_transform",
			before:   "../../testdata/golden/ue5.6/operations/prop_set_transform/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/prop_set_transform/after.uasset",
			argv:     []string{"prop", "set", "--export", "3", "--path", "VTransform", "--value", `{"structType":"Transform","value":{"Translation":{"type":"StructProperty(Vector(/Script/CoreUObject))","value":{"structType":"Vector","value":{"x":1,"y":2,"z":3}}},"Rotation":{"type":"StructProperty(Quat(/Script/CoreUObject))","value":{"structType":"Quat","value":{"x":0,"y":0,"z":0,"w":1}}},"Scale3D":{"type":"StructProperty(Vector(/Script/CoreUObject))","value":{"structType":"Vector","value":{"x":1,"y":1,"z":1}}}}}`},
		},
		{
			name:     "prop_set_custom_struct_enum",
			before:   "../../testdata/golden/ue5.6/operations/prop_set_custom_struct_enum/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/prop_set_custom_struct_enum/after.uasset",
			argv:     []string{"prop", "set", "--export", "5", "--path", "FixtureCustom.EnumVal", "--value", "1"},
		},
		{
			name:     "var_set_default_string",
			before:   "../../testdata/golden/ue5.6/operations/var_set_default_string/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/var_set_default_string/after.uasset",
			argv:     []string{"var", "set-default", "--name", "VString", "--value", `"golden"`},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			workPath := filepath.Join(tmpDir, "work.uasset")
			data, err := os.ReadFile(tc.before)
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			if err := os.WriteFile(workPath, data, 0o644); err != nil {
				t.Fatalf("write work file: %v", err)
			}

			argv := make([]string, 0, len(tc.argv)+1)
			argv = append(argv, tc.argv[:2]...)
			argv = append(argv, workPath)
			argv = append(argv, tc.argv[2:]...)

			var stdout, stderr strings.Builder
			if code := Run(argv, &stdout, &stderr); code != 0 {
				t.Fatalf("run command: code=%d stderr=%s", code, stderr.String())
			}

			actualAsset, err := uasset.ParseFile(workPath, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse actual: %v", err)
			}
			expectedAsset, err := uasset.ParseFile(tc.expected, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse expected: %v", err)
			}

			actualLeafs := collectFiBLeafSummaries(t, actualAsset)
			expectedLeafs := collectFiBLeafSummaries(t, expectedAsset)
			if slices.Equal(actualLeafs, expectedLeafs) {
				return
			}

			diffs := make([]string, 0, 12)
			limit := min(len(actualLeafs), len(expectedLeafs))
			for i := 0; i < limit && len(diffs) < 12; i++ {
				if actualLeafs[i] == expectedLeafs[i] {
					continue
				}
				diffs = append(diffs, fmt.Sprintf("%d actual=%q expected=%q", i, actualLeafs[i], expectedLeafs[i]))
			}
			if len(actualLeafs) != len(expectedLeafs) {
				diffs = append(diffs, fmt.Sprintf("len actual=%d expected=%d", len(actualLeafs), len(expectedLeafs)))
			}
			t.Fatalf("FiB leaf mismatches: %v", diffs)
		})
	}
}

func TestDebugDumpFiBLeaves(t *testing.T) {
	if testing.Short() {
		t.Skip("debug")
	}
	paths := []struct {
		name string
		path string
	}{
		{name: "before_nested_struct", path: "../../testdata/golden/ue5.6/operations/prop_set_nested_struct/before.uasset"},
		{name: "after_nested_struct", path: "../../testdata/golden/ue5.6/operations/prop_set_nested_struct/after.uasset"},
		{name: "before_color", path: "../../testdata/golden/ue5.6/operations/prop_set_color/before.uasset"},
		{name: "after_color", path: "../../testdata/golden/ue5.6/operations/prop_set_color/after.uasset"},
	}
	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			asset, err := uasset.ParseFile(tc.path, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse asset: %v", err)
			}
			leafs := collectFiBLeafSummaries(t, asset)
			for i, leaf := range leafs {
				if i < 20 || i > 40 {
					continue
				}
				t.Logf("%d %s", i, leaf)
			}
		})
	}
}

func TestDebugDumpFiBJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("debug")
	}
	paths := []struct {
		name string
		path string
	}{
		{name: "before_nested_struct", path: "../../testdata/golden/ue5.6/operations/prop_set_nested_struct/before.uasset"},
		{name: "after_nested_struct", path: "../../testdata/golden/ue5.6/operations/prop_set_nested_struct/after.uasset"},
		{name: "before_color", path: "../../testdata/golden/ue5.6/operations/prop_set_color/before.uasset"},
		{name: "after_color", path: "../../testdata/golden/ue5.6/operations/prop_set_color/after.uasset"},
		{name: "before_custom_enum", path: "../../testdata/golden/ue5.6/operations/prop_set_custom_struct_enum/before.uasset"},
		{name: "after_custom_enum", path: "../../testdata/golden/ue5.6/operations/prop_set_custom_struct_enum/after.uasset"},
		{name: "before_prop_text", path: "../../testdata/golden/ue5.6/operations/prop_set_text/before.uasset"},
		{name: "after_prop_text", path: "../../testdata/golden/ue5.6/operations/prop_set_text/after.uasset"},
	}
	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			asset, err := uasset.ParseFile(tc.path, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse asset: %v", err)
			}
			section, _, _, err := parseAssetRegistrySection(asset)
			if err != nil {
				t.Fatalf("parse asset registry: %v", err)
			}
			for _, obj := range section.Objects {
				for _, tag := range obj.Tags {
					if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) {
						continue
					}
					parsed, err := parseFiBData(asset, tag.Value)
					if err != nil {
						t.Fatalf("parse fib: %v", err)
					}
					t.Log(parsed.JSON)
				}
			}
		})
	}
}

func TestDebugDumpAssetRegistryTags(t *testing.T) {
	if testing.Short() {
		t.Skip("debug")
	}
	paths := []struct {
		name string
		path string
	}{
		{name: "before_prop_text", path: "../../testdata/golden/ue5.6/operations/prop_set_text/before.uasset"},
		{name: "after_prop_text", path: "../../testdata/golden/ue5.6/operations/prop_set_text/after.uasset"},
	}
	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			asset, err := uasset.ParseFile(tc.path, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse asset: %v", err)
			}
			section, _, _, err := parseAssetRegistrySection(asset)
			if err != nil {
				t.Fatalf("parse asset registry: %v", err)
			}
			for _, obj := range section.Objects {
				t.Logf("%s %s", obj.ObjectPath, obj.ObjectClass)
				for _, tag := range obj.Tags {
					if strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) {
						t.Logf("  %s len=%d", tag.Key, len(tag.Value))
						continue
					}
					t.Logf("  %s = %q", tag.Key, tag.Value)
				}
			}
		})
	}
}

func TestDebugDumpCurrentFiBJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("debug")
	}
	cases := []struct {
		name     string
		before   string
		expected string
		argv     []string
	}{
		{
			name:     "var_set_default_vector",
			before:   "../../testdata/golden/ue5.6/operations/var_set_default_vector/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/var_set_default_vector/after.uasset",
			argv:     []string{"var", "set-default", "--name", "VVector", "--value", `{"x":1,"y":2,"z":3}`},
		},
		{
			name:     "prop_set_color",
			before:   "../../testdata/golden/ue5.6/operations/prop_set_color/before.uasset",
			expected: "../../testdata/golden/ue5.6/operations/prop_set_color/after.uasset",
			argv:     []string{"prop", "set", "--export", "3", "--path", "VColor", "--value", `{"structType":"LinearColor","value":{"r":0.25,"g":0.5,"b":0.75,"a":1}}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			workPath := filepath.Join(tmpDir, "work.uasset")
			data, err := os.ReadFile(tc.before)
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			if err := os.WriteFile(workPath, data, 0o644); err != nil {
				t.Fatalf("write work file: %v", err)
			}
			argv := append([]string{tc.argv[0], tc.argv[1], workPath}, tc.argv[2:]...)
			var stdout, stderr strings.Builder
			if code := Run(argv, &stdout, &stderr); code != 0 {
				t.Fatalf("run: code=%d stderr=%s", code, stderr.String())
			}

			actualAsset, err := uasset.ParseFile(workPath, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse actual: %v", err)
			}
			expectedAsset, err := uasset.ParseFile(tc.expected, uasset.DefaultParseOptions())
			if err != nil {
				t.Fatalf("parse expected: %v", err)
			}
			t.Logf("actual %s", firstFiBJSON(t, actualAsset))
			t.Logf("expected %s", firstFiBJSON(t, expectedAsset))
		})
	}
}

func firstFiBJSON(t *testing.T, asset *uasset.Asset) string {
	t.Helper()
	section, _, _, err := parseAssetRegistrySection(asset)
	if err != nil {
		t.Fatalf("parse asset registry: %v", err)
	}
	for _, obj := range section.Objects {
		for _, tag := range obj.Tags {
			if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) {
				continue
			}
			parsed, err := parseFiBData(asset, tag.Value)
			if err != nil {
				t.Fatalf("parse fib: %v", err)
			}
			return parsed.JSON
		}
	}
	t.Fatalf("fib data missing")
	return ""
}

func collectFiBLeafSummaries(t *testing.T, asset *uasset.Asset) []string {
	t.Helper()

	section, _, _, err := parseAssetRegistrySection(asset)
	if err != nil {
		t.Fatalf("parse asset registry: %v", err)
	}
	if section == nil {
		t.Fatalf("asset registry section missing")
	}

	out := make([]string, 0, 128)
	for _, obj := range section.Objects {
		for _, tag := range obj.Tags {
			if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) &&
				!strings.EqualFold(tag.Key, assetRegistryTagUnversionedFindInBlueprintsData) {
				continue
			}
			parsed, err := parseFiBData(asset, tag.Value)
			if err != nil {
				t.Fatalf("parse FiBData %s/%s: %v", obj.ObjectPath, tag.Key, err)
			}
			for _, entry := range parsed.Lookup {
				out = append(out, collectFiBTextLeafSummaries(obj.ObjectPath, tag.Key, entry.Key, entry.Text)...)
			}
		}
	}
	return out
}

func collectFiBTextLeafSummaries(objectPath, tagKey string, entryKey int32, text fibLookupText) []string {
	prefix := fmt.Sprintf("%s/%s#%d", objectPath, tagKey, entryKey)
	switch text.HistoryTypeCode {
	case 255:
		return []string{fmt.Sprintf("%s|None|%s", prefix, text.CultureInvariantString)}
	case 0:
		return []string{fmt.Sprintf("%s|Base|%s|%s|%s", prefix, text.Namespace, text.Key, text.SourceString)}
	case 11:
		return []string{fmt.Sprintf("%s|StringTableEntry|%s|%s", prefix, text.TableIDName, text.Key)}
	}

	out := make([]string, 0, 8)
	if text.FormatText != nil {
		out = append(out, collectFiBTextLeafSummaries(prefix, "format", entryKey, *text.FormatText)...)
	}
	for _, arg := range text.NamedArguments {
		if arg.Value.Text != nil {
			out = append(out, collectFiBTextLeafSummaries(prefix, "named:"+arg.Name, entryKey, *arg.Value.Text)...)
		}
	}
	for i, arg := range text.OrderedArguments {
		if arg.Text != nil {
			out = append(out, collectFiBTextLeafSummaries(prefix, fmt.Sprintf("ordered:%d", i), entryKey, *arg.Text)...)
		}
	}
	for _, arg := range text.ArgumentData {
		if arg.Text != nil {
			out = append(out, collectFiBTextLeafSummaries(prefix, "arg:"+arg.Name, entryKey, *arg.Text)...)
		}
	}
	if text.SourceValue != nil && text.SourceValue.Text != nil {
		out = append(out, collectFiBTextLeafSummaries(prefix, "sourceValue", entryKey, *text.SourceValue.Text)...)
	}
	if text.SourceText != nil {
		out = append(out, collectFiBTextLeafSummaries(prefix, "sourceText", entryKey, *text.SourceText)...)
	}
	return out
}

func firstByteDiff(left, right []byte) int {
	limit := min(len(left), len(right))
	for i := 0; i < limit; i++ {
		if left[i] != right[i] {
			return i
		}
	}
	return limit
}

func firstStringDiff(left, right string) int {
	lr := []rune(left)
	rr := []rune(right)
	limit := min(len(lr), len(rr))
	for i := 0; i < limit; i++ {
		if lr[i] != rr[i] {
			return i
		}
	}
	return limit
}

func TestDebugOperationFiBData(t *testing.T) {
	caseName := strings.TrimSpace(os.Getenv("BPX_DEBUG_FIB_CASE"))
	if caseName == "" {
		t.Skip("set BPX_DEBUG_FIB_CASE to inspect one operation fixture")
	}

	beforePath := filepath.Join("../../testdata/golden/ue5.6/operations", caseName, "before.uasset")
	expectedPath := filepath.Join("../../testdata/golden/ue5.6/operations", caseName, "after.uasset")
	opPath := filepath.Join("../../testdata/golden/ue5.6/operations", caseName, "operation.json")

	specBytes, err := os.ReadFile(opPath)
	if err != nil {
		t.Fatalf("read operation spec: %v", err)
	}
	var spec operationSpec
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("parse operation spec: %v", err)
	}

	tmpDir := t.TempDir()
	workPath := filepath.Join(tmpDir, "work.uasset")
	beforeBytes, err := os.ReadFile(beforePath)
	if err != nil {
		t.Fatalf("read before fixture: %v", err)
	}
	if err := os.WriteFile(workPath, beforeBytes, 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}

	argv, err := buildOperationArgv(spec, workPath)
	if err != nil {
		t.Fatalf("build argv: %v", err)
	}
	var stdout, stderr strings.Builder
	if code := Run(argv, &stdout, &stderr); code != 0 {
		t.Fatalf("run command: code=%d stderr=%s", code, stderr.String())
	}

	actualAsset, err := uasset.ParseFile(workPath, uasset.DefaultParseOptions())
	if err != nil {
		t.Fatalf("parse actual: %v", err)
	}
	expectedAsset, err := uasset.ParseFile(expectedPath, uasset.DefaultParseOptions())
	if err != nil {
		t.Fatalf("parse expected: %v", err)
	}

	actualFiB := mustFirstFiBTag(t, actualAsset)
	expectedFiB := mustFirstFiBTag(t, expectedAsset)
	jsonDiff := firstStringDiff(actualFiB.JSON, expectedFiB.JSON)
	t.Logf("lookup count actual=%d expected=%d", len(actualFiB.Lookup), len(expectedFiB.Lookup))
	t.Logf("json len actual=%d expected=%d firstDiff=%d", len(actualFiB.JSON), len(expectedFiB.JSON), jsonDiff)
	if jsonDiff < len([]rune(actualFiB.JSON)) || jsonDiff < len([]rune(expectedFiB.JSON)) {
		actualJSONRunes := []rune(actualFiB.JSON)
		expectedJSONRunes := []rune(expectedFiB.JSON)
		start := max(0, jsonDiff-48)
		actualEnd := min(len(actualJSONRunes), jsonDiff+96)
		expectedEnd := min(len(expectedJSONRunes), jsonDiff+96)
		t.Logf("json actual[%d:%d]=%q", start, actualEnd, string(actualJSONRunes[start:actualEnd]))
		t.Logf("json expected[%d:%d]=%q", start, expectedEnd, string(expectedJSONRunes[start:expectedEnd]))
	}

	limit := min(len(actualFiB.Lookup), len(expectedFiB.Lookup))
	for i := 0; i < limit; i++ {
		actualEntry := actualFiB.Lookup[i]
		expectedEntry := expectedFiB.Lookup[i]
		if actualEntry.Key != expectedEntry.Key ||
			actualEntry.Text.HistoryTypeCode != expectedEntry.Text.HistoryTypeCode ||
			!bytes.Equal(actualEntry.Text.Raw, expectedEntry.Text.Raw) {
			t.Logf(
				"entry[%d] key actual=%d expected=%d type actual=%s expected=%s rawLen actual=%d expected=%d firstDiff=%d",
				i,
				actualEntry.Key,
				expectedEntry.Key,
				fibHistoryTypeName(actualEntry.Text.HistoryTypeCode),
				fibHistoryTypeName(expectedEntry.Text.HistoryTypeCode),
				len(actualEntry.Text.Raw),
				len(expectedEntry.Text.Raw),
				firstByteDiff(actualEntry.Text.Raw, expectedEntry.Text.Raw),
			)
			t.Logf("entry[%d] actual leafs=%v", i, collectFiBTextLeafSummaries("actual", "FiBData", actualEntry.Key, actualEntry.Text))
			t.Logf("entry[%d] expected leafs=%v", i, collectFiBTextLeafSummaries("expected", "FiBData", expectedEntry.Key, expectedEntry.Text))
			start := max(0, i-3)
			end := min(limit, i+6)
			for j := start; j < end; j++ {
				t.Logf("actual entry[%d]=%v", j, collectFiBTextLeafSummaries("actual", "FiBData", actualFiB.Lookup[j].Key, actualFiB.Lookup[j].Text))
				t.Logf("expected entry[%d]=%v", j, collectFiBTextLeafSummaries("expected", "FiBData", expectedFiB.Lookup[j].Key, expectedFiB.Lookup[j].Text))
			}
			break
		}
	}
}

func mustFirstFiBTag(t *testing.T, asset *uasset.Asset) *fibEncodedData {
	t.Helper()

	section, _, _, err := parseAssetRegistrySection(asset)
	if err != nil {
		t.Fatalf("parse asset registry: %v", err)
	}
	if section == nil {
		t.Fatalf("asset registry section missing")
	}

	for _, obj := range section.Objects {
		for _, tag := range obj.Tags {
			if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) &&
				!strings.EqualFold(tag.Key, assetRegistryTagUnversionedFindInBlueprintsData) {
				continue
			}
			parsed, err := parseFiBData(asset, tag.Value)
			if err != nil {
				t.Fatalf("parse FiBData %s/%s: %v", obj.ObjectPath, tag.Key, err)
			}
			return parsed
		}
	}
	t.Fatalf("FiBData tag not found")
	return nil
}
