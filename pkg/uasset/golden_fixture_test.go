package uasset

import (
	"os"
	"path/filepath"
	"testing"
)

var supportedGoldenVersions = []string{"ue5.6", "ue5.7"}

func goldenParseFixtureRoots(t *testing.T) []string {
	t.Helper()

	base := filepath.Join("..", "..", "testdata", "golden")
	roots := make([]string, 0, len(supportedGoldenVersions))
	for _, version := range supportedGoldenVersions {
		root := filepath.Join(base, version)
		candidate := filepath.Join(root, "parse")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		t.Skip("no golden parse fixture roots found for ue5.6/ue5.7")
	}
	return roots
}

func goldenParseFixturePath(root, name string) string {
	return filepath.Join(root, "parse", name)
}
