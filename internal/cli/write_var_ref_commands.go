package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/wilddogjp/openbpx/pkg/edit"
	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func runVarRename(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("var rename", stderr)
	opts := registerCommonFlags(fs)
	from := fs.String("from", "", "old variable name")
	to := fs.String("to", "", "new variable name")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		fmt.Fprintln(stderr, "usage: bpx var rename <file.uasset> --from <old> --to <new> [--dry-run] [--backup]")
		return 1
	}

	fromName := strings.TrimSpace(*from)
	toName := strings.TrimSpace(*to)
	if fromName == toName {
		fmt.Fprintln(stderr, "error: --from and --to must differ")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	declared, declWarnings := collectDeclaredVariables(asset)
	declaredFrom := false
	declaredType := ""
	if _, ok := declared[fromName]; ok {
		declaredFrom = true
		declaredType = declared[fromName]
	}
	if !declaredFrom {
		fmt.Fprintf(stderr, "error: declaration for %q was not found; refusing NameMap-only rename\n", fromName)
		return 1
	}
	if _, ok := declared[toName]; ok {
		fmt.Fprintf(stderr, "error: destination variable already exists in declarations: %s\n", toName)
		return 1
	}

	renameIndexes := make([]int, 0, 4)
	removedNames := make([]uasset.NameEntry, 0, len(asset.Names))
	for i := range asset.Names {
		if asset.Names[i].Value != fromName {
			removedNames = append(removedNames, asset.Names[i])
			continue
		}
		renameIndexes = append(renameIndexes, i)
	}
	if len(renameIndexes) == 0 {
		fmt.Fprintf(stderr, "error: variable name %q not found in NameMap\n", fromName)
		return 1
	}
	nonCaseHash, casePreservingHash := edit.ComputeNameEntryHashesUE56(toName)
	insertPos := len(removedNames)
	for i := range removedNames {
		if compareNameMapNamesFold(toName, removedNames[i].Value) < 0 {
			insertPos = i
			break
		}
	}
	updatedNames := make([]uasset.NameEntry, 0, len(asset.Names))
	updatedNames = append(updatedNames, removedNames[:insertPos]...)
	updatedNames = append(updatedNames, uasset.NameEntry{
		Value:              toName,
		NonCaseHash:        nonCaseHash,
		CasePreservingHash: casePreservingHash,
	})
	updatedNames = append(updatedNames, removedNames[insertPos:]...)

	outBytes, err := edit.RewriteNameMap(asset, updatedNames)
	if err != nil {
		fmt.Fprintf(stderr, "error: rewrite name map: %v\n", err)
		return 1
	}
	updatedAsset, err := uasset.ParseBytes(outBytes, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: reparse renamed asset: %v\n", err)
		return 1
	}
	indexRemap, err := edit.BuildNameIndexRemap(asset.Names, updatedAsset.Names)
	if err != nil {
		fmt.Fprintf(stderr, "error: build name remap: %v\n", err)
		return 1
	}
	outBytes, err = edit.RewriteImportExportNameRefs(updatedAsset, indexRemap)
	if err != nil {
		fmt.Fprintf(stderr, "error: rewrite import/export name refs: %v\n", err)
		return 1
	}
	updatedAsset, err = uasset.ParseBytes(outBytes, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: reparse remapped asset: %v\n", err)
		return 1
	}
	isBoolVar := strings.EqualFold(strings.TrimSpace(declaredType), "BoolProperty")
	displayFrom := blueprintNameToDisplayString(fromName, isBoolVar)
	displayTo := blueprintNameToDisplayString(toName, isBoolVar)
	exportMutations, err := edit.BuildExportNameRemapMutations(asset, updatedAsset, indexRemap, displayFrom, displayTo)
	if err != nil {
		fmt.Fprintf(stderr, "error: rewrite export payload name refs: %v\n", err)
		return 1
	}
	if len(exportMutations) > 0 {
		outBytes, err = edit.RewriteAsset(updatedAsset, exportMutations)
		if err != nil {
			fmt.Fprintf(stderr, "error: rewrite export payloads: %v\n", err)
			return 1
		}
		updatedAsset, err = uasset.ParseBytes(outBytes, *opts)
		if err != nil {
			fmt.Fprintf(stderr, "error: reparse export-rewritten asset: %v\n", err)
			return 1
		}
	}
	outBytes, _, err = rewriteAssetRegistryVarRename(updatedAsset, fromName, toName, isBoolVar)
	if err != nil {
		fmt.Fprintf(stderr, "error: rewrite asset registry search data: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":           file,
		"from":           fromName,
		"to":             toName,
		"renamedIndexes": renameIndexes,
		"declaredCount":  len(declared),
		"dryRun":         *dryRun,
		"changed":        changed,
		"warnings":       declWarnings,
		"outputBytes":    len(outBytes),
	}
	if *dryRun {
		return printJSON(stdout, resp)
	}
	if *backup {
		if err := createBackupFile(file); err != nil {
			fmt.Fprintf(stderr, "error: backup source file: %v\n", err)
			return 1
		}
	}
	if err := os.WriteFile(file, outBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "error: write file: %v\n", err)
		return 1
	}
	return printJSON(stdout, resp)
}

