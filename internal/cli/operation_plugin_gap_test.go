package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestOperationFixturesMissingFromUEPluginAreExplicit(t *testing.T) {
	t.Parallel()

	pluginSpecs, err := uePluginGeneratedOperationNames()
	if err != nil {
		t.Fatalf("read UE plugin operation specs: %v", err)
	}
	if len(pluginSpecs) == 0 {
		t.Fatalf("no UE plugin operation specs found")
	}

	for _, root := range goldenFixtureRoots(t, "operations") {
		root := root
		t.Run(filepath.Base(root), func(t *testing.T) {
			entries, err := os.ReadDir(filepath.Join(root, "operations"))
			if err != nil {
				t.Fatalf("read operations dir: %v", err)
			}

			goldenOps := make([]string, 0, len(entries))
			for _, entry := range entries {
				if entry.IsDir() {
					goldenOps = append(goldenOps, entry.Name())
				}
			}
			slices.Sort(goldenOps)

			missing := make([]string, 0, 32)
			for _, name := range goldenOps {
				if !slices.Contains(pluginSpecs, name) {
					missing = append(missing, name)
				}
			}

			expectedMissing := []string{}
			if !slices.Equal(missing, expectedMissing) {
				t.Fatalf("unexpected UE plugin coverage gap\nactual=%v\nexpected=%v", missing, expectedMissing)
			}
		})
	}
}

func uePluginGeneratedOperationNames() ([]string, error) {
	body, err := os.ReadFile("../../testdata/BPXFixtureGenerator/Source/BPXFixtureGenerator/Private/BPXGenerateFixturesCommandlet.cpp")
	if err != nil {
		return nil, err
	}
	text := string(body)
	specRe := regexp.MustCompile(`MakeOperation(?:WithErrorContains)?\(TEXT\("([^"]+)"\)`)
	matches := specRe.FindAllStringSubmatch(text, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			names = append(names, match[1])
		}
	}

	notGenerated := map[string]struct{}{}
	start := regexp.MustCompile(`bool IsNotYetGeneratedOperation\(const FOperationFixtureSpec& Spec\)\s*\{`).FindStringIndex(text)
	if start != nil {
		rest := text[start[1]:]
		end := regexp.MustCompile(`\n\}`).FindStringIndex(rest)
		if end != nil {
			fnBody := rest[:end[0]]
			nameRe := regexp.MustCompile(`Spec\.Name == TEXT\("([^"]+)"\)`)
			for _, match := range nameRe.FindAllStringSubmatch(fnBody, -1) {
				if len(match) > 1 {
					notGenerated[match[1]] = struct{}{}
				}
			}
		}
	}

	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if _, blocked := notGenerated[name]; blocked {
			continue
		}
		filtered = append(filtered, name)
	}
	slices.Sort(filtered)
	return slices.Compact(filtered), nil
}
