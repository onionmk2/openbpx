package validate

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func TestRunSuccess(t *testing.T) {
	asset := &uasset.Asset{
		Raw: uasset.RawAsset{Bytes: []byte{0x01, 0x02, 0x03}},
		Summary: uasset.PackageSummary{
			FileVersionUE5:       1018,
			NameCount:            1,
			ImportCount:          0,
			ExportCount:          0,
			NameOffset:           0,
			ImportOffset:         0,
			ExportOffset:         0,
			SavedByEngineVersion: uasset.EngineVersion{Major: 5, Minor: 6},
		},
		Names:   []uasset.NameEntry{{Value: "None"}},
		Imports: []uasset.ImportEntry{},
		Exports: []uasset.ExportEntry{},
	}

	report := Run(asset, false)
	if !report.OK {
		t.Fatalf("expected successful report, got %+v", report)
	}
}

func TestRunDetectsBrokenExportSerialRange(t *testing.T) {
	asset := &uasset.Asset{
		Raw: uasset.RawAsset{Bytes: []byte{0x01, 0x02, 0x03}},
		Summary: uasset.PackageSummary{
			FileVersionUE5:       1018,
			NameCount:            1,
			ImportCount:          0,
			ExportCount:          1,
			NameOffset:           0,
			ImportOffset:         0,
			ExportOffset:         0,
			SavedByEngineVersion: uasset.EngineVersion{Major: 5, Minor: 6},
		},
		Names: []uasset.NameEntry{{Value: "None"}},
		Exports: []uasset.ExportEntry{{
			ObjectName:   uasset.NameRef{Index: 0, Number: 0},
			SerialOffset: 1,
			SerialSize:   10,
		}},
	}

	report := Run(asset, false)
	if report.OK {
		t.Fatalf("expected failure report")
	}
}

func TestRoundTripGoldenFixtures(t *testing.T) {
	goldenBase := filepath.Join("..", "..", "testdata", "golden")
	versionedRoots := make([]string, 0, 4)
	entries, err := os.ReadDir(goldenBase)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(goldenBase, entry.Name(), "parse")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			versionedRoots = append(versionedRoots, filepath.Join(goldenBase, entry.Name()))
		}
	}
	sort.Strings(versionedRoots)

	roots := make([]string, 0, 4)
	if len(versionedRoots) > 0 {
		roots = append(roots, versionedRoots...)
	} else if info, err := os.Stat(filepath.Join(goldenBase, "parse")); err == nil && info.IsDir() {
		roots = append(roots, goldenBase)
	}

	if len(roots) == 0 {
		t.Skip("no golden parse fixture roots present yet")
	}

	opts := uasset.DefaultParseOptions()
	for _, root := range roots {
		root := root
		t.Run(filepath.Base(root), func(t *testing.T) {
			parseDir := filepath.Join(root, "parse")
			patterns := []string{
				filepath.Join(parseDir, "*.uasset"),
				filepath.Join(parseDir, "*.umap"),
			}

			var fixtures []string
			for _, pattern := range patterns {
				matches, err := filepath.Glob(pattern)
				if err != nil {
					t.Fatalf("glob failed: %v", err)
				}
				fixtures = append(fixtures, matches...)
			}

			if len(fixtures) == 0 {
				t.Fatalf("no golden .uasset/.umap fixtures present in %s", parseDir)
			}

			for _, fixture := range fixtures {
				fixture := fixture
				t.Run(filepath.Base(fixture), func(t *testing.T) {
					data, err := os.ReadFile(fixture)
					if err != nil {
						t.Fatalf("read fixture: %v", err)
					}
					asset, err := uasset.ParseBytes(data, opts)
					if err != nil {
						t.Fatalf("parse fixture: %v", err)
					}
					if !bytes.Equal(data, asset.Raw.SerializeUnmodified()) {
						t.Fatalf("roundtrip mismatch")
					}
				})
			}
		})
	}
}

func TestRunNilAsset(t *testing.T) {
	report := Run(nil, false)
	if report.OK {
		t.Fatalf("expected nil asset report to fail")
	}
	if len(report.Checks) != 1 {
		t.Fatalf("checks len: got %d want 1", len(report.Checks))
	}
	if report.Checks[0].Name != "asset-present" {
		t.Fatalf("check name: got %q", report.Checks[0].Name)
	}
}
