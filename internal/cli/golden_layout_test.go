package cli

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func goldenFixtureRoots(t *testing.T, requiredSubdir string) []string {
	t.Helper()

	base := filepath.Join("..", "..", "testdata", "golden")
	versioned := make([]string, 0, 4)

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(base, entry.Name())
		if hasGoldenSubdir(candidate, requiredSubdir) {
			versioned = append(versioned, candidate)
		}
	}
	sort.Strings(versioned)
	if len(versioned) > 0 {
		return versioned
	}

	roots := make([]string, 0, 1)
	if hasGoldenSubdir(base, requiredSubdir) {
		roots = append(roots, base)
	}

	sort.Strings(roots)
	return roots
}

func hasGoldenSubdir(root, subdir string) bool {
	info, err := os.Stat(filepath.Join(root, subdir))
	if err != nil {
		return false
	}
	return info.IsDir()
}
