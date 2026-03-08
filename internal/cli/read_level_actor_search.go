package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func runLevelActorSearch(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("level actor-search", stderr)
	opts := registerCommonFlags(fs)
	nameToken := fs.String("name", "", "actor object name search token")
	actorLabelToken := fs.String("actor-label", "", "actor label search token")
	actorClassToken := fs.String("actor-class", "", "actor class search token")
	limit := fs.Int("limit", 0, "optional result limit (0 = unlimited)")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: bpx level actor-search <file.umap> [--name <token>] [--actor-label <token>] [--actor-class <token>] [--limit <n>]")
		return 1
	}
	if *limit < 0 {
		fmt.Fprintln(stderr, "error: --limit must be >= 0")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	_, candidates, err := collectPersistentLevelChildren(asset)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	nameFilter := strings.TrimSpace(*nameToken)
	actorLabelFilter := strings.TrimSpace(*actorLabelToken)
	actorClassFilter := strings.TrimSpace(*actorClassToken)

	matches := make([]map[string]any, 0, 16)
	scanned := 0
	for _, idx := range candidates {
		scanned++
		exp := asset.Exports[idx]
		objectName := exp.ObjectName.Display(asset.Names)
		if nameFilter != "" && !containsFold(objectName, nameFilter) {
			continue
		}

		props := asset.ParseExportProperties(idx)
		actorLabel := levelActorLabelFromProperties(asset, props.Properties)
		actorClass := levelActorClassFromProperties(asset, exp, props.Properties)
		className := asset.ResolveClassName(exp)

		if actorLabelFilter != "" && !containsFold(actorLabel, actorLabelFilter) {
			continue
		}
		if actorClassFilter != "" &&
			!containsFold(className, actorClassFilter) &&
			!containsFold(actorClass, actorClassFilter) {
			continue
		}

		item := levelActorInfo(asset, idx, "search")
		item["actorLabel"] = actorLabel
		item["actorClass"] = actorClass
		if len(props.Warnings) > 0 {
			item["warnings"] = append([]string(nil), props.Warnings...)
		}

		matches = append(matches, item)
		if *limit > 0 && len(matches) >= *limit {
			break
		}
	}

	return printJSON(stdout, map[string]any{
		"file":             file,
		"nameFilter":       nameFilter,
		"actorLabelFilter": actorLabelFilter,
		"actorClassFilter": actorClassFilter,
		"limit":            *limit,
		"candidateCount":   len(candidates),
		"scannedCount":     scanned,
		"matchCount":       len(matches),
		"matches":          matches,
	})
}

func levelActorLabelFromProperties(asset *uasset.Asset, props []uasset.PropertyTag) string {
	if asset == nil {
		return ""
	}
	for _, p := range props {
		if !strings.EqualFold(p.Name.Display(asset.Names), "ActorLabel") {
			continue
		}
		decoded, ok := asset.DecodePropertyValue(p)
		if !ok {
			continue
		}
		if label := firstDecodedString(decoded); label != "" {
			return label
		}
	}
	return ""
}

func levelActorClassFromProperties(asset *uasset.Asset, exp uasset.ExportEntry, props []uasset.PropertyTag) string {
	if asset == nil {
		return ""
	}
	className := strings.TrimSpace(asset.ResolveClassName(exp))
	for _, p := range props {
		name := p.Name.Display(asset.Names)
		if !strings.EqualFold(name, "ActorClass") && !strings.EqualFold(name, "NativeClass") {
			continue
		}
		decoded, ok := asset.DecodePropertyValue(p)
		if !ok {
			continue
		}
		if classValue := firstDecodedString(decoded); classValue != "" {
			return classValue
		}
	}
	return className
}

func firstDecodedString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		if packageName, ok := t["packageName"].(string); ok {
			packageName = strings.TrimSpace(packageName)
			assetName, _ := t["assetName"].(string)
			assetName = strings.TrimSpace(assetName)
			subPath, _ := t["subPath"].(string)
			subPath = strings.TrimSpace(subPath)
			out := packageName
			if assetName != "" {
				if out == "" {
					out = assetName
				} else {
					out = out + "." + assetName
				}
			}
			if subPath != "" {
				if out == "" {
					out = subPath
				} else {
					out = out + ":" + subPath
				}
			}
			if out != "" {
				return out
			}
		}
		for _, key := range []string{"displayString", "sourceString", "cultureInvariantString", "resolved", "name", "value", "assetName", "packageName"} {
			if inner, ok := t[key]; ok {
				s := firstDecodedString(inner)
				if s != "" && !strings.EqualFold(s, "null") {
					return s
				}
			}
		}
	case []any:
		for _, item := range t {
			if s := firstDecodedString(item); s != "" {
				return s
			}
		}
	}
	return ""
}
