package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type generatedSkill struct {
	Name        string
	Description string
	Content     string
}

func runGenerateSkills(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("generate-skills", stderr)
	outputDir := fs.String("output-dir", "skills", "output directory for generated skills")
	filter := fs.String("filter", "", "optional substring filter for skill name/description")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: bpx generate-skills [--output-dir <dir>] [--filter <token>]")
		return 1
	}

	root, err := resolveSkillOutputDir(*outputDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	specs := buildGeneratedSkills(*filter)
	generated := make([]string, 0, len(specs))
	for _, spec := range specs {
		dir := filepath.Join(root, spec.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(stderr, "error: create skill directory %s: %v\n", dir, err)
			return 1
		}
		path := filepath.Join(dir, "SKILL.md")
		if err := writeFileAtomically(path, []byte(spec.Content), 0o644); err != nil {
			fmt.Fprintf(stderr, "error: write %s: %v\n", path, err)
			return 1
		}
		generated = append(generated, spec.Name)
	}

	resp := map[string]any{
		"outputDir":      root,
		"filter":         strings.TrimSpace(*filter),
		"generatedCount": len(generated),
		"skills":         generated,
	}
	return printJSON(stdout, resp)
}

func resolveSkillOutputDir(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("--output-dir must not be empty")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if trimmed == "~" {
			trimmed = home
		} else {
			trimmed = filepath.Join(home, trimmed[2:])
		}
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	switch {
	case err == nil:
		if !info.IsDir() {
			return "", fmt.Errorf("--output-dir is not a directory: %s", abs)
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", fmt.Errorf("create output directory: %w", err)
		}
	default:
		return "", fmt.Errorf("stat output directory: %w", err)
	}
	return abs, nil
}

func buildGeneratedSkills(filter string) []generatedSkill {
	matches := skillFilterMatcher(filter)

	skills := make([]generatedSkill, 0, 32)
	shared := generatedSharedSkill()
	shared.Content = mergeGeneratedSkillWithSupplement(shared.Name, shared.Content)
	if matches(shared.Name, shared.Description) {
		skills = append(skills, shared)
	}

	for _, topic := range orderedHelpTopics() {
		if topic == "generate-skills" {
			continue
		}
		usage := usageLinesForTopic(topic)
		if len(usage) == 0 {
			continue
		}
		summary := helpTopicSummary(topic)
		if summary == "" {
			summary = "BPX command skill."
		}
		name := "bpx-" + topic
		desc := fmt.Sprintf("BPX `%s` command skill. %s", topic, summary)
		if !matches(name, desc) {
			continue
		}
		skill := generatedTopicSkill(name, topic, desc, usage, helpTopicBehaviorLines(topic), topicHasWriteCommands(topic))
		skill.Content = mergeGeneratedSkillWithSupplement(skill.Name, skill.Content)
		skills = append(skills, skill)
	}

	sort.SliceStable(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills
}

func skillFilterMatcher(filter string) func(name, description string) bool {
	needle := strings.ToLower(strings.TrimSpace(filter))
	return func(name, description string) bool {
		if needle == "" {
			return true
		}
		name = strings.ToLower(name)
		description = strings.ToLower(description)
		return strings.Contains(name, needle) || strings.Contains(description, needle)
	}
}

func orderedHelpTopics() []string {
	seen := map[string]struct{}{}
	topics := make([]string, 0, 32)
	for _, category := range helpCatalog() {
		for _, line := range category.Lines {
			topic := usageTopicFromLine(line)
			if topic == "" {
				continue
			}
			if _, ok := seen[topic]; ok {
				continue
			}
			seen[topic] = struct{}{}
			topics = append(topics, topic)
		}
	}
	return topics
}

func generatedSharedSkill() generatedSkill {
	description := "Shared BPX safety and execution guidance. Use before command-specific BPX skills."
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: bpx-shared\n")
	b.WriteString("description: " + description + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# bpx shared\n\n")
	b.WriteString("## Installation\n\n")
	b.WriteString("- Ensure `bpx` is available on `PATH`.\n")
	b.WriteString("- Confirm with `bpx version` and `bpx help`.\n\n")
	b.WriteString("## Safety Rules\n\n")
	b.WriteString("- Treat assets as untrusted binary input.\n")
	b.WriteString("- Prefer read commands before write commands.\n")
	b.WriteString("- Run `--dry-run` first for write-capable commands.\n")
	b.WriteString("- Use `--backup` when writing files in place.\n\n")
	b.WriteString("## Standard Workflow\n\n")
	b.WriteString("1. Inspect command shape with `bpx help <command>`.\n")
	b.WriteString("2. Identify exact targets using read commands.\n")
	b.WriteString("3. Run write command with `--dry-run`.\n")
	b.WriteString("4. Execute real write with `--backup` when approved.\n")
	b.WriteString("5. Validate with `bpx validate <file> --binary-equality` as needed.\n")
	return generatedSkill{
		Name:        "bpx-shared",
		Description: description,
		Content:     b.String(),
	}
}

func generatedTopicSkill(name, topic, description string, usageLines, behaviorLines []string, hasWrites bool) generatedSkill {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("description: " + description + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + topic + "\n\n")
	b.WriteString("> **PREREQUISITE:** Read [bpx-shared](../bpx-shared/SKILL.md).\n\n")
	b.WriteString("## Usage\n\n")
	b.WriteString("```bash\n")
	for _, line := range usageLines {
		b.WriteString(line + "\n")
	}
	b.WriteString("```\n")
	if len(behaviorLines) > 0 {
		b.WriteString("\n## Behavior\n\n")
		for _, line := range behaviorLines {
			b.WriteString("- " + line + "\n")
		}
	}
	if hasWrites {
		b.WriteString("\n> [!CAUTION]\n")
		b.WriteString("> This command includes write-capable operations. Confirm intent and run `--dry-run` first.\n")
	}
	return generatedSkill{
		Name:        name,
		Description: description,
		Content:     b.String(),
	}
}
