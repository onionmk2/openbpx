package uasset

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyntheticErrors(t *testing.T) {
	syntheticDir := filepath.Join("..", "..", "testdata", "synthetic")
	entries, err := os.ReadDir(syntheticDir)
	if err != nil {
		t.Fatalf("read synthetic dir: %v", err)
	}

	fixtures := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), "gen_synthetic.go") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "BP_UE") {
			continue
		}
		if entry.Name() == "BP_FutureVersion.uasset" {
			continue
		}
		fixtures = append(fixtures, filepath.Join(syntheticDir, entry.Name()))
	}
	if len(fixtures) == 0 {
		t.Fatalf("no synthetic error fixtures found")
	}

	opts := DefaultParseOptions()
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			_, err := ParseFile(fixture, opts)
			if err == nil {
				t.Fatalf("expected parse failure for synthetic fixture")
			}
		})
	}
}

func TestVersionWindow(t *testing.T) {
	syntheticDir := filepath.Join("..", "..", "testdata", "synthetic")
	rejectedFixture := filepath.Join(syntheticDir, "BP_FutureVersion.uasset")
	if _, err := ParseFile(rejectedFixture, DefaultParseOptions()); err == nil {
		t.Fatalf("expected future-version fixture to be rejected")
	} else if !strings.Contains(err.Error(), "unsupported fileVersionUE5=9999") {
		t.Fatalf("expected version-window rejection, got: %v", err)
	}
}

func TestSyntheticInWindowVersionFixturesRoundTrip(t *testing.T) {
	syntheticDir := filepath.Join("..", "..", "testdata", "synthetic")
	fixtures := []struct {
		name        string
		fileUE5     int32
		engineMinor uint16
	}{
		{name: "BP_UE54.uasset", fileUE5: 1009, engineMinor: 4},
		{name: "BP_UE55.uasset", fileUE5: 1014, engineMinor: 5},
	}

	opts := DefaultParseOptions()
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(syntheticDir, fixture.name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			asset, err := ParseBytes(data, opts)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if got, want := asset.Summary.FileVersionUE5, fixture.fileUE5; got != want {
				t.Fatalf("fileVersionUE5: got %d want %d", got, want)
			}
			if got, want := asset.Summary.SavedByEngineVersion.Minor, fixture.engineMinor; got != want {
				t.Fatalf("saved engine minor: got %d want %d", got, want)
			}

			serialized := asset.Raw.SerializeUnmodified()
			if !bytes.Equal(data, serialized) {
				t.Fatalf("roundtrip mismatch")
			}

			reparsed, err := ParseBytes(serialized, opts)
			if err != nil {
				t.Fatalf("reparse roundtrip bytes: %v", err)
			}
			if got, want := reparsed.Summary.FileVersionUE5, fixture.fileUE5; got != want {
				t.Fatalf("reparsed fileVersionUE5: got %d want %d", got, want)
			}
		})
	}
}