func rewriteAssetRegistryVarRename(asset *uasset.Asset, fromName, toName string, isBoolVar bool) ([]byte, int, error) {
	fromDisplay := blueprintNameToDisplayString(fromName, isBoolVar)
	toDisplay := blueprintNameToDisplayString(toName, isBoolVar)
	if fromDisplay == "" || fromDisplay == toDisplay {
		return append([]byte(nil), asset.Raw.Bytes...), 0, nil
	}
	return rewriteAssetRegistryTextValues(asset, func(history map[string]any) int {
		return replaceHistoryStrings(history, fromDisplay, toDisplay)
	})
}

var blueprintDisplayNameArticles = []string{
	"In",
	"As",
	"To",
	"Or",
	"At",
	"On",
	"If",
	"Be",
	"By",
	"The",
	"For",
	"And",
	"With",
	"When",
	"From",
}

func blueprintNameToDisplayString(name string, isBool bool) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	runes := []rune(name)
	out := make([]rune, 0, len(runes)+4)
	inRun := false
	wasSpace := false
	wasOpenParen := false
	wasNumber := false
	wasMinusSign := false

	for i, r := range runes {
		isLower := unicode.IsLower(r)
		isUpper := unicode.IsUpper(r)
		isDigit := unicode.IsDigit(r)
		isUnderscore := r == '_'

		if i == 0 && isBool && r == 'b' && len(runes) > 1 && unicode.IsUpper(runes[1]) {
			continue
		}

		if (isUpper || (isDigit && !wasMinusSign)) && !inRun && !wasOpenParen && !wasNumber {
			if !wasSpace && len(out) > 0 {
				out = append(out, ' ')
				wasSpace = true
			}
			inRun = true
		}

		if isLower {
			inRun = false
		}

		if isUnderscore {
			r = ' '
			inRun = true
		}

		if len(out) == 0 {
			r = unicode.ToUpper(r)
		} else if !isDigit && (wasSpace || wasOpenParen) {
			if shouldLowercaseDisplayWordStart(runes, i) {
				r = unicode.ToLower(r)
			} else {
				r = unicode.ToUpper(r)
			}
		}

		out = append(out, r)
		wasSpace = r == ' '
		wasOpenParen = r == '('
		wasMinusSign = r == '-'
		isPotentialNumerical := wasMinusSign || r == '.'
		wasNumber = isDigit || (wasNumber && isPotentialNumerical)
	}

	return string(out)
}

func compareNameMapNamesFold(a, b string) int {
	foldA := strings.ToLower(a)
	foldB := strings.ToLower(b)
	if diff := strings.Compare(foldA, foldB); diff != 0 {
		return diff
	}
	return strings.Compare(a, b)
}

func shouldLowercaseDisplayWordStart(runes []rune, start int) bool {
	for _, article := range blueprintDisplayNameArticles {
		if !displayWordHasExactPrefix(runes, start, article) {
			continue
		}
		next := start + len([]rune(article))
		if next < len(runes) && !unicode.IsLower(runes[next]) {
			return true
		}
	}
	return false
}

