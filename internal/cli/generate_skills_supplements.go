package cli

import (
	"fmt"
	"strings"
)

type markdownSection struct {
	Heading string
	Body    string
}

type generatedSkillSupplement struct {
	Sections []markdownSection
	Caution  string
}

func mergeGeneratedSkillWithSupplement(name, base string) string {
	base = strings.TrimRight(base, "\n")
	supplement := buildGeneratedSkillSupplement(name)
	if len(supplement.Sections) == 0 && strings.TrimSpace(supplement.Caution) == "" {
		return base + "\n"
	}

	seen := map[string]struct{}{}
	for _, heading := range sectionHeadings(base) {
		seen[normalizeHeading(heading)] = struct{}{}
	}

	var b strings.Builder
	b.WriteString(base)
	for _, section := range supplement.Sections {
		heading := strings.TrimSpace(section.Heading)
		body := strings.TrimSpace(section.Body)
		if heading == "" || body == "" {
			continue
		}
		key := normalizeHeading(heading)
		if _, exists := seen[key]; exists {
			continue
		}
		b.WriteString("\n\n## ")
		b.WriteString(heading)
		b.WriteString("\n\n")
		b.WriteString(body)
		seen[key] = struct{}{}
	}

	if strings.TrimSpace(supplement.Caution) != "" && !strings.Contains(strings.ToLower(base), "[!caution]") {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(supplement.Caution))
	}

	b.WriteString("\n")
	return b.String()
}

func buildGeneratedSkillSupplement(name string) generatedSkillSupplement {
	if name == "bpx-shared" {
		return generatedSkillSupplement{
			Sections: []markdownSection{
				{
					Heading: "Global Rules",
					Body: strings.Join([]string{
						"- Prefer read commands before write commands.",
						"- Use `--dry-run` first for write-capable commands.",
						"- Use `--backup` for in-place updates unless explicitly declined.",
						"- For automation, prefer `--format toml` where available.",
					}, "\n"),
				},
				{
					Heading: "Command Selection Heuristics",
					Body: strings.Join([]string{
						"- Package shape/version checks: `info`, `dump`, `validate`.",
						"- Export/import and reference analysis: `export`, `import`, `ref`, `raw`.",
						"- Gameplay/content edits: `prop`, `var`, `datatable`, `localization`, `stringtable`, `level`.",
						"- Blueprint analysis workflow: `blueprint info` -> `blueprint disasm` -> `blueprint trace/search`.",
					}, "\n"),
				},
				{
					Heading: "Output Reading Tips",
					Body: strings.Join([]string{
						"- Treat warnings as actionable signals; they often indicate partial decode paths.",
						"- For write responses, inspect changed-byte and size-delta fields before applying.",
						"- On errors, re-run `bpx help <command>` to confirm required flags and accepted forms.",
					}, "\n"),
				},
			},
		}
	}

	topic := strings.TrimPrefix(name, "bpx-")
	if topic == "" || topic == name {
		return generatedSkillSupplement{}
	}
	usage := usageLinesForTopic(topic)
	behavior := helpTopicBehaviorLines(topic)
	sections := make([]markdownSection, 0, 3)

	if matrix := renderCommandMatrix(topic, usage, behavior); matrix != "" {
		sections = append(sections, markdownSection{
			Heading: "Command Matrix",
			Body:    matrix,
		})
	}
	if caveats := renderSkillCaveats(topic); caveats != "" {
		sections = append(sections, markdownSection{
			Heading: "Code-Aligned Caveats",
			Body:    caveats,
		})
	}
	if examples := renderExampleCommands(usage); examples != "" {
		sections = append(sections, markdownSection{
			Heading: "High-Signal Examples",
			Body:    examples,
		})
	}
	return generatedSkillSupplement{Sections: sections}
}

