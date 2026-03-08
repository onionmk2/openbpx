package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type generateSkillsResponse struct {
	OutputDir      string   `json:"outputDir"`
	Filter         string   `json:"filter"`
	GeneratedCount int      `json:"generatedCount"`
	Skills         []string `json:"skills"`
}

func TestRunGenerateSkillsWritesSkillFiles(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "skills-out")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"generate-skills", "--output-dir", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d want 0 (stderr=%s)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}

	var resp generateSkillsResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, stdout.String())
	}
	if resp.GeneratedCount == 0 {
		t.Fatalf("generatedCount must be > 0")
	}
	if resp.OutputDir != filepath.Clean(outDir) {
		t.Fatalf("outputDir: got %q want %q", resp.OutputDir, filepath.Clean(outDir))
	}

	requiredSkills := []string{"bpx-shared", "bpx-prop", "bpx-blueprint", "bpx-validate"}
	for _, skill := range requiredSkills {
		if _, err := os.Stat(filepath.Join(outDir, skill, "SKILL.md")); err != nil {
			t.Fatalf("missing generated file for %s: %v", skill, err)
		}
	}

	if _, err := os.Stat(filepath.Join(outDir, "bpx-generate-skills", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("bpx-generate-skills must not be generated")
	}

	propSkillPath := filepath.Join(outDir, "bpx-prop", "SKILL.md")
	propBody, err := os.ReadFile(propSkillPath)
	if err != nil {
		t.Fatalf("read generated skill: %v", err)
	}
	propText := string(propBody)
	if !strings.Contains(propText, "name: bpx-prop") {
		t.Fatalf("missing frontmatter name in generated prop skill")
	}
	if !strings.Contains(propText, "bpx prop list <file.uasset> --export <n>") {
		t.Fatalf("missing usage line in generated prop skill")
	}
	if !strings.Contains(propText, "## Behavior") {
		t.Fatalf("missing behavior section in generated prop skill")
	}
	if !strings.Contains(propText, "This command includes write-capable operations.") {
		t.Fatalf("missing write caution in generated prop skill")
	}

	blueprintSkillPath := filepath.Join(outDir, "bpx-blueprint", "SKILL.md")
	blueprintBody, err := os.ReadFile(blueprintSkillPath)
	if err != nil {
		t.Fatalf("read generated blueprint skill: %v", err)
	}
	blueprintText := string(blueprintBody)
	if !strings.Contains(blueprintText, "## Command Matrix") {
		t.Fatalf("missing embedded supplement section in generated blueprint skill")
	}
	if !strings.Contains(blueprintText, "## Code-Aligned Caveats") {
		t.Fatalf("missing embedded supplement caveats in generated blueprint skill")
	}

	sharedSkillPath := filepath.Join(outDir, "bpx-shared", "SKILL.md")
	sharedBody, err := os.ReadFile(sharedSkillPath)
	if err != nil {
		t.Fatalf("read generated shared skill: %v", err)
	}
	sharedText := string(sharedBody)
	if !strings.Contains(sharedText, "## Global Rules") {
		t.Fatalf("missing embedded supplement section in generated shared skill")
	}
}

func TestRunGenerateSkillsFilter(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "skills-out")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"generate-skills", "--output-dir", outDir, "--filter", "blueprint"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d want 0 (stderr=%s)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}

	var resp generateSkillsResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, stdout.String())
	}
	if resp.Filter != "blueprint" {
		t.Fatalf("filter: got %q want %q", resp.Filter, "blueprint")
	}
	if resp.GeneratedCount != 1 {
		t.Fatalf("generatedCount: got %d want 1", resp.GeneratedCount)
	}
	if len(resp.Skills) != 1 || resp.Skills[0] != "bpx-blueprint" {
		t.Fatalf("skills: got %#v want [\"bpx-blueprint\"]", resp.Skills)
	}
	if _, err := os.Stat(filepath.Join(outDir, "bpx-blueprint", "SKILL.md")); err != nil {
		t.Fatalf("missing filtered skill file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "bpx-shared", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("bpx-shared should not be generated for blueprint-only filter")
	}
}

func TestRunGenerateSkillsRejectsPositionalArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"generate-skills", "extra"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code: got %d want 1", code)
	}
	if !strings.Contains(stderr.String(), "usage: bpx generate-skills [--output-dir <dir>] [--filter <token>]") {
		t.Fatalf("expected usage error, got: %s", stderr.String())
	}
}

func TestResolveSkillOutputDirExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}
	got, err := resolveSkillOutputDir("~")
	if err != nil {
		t.Fatalf("resolve output dir: %v", err)
	}
	if got != filepath.Clean(home) {
		t.Fatalf("resolved dir: got %q want %q", got, filepath.Clean(home))
	}
}

func TestRunGenerateSkillsRejectsFileOutputPath(t *testing.T) {
	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "not-a-dir")
	if err := os.WriteFile(outputFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"generate-skills", "--output-dir", outputFile}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code: got %d want 1", code)
	}
	if !strings.Contains(stderr.String(), "--output-dir is not a directory") {
		t.Fatalf("expected directory validation error, got: %s", stderr.String())
	}
}
