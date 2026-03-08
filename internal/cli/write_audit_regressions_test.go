package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wilddogjp/openbpx/pkg/edit"
	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func TestDataTableAddRowUsesDefaultRowPayloadNotFirstRowTemplate(t *testing.T) {
	beforePath := goldenOperationFixturePath(t, "dt_add_row", "before.uasset")
	asset := mustParseCLIAsset(t, beforePath)

	targetIdx, err := resolveDataTableExportIndexForUpdate(asset, 0)
	if err != nil {
		t.Fatalf("resolveDataTableExportIndexForUpdate: %v", err)
	}
	layout, err := detectDataTableRowLayout(asset, targetIdx)
	if err != nil {
		t.Fatalf("detectDataTableRowLayout: %v", err)
	}
	if len(layout.Rows) < 3 {
		t.Fatalf("expected at least 3 rows, got %d", len(layout.Rows))
	}

	serialStart, serialEnd, err := dataTableSerialRange(asset, targetIdx)
	if err != nil {
		t.Fatalf("dataTableSerialRange: %v", err)
	}
	oldPayload := asset.Raw.Bytes[serialStart:serialEnd]
	rowsStart := layout.Rows[0].NameStart - serialStart
	rowsEnd := layout.RowsEnd - serialStart
	if rowsStart < 0 || rowsEnd < rowsStart || rowsEnd > len(oldPayload) {
		t.Fatalf("row range out of bounds: %d..%d size=%d", rowsStart, rowsEnd, len(oldPayload))
	}

	entryBytes := func(row dataTableRowLocation) []byte {
		start := row.NameStart - serialStart
		end := row.End - serialStart
		return append([]byte(nil), oldPayload[start:end]...)
	}

	// Put the non-default row first. The add-row implementation must still use
	// the struct default payload, not clone the first serialized row.
	reorderedPayload := make([]byte, 0, len(oldPayload))
	reorderedPayload = append(reorderedPayload, oldPayload[:rowsStart]...)
	reorderedPayload = append(reorderedPayload, entryBytes(layout.Rows[1])...)
	reorderedPayload = append(reorderedPayload, entryBytes(layout.Rows[0])...)
	reorderedPayload = append(reorderedPayload, entryBytes(layout.Rows[2])...)
	reorderedPayload = append(reorderedPayload, oldPayload[rowsEnd:]...)

	outBytes, err := edit.RewriteAsset(asset, []edit.ExportMutation{{
		ExportIndex: targetIdx,
		Payload:     reorderedPayload,
	}})
	if err != nil {
		t.Fatalf("RewriteAsset(reorder): %v", err)
	}

	tempDir := t.TempDir()
	workPath := filepath.Join(tempDir, "work.uasset")
	if err := os.WriteFile(workPath, outBytes, 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{
		"datatable", "add-row", workPath,
		"--row", "Row_A_1",
		"--values", `{"Score":123}`,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run datatable add-row: code=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"datatable", "read", workPath,
		"--row", "Row_A_1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run datatable read: code=%d stderr=%s", code, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode datatable read output: %v\nstdout=%s", err, stdout.String())
	}
	tables, ok := payload["tables"].([]any)
	if !ok || len(tables) == 0 {
		t.Fatalf("tables payload missing: %#v", payload["tables"])
	}
	table, ok := tables[0].(map[string]any)
	if !ok {
		t.Fatalf("table payload type: %#v", tables[0])
	}
	rows, ok := table["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected one filtered row, got %#v", table["rows"])
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("row payload type: %#v", rows[0])
	}
	props, ok := row["properties"].([]any)
	if !ok {
		t.Fatalf("properties payload missing: %#v", row["properties"])
	}

	got := map[string]any{}
	for _, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := prop["name"].(string)
		got[name] = prop["value"]
	}

	if score, ok := got["Score"].(float64); !ok || score != 123 {
		t.Fatalf("Score: got %#v want 123", got["Score"])
	}
	if rate, ok := got["Rate"].(float64); !ok || rate != 0 {
		t.Fatalf("Rate: got %#v want 0", got["Rate"])
	}
	if label, ok := got["Label"].(string); !ok || label != "" {
		t.Fatalf("Label: got %#v want empty string", got["Label"])
	}
	modeValue, ok := got["Mode"].(map[string]any)
	if !ok {
		t.Fatalf("Mode payload: %#v", got["Mode"])
	}
	if gotMode, _ := modeValue["value"].(string); gotMode != "EBPXFixtureEnum::BPXEnum_ValueA" {
		t.Fatalf("Mode value: got %q want %q", gotMode, "EBPXFixtureEnum::BPXEnum_ValueA")
	}
}