func renderCommandMatrix(topic string, usageLines, behaviorLines []string) string {
	type row struct {
		Command string
		When    string
		Notes   string
	}
	rows := make([]row, 0, len(usageLines))
	seen := map[string]struct{}{}
	for _, usage := range usageLines {
		variant := usageVariant(topic, usage)
		if variant == "" {
			variant = topic
		}
		if _, ok := seen[variant]; ok {
			continue
		}
		seen[variant] = struct{}{}

		when := behaviorSummaryForVariant(variant, behaviorLines)
		if when == "" {
			when = "Use this command when this operation matches the target workflow."
		}
		rows = append(rows, row{
			Command: "`" + variant + "`",
			When:    when,
			Notes:   defaultNotesForVariant(variant),
		})
	}
	if len(rows) <= 1 {
		return ""
	}

	var b strings.Builder
	b.WriteString("| Command | Use when | Notable defaults |\n")
	b.WriteString("|------|------|------|\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.Command, sanitizeTableCell(r.When), sanitizeTableCell(r.Notes)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func usageVariant(topic, usage string) string {
	fields := strings.Fields(usage)
	if len(fields) < 2 {
		return ""
	}
	// Expect: bpx <topic> [subcommand] ...
	if fields[0] != "bpx" || fields[1] != topic {
		return ""
	}
	if len(fields) < 3 {
		return topic
	}
	candidate := fields[2]
	if strings.HasPrefix(candidate, "<") || strings.HasPrefix(candidate, "[") || strings.HasPrefix(candidate, "--") {
		return topic
	}
	return candidate
}

func behaviorSummaryForVariant(variant string, behaviorLines []string) string {
	prefixes := []string{
		fmt.Sprintf("`%s`:", variant),
		fmt.Sprintf("`%s` ", variant),
	}
	for _, line := range behaviorLines {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range prefixes {
			if strings.HasPrefix(trimmed, prefix) {
				out := strings.TrimPrefix(trimmed, fmt.Sprintf("`%s`:", variant))
				out = strings.TrimPrefix(out, fmt.Sprintf("`%s`", variant))
				out = strings.TrimSpace(strings.TrimPrefix(out, ":"))
				out = strings.TrimSuffix(out, ".")
				if out != "" {
					return out + "."
				}
			}
		}
	}
	if len(behaviorLines) > 0 {
		return strings.TrimSuffix(strings.TrimSpace(behaviorLines[0]), ".") + "."
	}
	return ""
}

func defaultNotesForVariant(variant string) string {
	switch variant {
	case "read", "list", "info", "summary":
		return "Read-only path; safe for discovery."
	case "set", "add", "remove", "rename", "rewrite", "update-row", "add-row", "remove-row", "set-header", "set-flags", "set-source", "set-id", "set-stringtable-ref", "rekey", "rewrite-namespace", "var-set", "write-entry", "write-value":
		return "Run `--dry-run` first and use `--backup` for real writes."
	default:
		return "Check `bpx help` for exact required flags."
	}
}

func renderSkillCaveats(topic string) string {
	caveats := map[string][]string{
		"find": {
			"`find summary` continues across parse failures; inspect `parseFailures` before deciding next steps.",
			"For map-only scans, use `--pattern \"*.umap\"`.",
		},
		"import": {
			"`graph` is ImportMap-based and may not reflect K2 graph-level references.",
			"Large directory scans should use `--filter` and narrower patterns for speed.",
		},
		"prop": {
			"Write paths mutate one export only; invalid dot paths fail explicitly.",
			"Prefer `prop list` immediately before `prop set/add/remove` to avoid stale assumptions.",
		},
		"package": {
			"`set-flags` blocks unsupported/safety-critical toggles by design.",
			"`resolve-index` is the safest way to interpret signed `FPackageIndex` in automation flows.",
		},
		"localization": {
			"`resolve` is read-preview oriented; it does not mutate assets.",
			"Bulk namespace/key rewrites should always be previewed first in a narrowed scope.",
		},
		"datatable": {
			"Row mutations target DataTable exports only; non-DataTable types are rejected.",
			"When using CSV/TSV output, confirm flattened values before patching rows.",
		},
		"blueprint": {
			"Large blueprints can produce very large payloads; constrain via `--limit`/`--max-steps`.",
			"`refs --include-routes` can be expensive; disable routes when doing broad scans.",
			"`disasm --entrypoint` implies analysis-oriented output.",
		},
		"level": {
			"`--actor` resolution supports name, `PersistentLevel.<Name>`, or export index.",
			"`var-set` uses property path semantics; use `var-list` to validate target paths first.",
		},
		"material": {
			"`material read` is the canonical entry; use flags to opt into HLSL and child scans.",
			"Directory scans should always narrow with `--parent`, `--pattern`, and `--limit`.",
		},
		"metadata": {
			"There is no `metadata read` subcommand; read form is `bpx metadata <file> --export <n>`.",
			"Object metadata updates require a valid `--import` target.",
		},
		"validate": {
			"Exit code `2` indicates a non-OK validation result (not a transport/runtime failure).",
			"`--binary-equality` is the strongest no-op round-trip safety check.",
		},
	}
	lines := caveats[topic]
	if len(lines) == 0 {
		return ""
	}
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		formatted = append(formatted, "- "+line)
	}
	return strings.Join(formatted, "\n")
}

func renderExampleCommands(usageLines []string) string {
	if len(usageLines) == 0 {
		return ""
	}
	limit := 4
	if len(usageLines) < limit {
		limit = len(usageLines)
	}
	var b strings.Builder
	b.WriteString("```bash\n")
	for i := 0; i < limit; i++ {
		b.WriteString(exampleFromUsage(usageLines[i]))
		b.WriteByte('\n')
	}
	b.WriteString("```")
	return b.String()
}

func exampleFromUsage(usage string) string {
	replacer := strings.NewReplacer(
		"<file.uasset>", "./Sample.uasset",
		"<file.umap>", "./Sample.umap",
		"<directory>", "./Content",
		"<n>", "1",
		"<i>", "1",
		"<k>", "SampleKey",
		"<v>", "SampleValue",
		"<ns>", "Game",
		"<token>", "SampleToken",
		"<name>", "SampleName",
		"<section>", "Names",
		"<dot.path>", "MyProperty",
		"<path>", "MyProperty",
		"<culture>", "en",
		"<table-id>", "ST_Game",
		"<enum-or-raw>", "PKG_ContainsMap",
		"<new.uasset>", "./Sample.out.uasset",
		"<old>", "OldValue",
		"<new>", "NewValue",
		`'<json>'`, "'{\"value\":1}'",
		"<json>", `{"value":1}`,
		"<regex>", ".*",
		"<vm>", "0",
	)
	return replacer.Replace(usage)
}

func sanitizeTableCell(in string) string {
	s := strings.TrimSpace(in)
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

func sectionHeadings(markdown string) []string {
	lines := strings.Split(markdown, "\n")
	headings := make([]string, 0, 8)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			headings = append(headings, strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
		}
	}
	return headings
}

func normalizeHeading(heading string) string {
	return strings.ToLower(strings.TrimSpace(heading))
}
