package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRunLevelActorSearchFindsByClass(t *testing.T) {
	mapPath := goldenParseFixturePath(t, "L_Minimal.umap")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"level", "actor-search", mapPath, "--actor-class", "LyraWorldSettings"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d stderr=%s", code, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v\nstdout=%s", err, stdout.String())
	}
	if got, want := int(payload["matchCount"].(float64)), 1; got != want {
		t.Fatalf("matchCount: got %d want %d", got, want)
	}
	matches, ok := payload["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches payload: %#v", payload["matches"])
	}
	first, ok := matches[0].(map[string]any)
	if !ok {
		t.Fatalf("match[0] type: %T", matches[0])
	}
	if got, want := first["objectName"], "LyraWorldSettings"; got != want {
		t.Fatalf("objectName: got %v want %v", got, want)
	}
	if got, want := int(first["export"].(float64)), 4; got != want {
		t.Fatalf("export: got %d want %d", got, want)
	}
}

func TestRunLevelActorSearchAppliesLimit(t *testing.T) {
	mapPath := goldenParseFixturePath(t, "L_Minimal.umap")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"level", "actor-search", mapPath, "--name", "Polys", "--limit", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d stderr=%s", code, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v\nstdout=%s", err, stdout.String())
	}
	if got, want := int(payload["limit"].(float64)), 1; got != want {
		t.Fatalf("limit: got %d want %d", got, want)
	}
	if got, want := int(payload["matchCount"].(float64)), 1; got != want {
		t.Fatalf("matchCount: got %d want %d", got, want)
	}
	matches, ok := payload["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches payload: %#v", payload["matches"])
	}
	first := matches[0].(map[string]any)
	if got, want := first["objectName"], "Polys_0"; got != want {
		t.Fatalf("objectName: got %v want %v", got, want)
	}
}