func displayWordHasExactPrefix(runes []rune, start int, word string) bool {
	wordRunes := []rune(word)
	if start+len(wordRunes) > len(runes) {
		return false
	}
	for i, want := range wordRunes {
		if runes[start+i] != want {
			return false
		}
	}
	return true
}

func runRef(args []string, stdout, stderr io.Writer) int {
	return dispatchSubcommand(
		args,
		stdout,
		stderr,
		"usage: bpx ref <rewrite> ...",
		"unknown ref command: %s\n",
		subcommandSpec{Name: "rewrite", Run: runRefRewrite},
	)
}

func runRefRewrite(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ref rewrite", stderr)
	opts := registerCommonFlags(fs)
	from := fs.String("from", "", "reference prefix/token to replace")
	to := fs.String("to", "", "replacement prefix/token")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		fmt.Fprintln(stderr, "usage: bpx ref rewrite <file.uasset> --from <old> --to <new> [--dry-run] [--backup]")
		return 1
	}

	fromRef := strings.TrimSpace(*from)
	toRef := strings.TrimSpace(*to)
	if fromRef == toRef {
		fmt.Fprintln(stderr, "error: --from and --to must differ")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	outBytes, nameRewrites, propReplacements, warnings, err := rewriteReferencesAsset(asset, *opts, fromRef, toRef)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":                 file,
		"from":                 fromRef,
		"to":                   toRef,
		"nameMapRewriteCount":  nameRewrites,
		"propertyReplaceCount": propReplacements,
		"dryRun":               *dryRun,
		"changed":              changed,
		"warnings":             warnings,
		"outputBytes":          len(outBytes),
	}
	if *dryRun {
		return printJSON(stdout, resp)
	}
	if *backup {
		if err := createBackupFile(file); err != nil {
			fmt.Fprintf(stderr, "error: backup source file: %v\n", err)
			return 1
		}
	}
	if err := os.WriteFile(file, outBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "error: write file: %v\n", err)
		return 1
	}
	return printJSON(stdout, resp)
}

func rewriteReferencesAsset(asset *uasset.Asset, opts uasset.ParseOptions, from, to string) ([]byte, int, int, []string, error) {
	if asset == nil {
		return nil, 0, 0, nil, fmt.Errorf("asset is nil")
	}

	workingAsset := asset
	var workingBytes []byte
	warnings := make([]string, 0, 8)

	nameMapRewrites := 0
	updatedNames := append(make([]uasset.NameEntry, 0, len(workingAsset.Names)), workingAsset.Names...)
	for i := range updatedNames {
		replaced, count := replaceAllWithCount(updatedNames[i].Value, from, to)
		if count == 0 {
			continue
		}
		nonCaseHash, casePreservingHash := edit.ComputeNameEntryHashesUE56(replaced)
		updatedNames[i] = uasset.NameEntry{
			Value:              replaced,
			NonCaseHash:        nonCaseHash,
			CasePreservingHash: casePreservingHash,
		}
		nameMapRewrites += count
	}
	if nameMapRewrites > 0 {
		var err error
		workingBytes, err = edit.RewriteNameMap(workingAsset, updatedNames)
		if err != nil {
			return nil, 0, 0, nil, fmt.Errorf("rewrite name map: %w", err)
		}
		workingAsset, err = uasset.ParseBytes(workingBytes, opts)
		if err != nil {
			return nil, 0, 0, nil, fmt.Errorf("reparse after name map rewrite: %w", err)
		}
	}

	propertyReplaceCount := 0
	for exportIdx := 0; exportIdx < len(workingAsset.Exports); exportIdx++ {
		for {
			props := workingAsset.ParseExportProperties(exportIdx)
			if len(props.Warnings) > 0 {
				return nil, 0, 0, nil, fmt.Errorf(
					"cannot safely rewrite references in export %d: %s",
					exportIdx+1,
					strings.Join(props.Warnings, "; "),
				)
			}
			mutated := false
			for _, p := range props.Properties {
				propName := p.Name.Display(workingAsset.Names)
				if strings.TrimSpace(propName) == "" || strings.EqualFold(propName, "None") {
					continue
				}

				decoded, ok := workingAsset.DecodePropertyValue(p)
				if !ok {
					continue
				}
				updated, replaceCount := rewriteReferencesInTypedValue(p.TypeString(workingAsset.Names), decoded, from, to)
				if replaceCount == 0 {
					continue
				}

				valueJSON, err := marshalJSONValue(updated)
				if err != nil {
					return nil, 0, 0, nil, fmt.Errorf("export %d %s: marshal rewritten value: %w", exportIdx+1, propName, err)
				}
				res, err := edit.BuildPropertySetMutation(workingAsset, exportIdx, propName, valueJSON)
				if err != nil {
					return nil, 0, 0, nil, fmt.Errorf("export %d %s: %w", exportIdx+1, propName, err)
				}

				workingBytes, err = edit.RewriteAsset(workingAsset, []edit.ExportMutation{res.Mutation})
				if err != nil {
					return nil, 0, 0, nil, fmt.Errorf("rewrite asset (export=%d property=%s): %w", exportIdx+1, propName, err)
				}
				workingAsset, err = uasset.ParseBytes(workingBytes, opts)
				if err != nil {
					return nil, 0, 0, nil, fmt.Errorf("reparse rewritten asset (export=%d property=%s): %w", exportIdx+1, propName, err)
				}
				propertyReplaceCount += replaceCount
				mutated = true
				break
			}
			if !mutated {
				break
			}
		}
	}

	assetRegistryBytes, _, err := rewriteAssetRegistryStringReplace(workingAsset, from, to)
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("rewrite asset registry search data: %w", err)
	}
	workingBytes = assetRegistryBytes
	return workingBytes, nameMapRewrites, propertyReplaceCount, warnings, nil
}

