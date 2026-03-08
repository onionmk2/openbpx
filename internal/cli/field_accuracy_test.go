package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type fieldAccuracySpec struct {
	Oracle         string   `json:"oracle"`
	Name           string   `json:"name"`
	Argv           []string `json:"argv"`
	Expected       any      `json:"expected"`
	ExpectedCode   int      `json:"expectedCode,omitempty"`
	ExpectedStderr string   `json:"expectedStderr,omitempty"`
}

const expectedFixtureOracle = "ue-fixture"
const canonicalGoldenRootForExpectedOutput = "testdata/golden"

func TestFieldAccuracy(t *testing.T) {
	roots := goldenFixtureRoots(t, "expected_output")
	if len(roots) == 0 {
		t.Fatalf("no expected_output fixture roots found")
	}

	for _, root := range roots {
		root := root
		t.Run(filepath.Base(root), func(t *testing.T) {
			goldenRootForRun, err := goldenRootPathFromFixtureRoot(root)
			if err != nil {
				t.Fatalf("resolve golden root path: %v", err)
			}

			expectedDir := filepath.Join(root, "expected_output")
			entries, err := os.ReadDir(expectedDir)
			if err != nil {
				t.Fatalf("read expected_output dir: %v", err)
			}

			cases := make([]string, 0, len(entries))
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
					cases = append(cases, filepath.Join(expectedDir, entry.Name()))
				}
			}
			sort.Strings(cases)
			if len(cases) == 0 {
				t.Fatalf("no expected output fixtures found in %s", expectedDir)
			}

			for _, fixturePath := range cases {
				fixturePath := fixturePath
				t.Run(filepath.Base(fixturePath), func(t *testing.T) {
					payload, err := os.ReadFile(fixturePath)
					if err != nil {
						t.Fatalf("read fixture: %v", err)
					}

					var spec fieldAccuracySpec
					if err := json.Unmarshal(payload, &spec); err != nil {
						t.Fatalf("parse fixture json: %v", err)
					}
					if spec.Oracle != expectedFixtureOracle {
						t.Fatalf(
							"invalid fixture oracle (got=%q want=%q): expected_output fixtures must be UE-verified, not self-generated",
							spec.Oracle,
							expectedFixtureOracle,
						)
					}
					spec = rebaseFieldAccuracySpec(spec, canonicalGoldenRootForExpectedOutput, goldenRootForRun)
					if len(spec.Argv) == 0 {
						t.Fatalf("argv must not be empty")
					}

					var stdout bytes.Buffer
					var stderr bytes.Buffer
					code := runFromRepoRoot(t, spec.Argv, &stdout, &stderr)
					if code != spec.ExpectedCode {
						t.Fatalf("command exit code mismatch (got=%d want=%d): argv=%v stderr=%s", code, spec.ExpectedCode, spec.Argv, stderr.String())
					}
					if spec.ExpectedCode != 0 {
						gotStderr := strings.TrimSpace(stderr.String())
						if gotStderr != spec.ExpectedStderr {
							t.Fatalf("stderr mismatch\nargv=%v\nexpected=%q\nactual=%q", spec.Argv, spec.ExpectedStderr, gotStderr)
						}
						return
					}

					var actual any
					if err := json.Unmarshal(stdout.Bytes(), &actual); err != nil {
						t.Fatalf("parse command json output: %v\nstdout=%s", err, stdout.String())
					}

					if !reflect.DeepEqual(actual, spec.Expected) {
						expectedJSON, _ := json.MarshalIndent(spec.Expected, "", "  ")
						actualJSON, _ := json.MarshalIndent(actual, "", "  ")
						t.Fatalf("field accuracy mismatch\nargv=%v\nexpected=%s\nactual=%s", spec.Argv, string(expectedJSON), string(actualJSON))
					}
				})
			}
		})
	}
}

func runFromRepoRoot(t *testing.T, argv []string, stdout, stderr *bytes.Buffer) int {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	return Run(argv, stdout, stderr)
}

func goldenRootPathFromFixtureRoot(root string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}

	rootAbs := root
	if !filepath.IsAbs(rootAbs) {
		rootAbs = filepath.Join(cwd, rootAbs)
	}
	rootAbs = filepath.Clean(rootAbs)

	rel, err := filepath.Rel(repoRoot, rootAbs)
	if err != nil {
		return "", fmt.Errorf("rel path: %w", err)
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return "", fmt.Errorf("fixture root escapes repository: %s", rel)
	}
	return rel, nil
}

func rebaseFieldAccuracySpec(spec fieldAccuracySpec, oldRoot, newRoot string) fieldAccuracySpec {
	if oldRoot == newRoot {
		return spec
	}

	spec.Argv = rewritePathSlice(spec.Argv, oldRoot, newRoot)
	spec.ExpectedStderr = rewritePathPrefix(spec.ExpectedStderr, oldRoot, newRoot)
	spec.Expected = rewriteAnyPathPrefix(spec.Expected, oldRoot, newRoot)
	return spec
}

func rewriteAnyPathPrefix(v any, oldRoot, newRoot string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			rebasedKey := rewritePathPrefix(k, oldRoot, newRoot)
			out[rebasedKey] = rewriteAnyPathPrefix(child, oldRoot, newRoot)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			out[i] = rewriteAnyPathPrefix(child, oldRoot, newRoot)
		}
		return out
	case string:
		return rewritePathPrefix(x, oldRoot, newRoot)
	default:
		return v
	}
}

func rewritePathSlice(values []string, oldRoot, newRoot string) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = rewritePathPrefix(values[i], oldRoot, newRoot)
	}
	return out
}

func rewritePathPrefix(value, oldRoot, newRoot string) string {
	oldClean := filepath.ToSlash(filepath.Clean(oldRoot))
	newClean := filepath.ToSlash(filepath.Clean(newRoot))
	if oldClean == newClean {
		return value
	}

	normalized := filepath.ToSlash(value)
	if normalized == oldClean {
		return newClean
	}
	oldPrefix := oldClean + "/"
	if strings.HasPrefix(normalized, oldPrefix) {
		return newClean + normalized[len(oldClean):]
	}
	return value
}
