package edit

import (
	"os"
	"path/filepath"
	"testing"
)

var supportedGoldenVersions = []string{"ue5.6", "ue5.7"}

func goldenFixtureRoots(t *testing.T, requiredSubdir string) []string {
	t.Helper()

	base := filepath.Join("..", "..", "testdata", "golden")
	roots := make([]string, 0, len(supportedGoldenVersions))
	for _, version := range supportedGoldenVersions {
		root := filepath.Join(base, version)
		if info, err := os.Stat(filepath.Join(root, requiredSubdir)); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		t.Skipf("no golden fixture roots found for %s in ue5.6/ue5.7", requiredSubdir)
	}
	return roots
}

func goldenParseFixturePath(root, name string) string {
	return filepath.Join(root, "parse", name)
}

func goldenOperationFixturePath(root, opName, name string) string {
	return filepath.Join(root, "operations", opName, name)
}