func rewriteReferencesInTypedValue(typeName string, value any, from, to string) (any, int) {
	rootType := propertyRootType(typeName)
	switch rootType {
	case "SoftObjectProperty", "SoftObjectPathProperty", "SoftClassPathProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		replacements := 0
		for _, field := range []string{"packageName", "assetName", "subPath"} {
			raw, ok := valueMap[field].(string)
			if !ok {
				continue
			}
			replaced, count := replaceAllWithCount(raw, from, to)
			if count == 0 {
				continue
			}
			out[field] = replaced
			replacements += count
		}
		if replacements == 0 {
			return value, 0
		}
		return out, replacements
	case "TextProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		replacements := 0
		for _, field := range []string{"namespace", "key", "sourceString", "cultureInvariantString", "tableIdName"} {
			raw, ok := valueMap[field].(string)
			if !ok {
				continue
			}
			replaced, count := replaceAllWithCount(raw, from, to)
			if count == 0 {
				continue
			}
			out[field] = replaced
			replacements += count
		}
		if tableID, ok := valueMap["tableId"].(map[string]any); ok {
			if tableName, ok := tableID["name"].(string); ok {
				replaced, count := replaceAllWithCount(tableName, from, to)
				if count > 0 {
					tableOut := cloneAnyMapLocal(tableID)
					tableOut["name"] = replaced
					out["tableId"] = tableOut
					out["tableIdName"] = replaced
					replacements += count
				}
			}
		}
		if nested, ok := valueMap["formatText"].(map[string]any); ok {
			updatedNested, count := rewriteReferencesInTypedValue("TextProperty", nested, from, to)
			if count > 0 {
				out["formatText"] = updatedNested
				replacements += count
			}
		}
		if nested, ok := valueMap["sourceText"].(map[string]any); ok {
			updatedNested, count := rewriteReferencesInTypedValue("TextProperty", nested, from, to)
			if count > 0 {
				out["sourceText"] = updatedNested
				replacements += count
			}
		}
		if replacements == 0 {
			return value, 0
		}
		return out, replacements
	case "StructProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		fields, ok := valueMap["value"].(map[string]any)
		if !ok {
			return value, 0
		}
		fieldsOut := cloneAnyMapLocal(fields)
		replacements := 0
		for fieldName, fieldRaw := range fields {
			wrapper, ok := fieldRaw.(map[string]any)
			if !ok {
				continue
			}
			wrappedOut, count := rewriteReferenceWrappedNode(wrapper, from, to)
			if count == 0 {
				continue
			}
			fieldsOut[fieldName] = wrappedOut
			replacements += count
		}
		if replacements == 0 {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		out["value"] = fieldsOut
		return out, replacements
	case "ArrayProperty", "SetProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		replacements := 0
		for _, field := range []string{"value", "removed", "modified", "added", "shadowed"} {
			items, ok := asAnySlice(valueMap[field])
			if !ok {
				continue
			}
			itemsOut := append([]any(nil), items...)
			fieldChanged := false
			for i, itemRaw := range items {
				wrapper, ok := itemRaw.(map[string]any)
				if !ok {
					continue
				}
				wrappedOut, count := rewriteReferenceWrappedNode(wrapper, from, to)
				if count == 0 {
					continue
				}
				itemsOut[i] = wrappedOut
				fieldChanged = true
				replacements += count
			}
			if fieldChanged {
				out[field] = itemsOut
			}
		}
		if replacements == 0 {
			return value, 0
		}
		return out, replacements
	case "MapProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		replacements := 0

		for _, field := range []string{"value", "modified", "added", "shadowed"} {
			entries, ok := asAnySlice(valueMap[field])
			if !ok {
				continue
			}
			entriesOut := append([]any(nil), entries...)
			fieldChanged := false
			for i, entryRaw := range entries {
				entry, ok := entryRaw.(map[string]any)
				if !ok {
					continue
				}
				entryOut := cloneAnyMapLocal(entry)
				entryChanged := false

				if keyNode, ok := entry["key"].(map[string]any); ok {
					keyOut, count := rewriteReferenceWrappedNode(keyNode, from, to)
					if count > 0 {
						entryOut["key"] = keyOut
						replacements += count
						entryChanged = true
					}
				}
				if valueNode, ok := entry["value"].(map[string]any); ok {
					valueOut, count := rewriteReferenceWrappedNode(valueNode, from, to)
					if count > 0 {
						entryOut["value"] = valueOut
						replacements += count
						entryChanged = true
					}
				}

				if entryChanged {
					entriesOut[i] = entryOut
					fieldChanged = true
				}
			}
			if fieldChanged {
				out[field] = entriesOut
			}
		}

		if removed, ok := asAnySlice(valueMap["removed"]); ok {
			removedOut := append([]any(nil), removed...)
			fieldChanged := false
			for i, entryRaw := range removed {
				wrapper, ok := entryRaw.(map[string]any)
				if !ok {
					continue
				}
				wrappedOut, count := rewriteReferenceWrappedNode(wrapper, from, to)
				if count == 0 {
					continue
				}
				removedOut[i] = wrappedOut
				replacements += count
				fieldChanged = true
			}
			if fieldChanged {
				out["removed"] = removedOut
			}
		}

		if replacements == 0 {
			return value, 0
		}
		return out, replacements
	case "OptionalProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		if isSet, _ := valueMap["isSet"].(bool); !isSet {
			return value, 0
		}
		wrapper, ok := valueMap["value"].(map[string]any)
		if !ok {
			return value, 0
		}
		wrappedOut, count := rewriteReferenceWrappedNode(wrapper, from, to)
		if count == 0 {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		out["value"] = wrappedOut
		return out, count
	case "StrProperty":
		s, ok := value.(string)
		if !ok {
			return value, 0
		}
		replaced, count := replaceAllWithCount(s, from, to)
		if count == 0 {
			return value, 0
		}
		return replaced, count
	default:
		return value, 0
	}
}

func rewriteReferenceWrappedNode(wrapper map[string]any, from, to string) (map[string]any, int) {
	childType, _ := wrapper["type"].(string)
	childValue, exists := wrapper["value"]
	if !exists {
		return nil, 0
	}
	updated, count := rewriteReferencesInTypedValue(childType, childValue, from, to)
	if count == 0 {
		return nil, 0
	}
	out := cloneAnyMapLocal(wrapper)
	out["value"] = updated
	return out, count
}

func replaceAllWithCount(src, from, to string) (string, int) {
	if from == "" {
		return src, 0
	}
	count := strings.Count(src, from)
	if count == 0 {
		return src, 0
	}
	return strings.ReplaceAll(src, from, to), count
}

func cloneAnyMapLocal(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
