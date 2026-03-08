package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMaterialReadAggregatesInspectAndHLSL(t *testing.T) {
	fixture := goldenParseFixturePath(t, "MI_Chrome.uasset")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"material", "read", fixture}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d stderr=%s", code, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v\nstdout=%s", err, stdout.String())
	}
	if got, ok := payload["childrenScanned"].(bool); !ok || got {
		t.Fatalf("childrenScanned: got %#v want false", payload["childrenScanned"])
	}
	inspect, ok := payload["inspect"].(map[string]any)
	if !ok {
		t.Fatalf("inspect payload: %#v", payload["inspect"])
	}
	if got, want := int(inspect["materialCount"].(float64)), 1; got != want {
		t.Fatalf("inspect.materialCount: got %d want %d", got, want)
	}
	materials, ok := inspect["materials"].([]any)
	if !ok || len(materials) != 1 {
		t.Fatalf("inspect.materials payload: %#v", inspect["materials"])
	}
	firstMaterial, ok := materials[0].(map[string]any)
	if !ok {
		t.Fatalf("inspect.materials[0] type: %T", materials[0])
	}
	parent, ok := firstMaterial["parent"].(map[string]any)
	if !ok {
		t.Fatalf("inspect.materials[0].parent payload: %#v", firstMaterial["parent"])
	}
	if got, want := parent["resolved"], "import:3:WorldGridMaterial"; got != want {
		t.Fatalf("inspect.materials[0].parent.resolved: got %v want %v", got, want)
	}
	if got := int(firstMaterial["assetReferenceCount"].(float64)); got < 1 {
		t.Fatalf("inspect.materials[0].assetReferenceCount: got %d want >= 1", got)
	}

	hlsl, ok := payload["hlsl"].(map[string]any)
	if !ok {
		t.Fatalf("hlsl payload: %#v", payload["hlsl"])
	}
	if got, want := int(hlsl["customExpressionCount"].(float64)), 0; got != want {
		t.Fatalf("hlsl.customExpressionCount: got %d want %d", got, want)
	}
	if got, ok := hlsl["hlslAvailable"].(bool); !ok || got {
		t.Fatalf("hlsl.hlslAvailable: got %#v want false", hlsl["hlslAvailable"])
	}
	note, _ := hlsl["note"].(string)
	if !strings.Contains(note, "FHLSLMaterialTranslator") {
		t.Fatalf("hlsl.note must mention FHLSLMaterialTranslator, got: %s", note)
	}
}

func TestRunMaterialReadChildrenScanUsesDerivedParentToken(t *testing.T) {
	tempDir := t.TempDir()
	fixture := goldenParseFixturePath(t, "MI_Chrome.uasset")
	workFile := filepath.Join(tempDir, "MI_Chrome.uasset")
	body, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(workFile, body, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"material", "read", fixture, "--children-root", tempDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d stderr=%s", code, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v\nstdout=%s", err, stdout.String())
	}
	if got, ok := payload["childrenScanned"].(bool); !ok || !got {
		t.Fatalf("childrenScanned: got %#v want true", payload["childrenScanned"])
	}
	if got, want := payload["childrenParentFilter"], "MI_Chrome"; got != want {
		t.Fatalf("childrenParentFilter: got %v want %v", got, want)
	}
	children, ok := payload["children"].(map[string]any)
	if !ok {
		t.Fatalf("children payload: %#v", payload["children"])
	}
	if got, want := int(children["matchCount"].(float64)), 0; got != want {
		t.Fatalf("children.matchCount: got %d want %d", got, want)
	}
}

func TestRunMaterialLegacySubcommandsAreRejected(t *testing.T) {
	tests := [][]string{
		{"material", "inspect", "/tmp/nonexistent.uasset"},
		{"material", "children", "/tmp", "--parent", "M_Master"},
		{"material", "hlsl", "/tmp/nonexistent.uasset"},
	}
	for _, argv := range tests {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Run(argv, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("argv=%v exit code: got %d want 1", argv, code)
		}
		if !strings.Contains(stderr.String(), "unknown material command") {
			t.Fatalf("argv=%v expected unknown material command, got: %s", argv, stderr.String())
		}
	}
}