func TestRunVarRenameRejectsNameMapOnlyRename(t *testing.T) {
	fixturePath := goldenParseFixturePath(t, "BP_Empty.uasset")
	asset := mustParseCLIAsset(t, fixturePath)
	declared, _ := collectDeclaredVariables(asset)

	fromName := ""
	for _, entry := range asset.Names {
		value := strings.TrimSpace(entry.Value)
		if value == "" || strings.EqualFold(value, "None") {
			continue
		}
		if _, ok := declared[value]; ok {
			continue
		}
		if strings.ContainsAny(value, "/.:") {
			continue
		}
		fromName = value
		break
	}
	if fromName == "" {
		t.Skip("no suitable non-declared NameMap entry found")
	}

	orig, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tempDir := t.TempDir()
	workPath := filepath.Join(tempDir, "work.uasset")
	if err := os.WriteFile(workPath, orig, 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"var", "rename", workPath, "--from", fromName, "--to", fromName + "_Renamed"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected var rename to reject NameMap-only rename")
	}
	if !strings.Contains(stderr.String(), "refusing NameMap-only rename") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}

	after, err := os.ReadFile(workPath)
	if err != nil {
		t.Fatalf("read work file after failure: %v", err)
	}
	if !bytes.Equal(orig, after) {
		t.Fatalf("failed var rename must not modify bytes")
	}
}

func TestRefRewriteFailsOnUnsupportedPropertyMutation(t *testing.T) {
	asset, err := uasset.ParseBytes(buildCLIFixture(t, "/Game/Old/Mesh"), uasset.DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if len(asset.Exports) == 0 {
		t.Fatalf("fixture has no exports")
	}

	// Force a tagged-property parse warning so rewriteReferencesAsset must fail
	// instead of partially applying unrelated NameMap changes.
	asset.Exports[0].SerialSize -= 2
	_, _, _, _, err = rewriteReferencesAsset(asset, uasset.DefaultParseOptions(), "/Game/Old/Mesh", "/Game/New/Mesh")
	if err == nil {
		t.Fatalf("expected ref rewrite to fail on unsafe export parse warnings")
	}
	if !strings.Contains(err.Error(), "cannot safely rewrite references") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyLocalizationBulkTextRewriteRejectsGatherableWarnings(t *testing.T) {
	asset := mustParseCLIAsset(t, goldenParseFixturePath(t, "BP_Empty.uasset"))
	asset.Summary.GatherableTextDataCount = 1
	asset.Summary.GatherableTextDataOffset = int32(len(asset.Raw.Bytes) + 128)

	_, _, _, err := applyLocalizationBulkTextRewrite(asset, uasset.DefaultParseOptions(), func(history map[string]any) int {
		return 0
	})
	if err == nil {
		t.Fatalf("expected gatherable localization warning to become an error")
	}
	if !strings.Contains(err.Error(), "cannot safely rewrite gatherable text data") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssetRegistryScalarSearchValueSupportsTextHistoryMaps(t *testing.T) {
	value, ok := assetRegistryScalarSearchValue(map[string]any{
		"historyType":  "Base",
		"sourceString": "Gameplay",
		"value":        "Gameplay",
	})
	if !ok {
		t.Fatalf("expected text history map to be supported")
	}
	if got, ok := value.(string); !ok || got != "Gameplay" {
		t.Fatalf("value: got %#v want %q", value, "Gameplay")
	}
}

func TestRewriteAssetRegistryValueChangeRejectsUnsupportedSearchValue(t *testing.T) {
	asset := mustParseCLIAsset(t, goldenOperationFixturePath(t, "var_rename_simple", "before.uasset"))
	propertyName := ""
	seen := map[string]struct{}{}
	for _, entry := range asset.Names {
		for _, candidate := range []string{
			strings.TrimSpace(entry.Value),
			blueprintNameToDisplayString(entry.Value, false),
		} {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			hasProperty, err := assetRegistryContainsPropertySearch(asset, candidate)
			if err != nil {
				t.Fatalf("assetRegistryContainsPropertySearch(%q): %v", candidate, err)
			}
			if hasProperty {
				propertyName = candidate
				break
			}
		}
		if propertyName != "" {
			break
		}
	}
	if propertyName == "" {
		t.Skip("no FiB property search entry was discoverable from fixture NameMap candidates")
	}
	hasProperty, err := assetRegistryContainsPropertySearch(asset, propertyName)
	if err != nil {
		t.Fatalf("assetRegistryContainsPropertySearch: %v", err)
	}
	if !hasProperty {
		t.Fatalf("expected FiB search data for property %q", propertyName)
	}

	_, _, err = rewriteAssetRegistryValueChange(asset, propertyName, map[string]any{"unsupported": true}, nil, nil)
	if err == nil {
		t.Fatalf("expected unsupported FiB rewrite to fail")
	}
	if !strings.Contains(err.Error(), "not safely rewritable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustParseCLIAsset(t *testing.T, path string) *uasset.Asset {
	t.Helper()

	asset, err := uasset.ParseFile(path, uasset.DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", path, err)
	}
	return asset
}
