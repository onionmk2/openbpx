package cli

import (
	"path/filepath"
	"testing"
)

func latestGoldenRoot(t *testing.T, requiredSubdir string) string {
	t.Helper()

	roots := goldenFixtureRoots(t, requiredSubdir)
	if len(roots) == 0 {
		t.Skipf("no golden fixture roots found for %s", requiredSubdir)
	}
	return roots[len(roots)-1]
}

func goldenParseFixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(latestGoldenRoot(t, "parse"), "parse", name)
}

func goldenOperationFixturePath(t *testing.T, opName, name string) string {
	t.Helper()
	return filepath.Join(latestGoldenRoot(t, "operations"), "operations", opName, name)
}
