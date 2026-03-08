package cli

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/wilddogjp/openbpx/pkg/edit"
	"github.com/wilddogjp/openbpx/pkg/uasset"
)

func runMetadataSetRoot(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("metadata set-root", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	key := fs.String("key", "", "metadata key")
	value := fs.String("value", "", "metadata value")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*key) == "" {
		fmt.Fprintln(stderr, "usage: bpx metadata set-root <file.uasset> --export <n> --key <k> --value <v> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(*exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	valueJSON := strconv.Quote(*value)
	paths := []string{
		*key,
		fmt.Sprintf("MetaData[%s]", strconv.Quote(*key)),
		fmt.Sprintf("Metadata[%s]", strconv.Quote(*key)),
	}
	outBytes, result, usedPath, err := applyFirstPropertyMutation(asset, idx, paths, valueJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	outBytes, err = applyDerivedLocalizationUpdates(outBytes, *opts, result.OldValue, result.NewValue, localizationSnapshotSourceString(result.NewValue) == "")
	if err != nil {
		fmt.Fprintf(stderr, "error: update derived localization data: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":        file,
		"export":      *exportIndex,
		"key":         *key,
		"value":       *value,
		"path":        usedPath,
		"oldValue":    result.OldValue,
		"newValue":    result.NewValue,
		"dryRun":      *dryRun,
		"changed":     changed,
		"outputBytes": len(outBytes),
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

func runMetadataSetObject(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("metadata set-object", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	importIndex := fs.Int("import", 0, "1-based import index")
	key := fs.String("key", "", "metadata key")
	value := fs.String("value", "", "metadata value")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || *importIndex <= 0 || strings.TrimSpace(*key) == "" {
		fmt.Fprintln(stderr, "usage: bpx metadata set-object <file.uasset> --export <n> --import <i> --key <k> --value <v> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(*exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if _, err := asset.ResolveImportIndex(*importIndex); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	valueJSON := strconv.Quote(*value)
	paths := []string{
		fmt.Sprintf("ObjectMetaData[%d][%s]", *importIndex, strconv.Quote(*key)),
		fmt.Sprintf("ObjectMetadata[%d][%s]", *importIndex, strconv.Quote(*key)),
		fmt.Sprintf("MetaData[%s]", strconv.Quote(*key)),
		*key,
	}
	outBytes, result, usedPath, err := applyFirstPropertyMutation(asset, idx, paths, valueJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	outBytes, err = applyDerivedLocalizationUpdates(outBytes, *opts, result.OldValue, result.NewValue, localizationSnapshotSourceString(result.NewValue) == "")
	if err != nil {
		fmt.Fprintf(stderr, "error: update derived localization data: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":        file,
		"export":      *exportIndex,
		"import":      *importIndex,
		"key":         *key,
		"value":       *value,
		"path":        usedPath,
		"oldValue":    result.OldValue,
		"newValue":    result.NewValue,
		"dryRun":      *dryRun,
		"changed":     changed,
		"outputBytes": len(outBytes),
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

func runEnumWriteValue(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("enum write-value", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	name := fs.String("name", "", "enum item name")
	value := fs.String("value", "", "enum value")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*name) == "" || strings.TrimSpace(*value) == "" {
		fmt.Fprintln(stderr, "usage: bpx enum write-value <file.uasset> --export <n> --name <k> --value <v> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(*exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	valueJSON := enumValueLiteralToJSON(*value)
	outBytes, result, usedPath, err := applyFirstPropertyMutation(asset, idx, []string{*name}, valueJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	updatedAsset, err := uasset.ParseBytes(outBytes, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: reparse rewritten asset: %v\n", err)
		return 1
	}
	outBytes, _, err = rewriteAssetRegistryValueChange(updatedAsset, result.PropertyName, result.NewValue, result.OldValue, result.NewValue)
	if err != nil {
		fmt.Fprintf(stderr, "error: rewrite asset registry search data: %v\n", err)
		return 1
	}
	updatedAsset, err = uasset.ParseBytes(outBytes, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: reparse asset after asset registry rewrite: %v\n", err)
		return 1
	}
	outBytes, _, err = compactPropertyWriteNameMap(updatedAsset, result.OldValue, result.NewValue, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: compact property write name map: %v\n", err)
		return 1
	}
	updatedAsset, err = uasset.ParseBytes(outBytes, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: reparse asset after name map compaction: %v\n", err)
		return 1
	}
	outBytes, _, err = compactPropertyTagNameIfUnused(updatedAsset, *opts, result.PropertyName)
	if err != nil {
		fmt.Fprintf(stderr, "error: compact enum property name: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":        file,
		"export":      *exportIndex,
		"name":        *name,
		"value":       *value,
		"path":        usedPath,
		"oldValue":    result.OldValue,
		"newValue":    result.NewValue,
		"dryRun":      *dryRun,
		"changed":     changed,
		"outputBytes": len(outBytes),
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

func runStringTableWriteEntry(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("stringtable write-entry", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	key := fs.String("key", "", "string table key")
	value := fs.String("value", "", "entry value")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*key) == "" {
		fmt.Fprintln(stderr, "usage: bpx stringtable write-entry <file.uasset> --export <n> --key <k> --value <v> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(*exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	outBytes, resp, err := applyStringTableEntryUpdate(asset, idx, *key, *value)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	output := map[string]any{
		"file":        file,
		"export":      *exportIndex,
		"key":         *key,
		"value":       *value,
		"dryRun":      *dryRun,
		"changed":     changed,
		"outputBytes": len(outBytes),
	}
	for k, v := range resp {
		output[k] = v
	}
	if *dryRun {
		return printJSON(stdout, output)
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
	return printJSON(stdout, output)
}

func runStringTableRemoveEntry(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("stringtable remove-entry", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	key := fs.String("key", "", "string table key")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*key) == "" {
		fmt.Fprintln(stderr, "usage: bpx stringtable remove-entry <file.uasset> --export <n> --key <k> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(*exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	outBytes, meta, err := applyStringTableEntryRemove(asset, idx, *key)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":        file,
		"export":      *exportIndex,
		"key":         *key,
		"dryRun":      *dryRun,
		"changed":     changed,
		"outputBytes": len(outBytes),
	}
	for k, v := range meta {
		resp[k] = v
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

func runStringTableSetNamespace(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("stringtable set-namespace", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	namespace := fs.String("namespace", "", "string table namespace")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 {
		fmt.Fprintln(stderr, "usage: bpx stringtable set-namespace <file.uasset> --export <n> --namespace <ns> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(*exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	outBytes, oldNamespace, err := applyStringTableNamespaceUpdate(asset, idx, *namespace)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":         file,
		"export":       *exportIndex,
		"oldNamespace": oldNamespace,
		"namespace":    *namespace,
		"dryRun":       *dryRun,
		"changed":      changed,
		"outputBytes":  len(outBytes),
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

func runLocalizationRewriteNamespace(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("localization rewrite-namespace", stderr)
	opts := registerCommonFlags(fs)
	fromNamespace := fs.String("from", "", "old namespace")
	toNamespace := fs.String("to", "", "new namespace")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || strings.TrimSpace(*fromNamespace) == "" || strings.TrimSpace(*toNamespace) == "" {
		fmt.Fprintln(stderr, "usage: bpx localization rewrite-namespace <file.uasset> --from <ns-old> --to <ns-new> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	outBytes, changeCount, warnings, err := applyLocalizationBulkTextRewrite(asset, *opts, func(history map[string]any) int {
		historyType, _ := history["historyType"].(string)
		switch strings.ToLower(strings.TrimSpace(historyType)) {
		case "base":
			namespace, _ := history["namespace"].(string)
			if namespace == *fromNamespace {
				history["namespace"] = *toNamespace
				history["flags"] = 0
				return 1
			}
		case "stringtableentry":
			tableID, _ := history["tableIdName"].(string)
			if tableID == *fromNamespace {
				history["tableIdName"] = *toNamespace
				history["flags"] = 0
				return 1
			}
		}
		return 0
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":          file,
		"fromNamespace": *fromNamespace,
		"toNamespace":   *toNamespace,
		"changeCount":   changeCount,
		"dryRun":        *dryRun,
		"changed":       changed,
		"warnings":      warnings,
		"outputBytes":   len(outBytes),
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

func runLocalizationRekey(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("localization rekey", stderr)
	opts := registerCommonFlags(fs)
	namespace := fs.String("namespace", "", "target namespace")
	fromKey := fs.String("from-key", "", "old localization key")
	toKey := fs.String("to-key", "", "new localization key")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || strings.TrimSpace(*namespace) == "" || strings.TrimSpace(*fromKey) == "" || strings.TrimSpace(*toKey) == "" {
		fmt.Fprintln(stderr, "usage: bpx localization rekey <file.uasset> --namespace <ns> --from-key <k-old> --to-key <k-new> [--dry-run] [--backup]")
		return 1
	}

	file := fs.Arg(0)
	asset, err := uasset.ParseFile(file, *opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	outBytes, changeCount, warnings, err := applyLocalizationBulkTextRewrite(asset, *opts, func(history map[string]any) int {
		historyType, _ := history["historyType"].(string)
		key, _ := history["key"].(string)
		if key != *fromKey {
			return 0
		}
		switch strings.ToLower(strings.TrimSpace(historyType)) {
		case "base":
			ns, _ := history["namespace"].(string)
			if ns == *namespace {
				history["key"] = *toKey
				history["flags"] = 0
				return 1
			}
		case "stringtableentry":
			tableID, _ := history["tableIdName"].(string)
			ns, _ := history["namespace"].(string)
			if tableID == *namespace || ns == *namespace {
				history["key"] = *toKey
				history["flags"] = 0
				return 1
			}
		}
		return 0
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":        file,
		"namespace":   *namespace,
		"fromKey":     *fromKey,
		"toKey":       *toKey,
		"changeCount": changeCount,
		"dryRun":      *dryRun,
		"changed":     changed,
		"warnings":    warnings,
		"outputBytes": len(outBytes),
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

func runLocalizationSetSource(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("localization set-source", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	path := fs.String("path", "", "property path")
	value := fs.String("value", "", "source string")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*path) == "" {
		fmt.Fprintln(stderr, "usage: bpx localization set-source <file.uasset> --export <n> --path <dot.path> --value <text> [--dry-run] [--backup]")
		return 1
	}

	return runPathMutationCommand(fs.Arg(0), *opts, *exportIndex, *path, strconv.Quote(*value), *dryRun, *backup, stdout, stderr, map[string]any{
		"command": "set-source",
		"value":   *value,
	})
}

func runLocalizationSetID(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("localization set-id", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	path := fs.String("path", "", "property path")
	namespace := fs.String("namespace", "", "namespace")
	key := fs.String("key", "", "key")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*path) == "" {
		fmt.Fprintln(stderr, "usage: bpx localization set-id <file.uasset> --export <n> --path <dot.path> --namespace <ns> --key <key> [--dry-run] [--backup]")
		return 1
	}

	payload, err := marshalJSONValue(map[string]any{
		"namespace": *namespace,
		"key":       *key,
		"flags":     0,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: build localization id payload: %v\n", err)
		return 1
	}

	return runPathMutationCommand(fs.Arg(0), *opts, *exportIndex, *path, payload, *dryRun, *backup, stdout, stderr, map[string]any{
		"command":   "set-id",
		"namespace": *namespace,
		"key":       *key,
	})
}

func runLocalizationSetStringTableRef(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("localization set-stringtable-ref", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "1-based export index")
	path := fs.String("path", "", "property path")
	table := fs.String("table", "", "string table id")
	key := fs.String("key", "", "string table entry key")
	dryRun := fs.Bool("dry-run", false, "do not write output")
	backup := fs.Bool("backup", false, "create <file>.backup before overwrite")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex <= 0 || strings.TrimSpace(*path) == "" || strings.TrimSpace(*table) == "" {
		fmt.Fprintln(stderr, "usage: bpx localization set-stringtable-ref <file.uasset> --export <n> --path <dot.path> --table <table-id> --key <key> [--dry-run] [--backup]")
		return 1
	}

	payload, err := marshalJSONValue(map[string]any{
		"historyType": "StringTableEntry",
		"tableIdName": *table,
		"key":         *key,
		"flags":       0,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: build localization stringtable payload: %v\n", err)
		return 1
	}

	return runPathMutationCommand(fs.Arg(0), *opts, *exportIndex, *path, payload, *dryRun, *backup, stdout, stderr, map[string]any{
		"command": "set-stringtable-ref",
		"table":   *table,
		"key":     *key,
	})
}

func runPathMutationCommand(
	file string,
	opts uasset.ParseOptions,
	exportIndex int,
	path string,
	valueJSON string,
	dryRun bool,
	backup bool,
	stdout io.Writer,
	stderr io.Writer,
	extra map[string]any,
) int {
	asset, err := uasset.ParseFile(file, opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	idx, err := asset.ResolveExportIndex(exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	outBytes, result, err := applyPropertyMutation(asset, idx, path, valueJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if command, _ := extra["command"].(string); command != "" {
		removeGatherable := command == "set-stringtable-ref" || (command == "set-source" && localizationSnapshotSourceString(result.NewValue) == "")
		outBytes, err = applyDerivedLocalizationUpdates(outBytes, opts, result.OldValue, result.NewValue, removeGatherable)
		if err != nil {
			fmt.Fprintf(stderr, "error: update derived localization data: %v\n", err)
			return 1
		}
	}
	changed := !bytes.Equal(asset.Raw.Bytes, outBytes)

	resp := map[string]any{
		"file":        file,
		"export":      exportIndex,
		"path":        path,
		"oldValue":    result.OldValue,
		"newValue":    result.NewValue,
		"dryRun":      dryRun,
		"changed":     changed,
		"outputBytes": len(outBytes),
	}
	for k, v := range extra {
		resp[k] = v
	}

	if dryRun {
		return printJSON(stdout, resp)
	}
	if backup {
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

func applyDerivedLocalizationUpdates(outBytes []byte, opts uasset.ParseOptions, oldValue, newValue any, removeGatherable bool) ([]byte, error) {
	updatedAsset, err := uasset.ParseBytes(outBytes, opts)
	if err != nil {
		return nil, fmt.Errorf("reparse localization mutation: %w", err)
	}
	if mutator := buildLocalizationHistoryMutator(oldValue, newValue); mutator != nil {
		nextBytes, _, warnings, err := applyLocalizationBulkTextRewrite(updatedAsset, opts, mutator)
		if err != nil {
			return nil, fmt.Errorf("rewrite derived localization data: %w", err)
		}
		if len(warnings) > 0 {
			return nil, fmt.Errorf("rewrite derived localization warnings: %s", strings.Join(warnings, "; "))
		}
		outBytes = nextBytes
	}
	if !removeGatherable {
		return outBytes, nil
	}

	updatedAsset, err = uasset.ParseBytes(outBytes, opts)
	if err != nil {
		return nil, fmt.Errorf("reparse gatherable localization removal target: %w", err)
	}
	outBytes, err = removeMatchingGatherableLocalization(updatedAsset, oldValue, newValue)
	if err != nil {
		return nil, fmt.Errorf("remove replaced gatherable localization: %w", err)
	}
	updatedAsset, err = uasset.ParseBytes(outBytes, opts)
	if err != nil {
		return nil, fmt.Errorf("reparse gatherable localization removal: %w", err)
	}
	if updatedAsset.Summary.GatherableTextDataCount == 0 && (updatedAsset.Summary.PackageFlags&packageFlagRequiresLoc) != 0 {
		outBytes, _, err = rewritePackageFlags(updatedAsset, updatedAsset.Summary.PackageFlags&^packageFlagRequiresLoc)
		if err != nil {
			return nil, fmt.Errorf("clear localization gather package flag: %w", err)
		}
	}
	return outBytes, nil
}

func buildLocalizationHistoryMutator(oldValue, newValue any) func(history map[string]any) int {
	oldMap, okOld := oldValue.(map[string]any)
	newMap, okNew := newValue.(map[string]any)
	if !okOld || !okNew {
		return nil
	}
	return func(history map[string]any) int {
		if !matchesLocalizationHistorySnapshot(history, oldMap) {
			return 0
		}
		applyLocalizationHistorySnapshot(history, newMap)
		return 1
	}
}

func matchesLocalizationHistorySnapshot(history, snapshot map[string]any) bool {
	if history == nil || snapshot == nil {
		return false
	}
	oldType := strings.TrimSpace(fmt.Sprint(snapshot["historyType"]))
	nextType := strings.TrimSpace(fmt.Sprint(history["historyType"]))
	if oldType != "" && !strings.EqualFold(oldType, nextType) {
		return false
	}

	switch strings.ToLower(oldType) {
	case "stringtableentry":
		if strings.TrimSpace(fmt.Sprint(snapshot["tableIdName"])) != strings.TrimSpace(fmt.Sprint(history["tableIdName"])) {
			return false
		}
		if strings.TrimSpace(fmt.Sprint(snapshot["key"])) != strings.TrimSpace(fmt.Sprint(history["key"])) {
			return false
		}
		return true
	default:
		if strings.TrimSpace(fmt.Sprint(snapshot["namespace"])) != strings.TrimSpace(fmt.Sprint(history["namespace"])) {
			return false
		}
		if strings.TrimSpace(fmt.Sprint(snapshot["key"])) != strings.TrimSpace(fmt.Sprint(history["key"])) {
			return false
		}
		if strings.TrimSpace(fmt.Sprint(snapshot["sourceString"])) != strings.TrimSpace(fmt.Sprint(history["sourceString"])) {
			return false
		}
		return true
	}
}

func applyLocalizationHistorySnapshot(history, snapshot map[string]any) {
	if history == nil || snapshot == nil {
		return
	}
	keys := []string{
		"cultureInvariantString",
		"flags",
		"historyType",
		"historyTypeCode",
		"key",
		"namespace",
		"sourceString",
		"tableId",
		"tableIdName",
		"value",
	}
	for _, key := range keys {
		if value, ok := snapshot[key]; ok {
			history[key] = value
			continue
		}
		delete(history, key)
	}
}

func removeMatchingGatherableLocalization(asset *uasset.Asset, snapshots ...any) ([]byte, error) {
	namespaceSet := map[string]struct{}{}
	keySet := map[string]struct{}{}
	sourceSet := map[string]struct{}{}
	for _, snapshot := range snapshots {
		m, ok := snapshot.(map[string]any)
		if !ok {
			continue
		}
		if ns := strings.TrimSpace(fmt.Sprint(m["namespace"])); ns != "" {
			namespaceSet[ns] = struct{}{}
		}
		if key := strings.TrimSpace(fmt.Sprint(m["key"])); key != "" {
			keySet[key] = struct{}{}
		}
		if source := strings.TrimSpace(fmt.Sprint(m["sourceString"])); source != "" || source == "" {
			sourceSet[source] = struct{}{}
		}
	}
	if len(namespaceSet) == 0 || len(keySet) == 0 {
		return append([]byte(nil), asset.Raw.Bytes...), nil
	}
	return rewriteStringTableGatherableData(asset, func(entries []gatherableTextDataEntry) ([]gatherableTextDataEntry, bool, error) {
		next := make([]gatherableTextDataEntry, 0, len(entries))
		changed := false
		for _, entry := range entries {
			if _, ok := namespaceSet[entry.NamespaceName]; !ok {
				next = append(next, entry)
				continue
			}
			remaining := make([]gatherableTextSourceSiteContext, 0, len(entry.SourceSiteContexts))
			for _, ctx := range entry.SourceSiteContexts {
				if _, keyOK := keySet[ctx.KeyName]; keyOK {
					if len(sourceSet) == 0 {
						changed = true
						continue
					}
					if _, sourceOK := sourceSet[entry.SourceString]; sourceOK {
						changed = true
						continue
					}
				}
				if len(sourceSet) == 1 {
					if _, hasEmpty := sourceSet[""]; hasEmpty && len(entry.SourceSiteContexts) == 1 {
						if _, keyOK := keySet[ctx.KeyName]; keyOK {
							changed = true
							continue
						}
					}
				}
				remaining = append(remaining, ctx)
			}
			if len(remaining) == 0 {
				if len(entry.SourceSiteContexts) != 0 {
					changed = true
				}
				continue
			}
			entry.SourceSiteContexts = remaining
			next = append(next, entry)
		}
		return next, changed, nil
	})
}

func localizationSnapshotSourceString(snapshot any) string {
	m, ok := snapshot.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(m["sourceString"]))
}

const maxStringTableSerializedEntries = 1_000_000

type stringTableSerializedEntry struct {
	Key   string
	Value string
}

type stringTableSerializedPayload struct {
	Namespace   string
	Entries     []stringTableSerializedEntry
	MetadataRaw []byte
	SerialStart int
	SerialEnd   int
	PayloadFrom int
}

func applyStringTableEntryUpdate(asset *uasset.Asset, exportIndex int, key, value string) ([]byte, map[string]any, error) {
	if strings.EqualFold(asset.ResolveClassName(asset.Exports[exportIndex]), "StringTable") {
		payload, err := parseStringTableSerializedPayload(asset, exportIndex)
		if err != nil {
			return nil, nil, err
		}
		for i := range payload.Entries {
			if payload.Entries[i].Key != key {
				continue
			}
			oldValue := payload.Entries[i].Value
			payload.Entries[i].Value = value
			outBytes, err := rewriteStringTableSerializedPayload(asset, exportIndex, payload)
			if err != nil {
				return nil, nil, err
			}
			updatedAsset, err := uasset.ParseBytes(outBytes, uasset.DefaultParseOptions())
			if err != nil {
				return nil, nil, fmt.Errorf("reparse stringtable payload rewrite: %w", err)
			}
			outBytes, err = rewriteStringTableGatherableData(updatedAsset, func(entries []gatherableTextDataEntry) ([]gatherableTextDataEntry, bool, error) {
				changed := false
				for i := range entries {
					for _, ctx := range entries[i].SourceSiteContexts {
						if ctx.KeyName != key {
							continue
						}
						if entries[i].SourceString != value {
							entries[i].SourceString = value
							changed = true
						}
						break
					}
				}
				return entries, changed, nil
			})
			if err != nil {
				return nil, nil, err
			}
			return outBytes, map[string]any{
				"path":     "<StringTable.SerializedEntries>",
				"oldValue": oldValue,
				"newValue": value,
				"format":   "serialized-stringtable",
			}, nil
		}
		return nil, nil, fmt.Errorf("string table key not found: %s", key)
	}

	paths := detectStringTableEntryPaths(asset, exportIndex, key)
	if len(paths) == 0 {
		paths = []string{fmt.Sprintf("Entries[%s]", strconv.Quote(key))}
	}
	outBytes, result, usedPath, err := applyFirstPropertyMutation(asset, exportIndex, paths, strconv.Quote(value))
	if err != nil {
		return nil, nil, err
	}
	return outBytes, map[string]any{
		"path":     usedPath,
		"oldValue": result.OldValue,
		"newValue": result.NewValue,
		"format":   "tagged-properties",
	}, nil
}

func applyStringTableEntryRemove(asset *uasset.Asset, exportIndex int, key string) ([]byte, map[string]any, error) {
	if strings.EqualFold(asset.ResolveClassName(asset.Exports[exportIndex]), "StringTable") {
		payload, err := parseStringTableSerializedPayload(asset, exportIndex)
		if err != nil {
			return nil, nil, err
		}
		filtered := make([]stringTableSerializedEntry, 0, len(payload.Entries))
		removed := ""
		found := false
		for _, entry := range payload.Entries {
			if entry.Key == key {
				removed = entry.Value
				found = true
				continue
			}
			filtered = append(filtered, entry)
		}
		if !found {
			return nil, nil, fmt.Errorf("string table key not found: %s", key)
		}
		payload.Entries = filtered
		outBytes, err := rewriteStringTableSerializedPayload(asset, exportIndex, payload)
		if err != nil {
			return nil, nil, err
		}
		updatedAsset, err := uasset.ParseBytes(outBytes, uasset.DefaultParseOptions())
		if err != nil {
			return nil, nil, fmt.Errorf("reparse stringtable payload rewrite: %w", err)
		}
		outBytes, err = rewriteStringTableGatherableData(updatedAsset, func(entries []gatherableTextDataEntry) ([]gatherableTextDataEntry, bool, error) {
			next := make([]gatherableTextDataEntry, 0, len(entries))
			changed := false
			for _, entry := range entries {
				remainingContexts := make([]gatherableTextSourceSiteContext, 0, len(entry.SourceSiteContexts))
				removedAny := false
				for _, ctx := range entry.SourceSiteContexts {
					if ctx.KeyName == key {
						removedAny = true
						changed = true
						continue
					}
					remainingContexts = append(remainingContexts, ctx)
				}
				if removedAny {
					if len(remainingContexts) == 0 {
						continue
					}
					entry.SourceSiteContexts = remainingContexts
				}
				next = append(next, entry)
			}
			return next, changed, nil
		})
		if err != nil {
			return nil, nil, err
		}
		return outBytes, map[string]any{
			"removedValue": removed,
			"entryCount":   len(filtered),
			"format":       "serialized-stringtable",
		}, nil
	}

	return nil, nil, fmt.Errorf("stringtable remove-entry supports StringTable export only")
}

func applyStringTableNamespaceUpdate(asset *uasset.Asset, exportIndex int, namespace string) ([]byte, string, error) {
	if strings.EqualFold(asset.ResolveClassName(asset.Exports[exportIndex]), "StringTable") {
		payload, err := parseStringTableSerializedPayload(asset, exportIndex)
		if err != nil {
			return nil, "", err
		}
		oldNamespace := payload.Namespace
		payload.Namespace = namespace
		outBytes, err := rewriteStringTableSerializedPayload(asset, exportIndex, payload)
		if err != nil {
			return nil, "", err
		}
		updatedAsset, err := uasset.ParseBytes(outBytes, uasset.DefaultParseOptions())
		if err != nil {
			return nil, "", fmt.Errorf("reparse stringtable payload rewrite: %w", err)
		}
		outBytes, err = rewriteStringTableGatherableData(updatedAsset, func(entries []gatherableTextDataEntry) ([]gatherableTextDataEntry, bool, error) {
			changed := false
			for i := range entries {
				if entries[i].NamespaceName == namespace {
					continue
				}
				entries[i].NamespaceName = namespace
				changed = true
			}
			return entries, changed, nil
		})
		if err != nil {
			return nil, "", err
		}
		return outBytes, oldNamespace, nil
	}

	paths := detectStringTableNamespacePaths(asset, exportIndex)
	if len(paths) == 0 {
		paths = []string{"Namespace", "StringTableNamespace", "TableNamespace"}
	}
	outBytes, result, _, err := applyFirstPropertyMutation(asset, exportIndex, paths, strconv.Quote(namespace))
	if err != nil {
		return nil, "", err
	}
	oldNamespace, _ := result.OldValue.(string)
	return outBytes, oldNamespace, nil
}

func detectStringTableNamespacePaths(asset *uasset.Asset, exportIndex int) []string {
	parsed := asset.ParseExportProperties(exportIndex)
	preferred := make([]string, 0, 4)
	others := make([]string, 0, 4)
	for _, p := range parsed.Properties {
		rootType := propertyRootType(p.TypeString(asset.Names))
		if rootType != "StrProperty" && rootType != "NameProperty" {
			continue
		}
		name := p.Name.Display(asset.Names)
		lower := strings.ToLower(name)
		if strings.Contains(lower, "namespace") {
			preferred = append(preferred, name)
		} else {
			others = append(others, name)
		}
	}
	return append(preferred, others...)
}

func parseStringTableSerializedPayload(asset *uasset.Asset, exportIndex int) (*stringTableSerializedPayload, error) {
	if asset == nil {
		return nil, fmt.Errorf("asset is nil")
	}
	exp := asset.Exports[exportIndex]
	serialStart := int(exp.SerialOffset)
	serialEnd := int(exp.SerialOffset + exp.SerialSize)
	if serialStart < 0 || serialEnd < serialStart || serialEnd > len(asset.Raw.Bytes) {
		return nil, fmt.Errorf("string table export serial range out of bounds")
	}

	parsed := asset.ParseExportProperties(exportIndex)
	if len(parsed.Warnings) > 0 {
		return nil, fmt.Errorf("string table property parse warnings: %s", strings.Join(parsed.Warnings, "; "))
	}
	payloadFrom := parsed.EndOffset
	if payloadFrom < serialStart || payloadFrom > serialEnd {
		return nil, fmt.Errorf("string table payload start is out of bounds")
	}
	detectedPayloadFrom, err := detectStringTableSerializedPayloadStart(asset.Raw.Bytes, payloadFrom, serialEnd, asset.Summary.UsesByteSwappedSerialization())
	if err != nil {
		return nil, err
	}
	payloadFrom = detectedPayloadFrom

	data := asset.Raw.Bytes[payloadFrom:serialEnd]
	r := uasset.NewByteReaderWithByteSwapping(data, asset.Summary.UsesByteSwappedSerialization())
	namespace, err := r.ReadFString()
	if err != nil {
		return nil, fmt.Errorf("read string table namespace: %w", err)
	}
	count, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read string table entry count: %w", err)
	}
	if count < 0 || count > maxStringTableSerializedEntries {
		return nil, fmt.Errorf("invalid string table entry count: %d", count)
	}

	entries := make([]stringTableSerializedEntry, 0, count)
	for i := int32(0); i < count; i++ {
		key, err := r.ReadFString()
		if err != nil {
			return nil, fmt.Errorf("read string table key[%d]: %w", i, err)
		}
		source, err := r.ReadFString()
		if err != nil {
			return nil, fmt.Errorf("read string table value[%d]: %w", i, err)
		}
		entries = append(entries, stringTableSerializedEntry{Key: key, Value: source})
	}

	metadataRaw := append([]byte(nil), data[r.Offset():]...)
	return &stringTableSerializedPayload{
		Namespace:   namespace,
		Entries:     entries,
		MetadataRaw: metadataRaw,
		SerialStart: serialStart,
		SerialEnd:   serialEnd,
		PayloadFrom: payloadFrom,
	}, nil
}

func detectStringTableSerializedPayloadStart(raw []byte, minStart, serialEnd int, byteSwapped bool) (int, error) {
	bestStart := -1
	bestRemaining := -1

	for candidate := minStart; candidate < serialEnd; candidate++ {
		r := uasset.NewByteReaderWithByteSwapping(raw[candidate:serialEnd], byteSwapped)
		if _, err := r.ReadFString(); err != nil {
			continue
		}
		count, err := r.ReadInt32()
		if err != nil || count < 0 || count > maxStringTableSerializedEntries {
			continue
		}

		valid := true
		for i := int32(0); i < count; i++ {
			if _, err := r.ReadFString(); err != nil {
				valid = false
				break
			}
			if _, err := r.ReadFString(); err != nil {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}

		remaining := r.Remaining()
		if remaining < 4 {
			continue
		}
		metaCount, err := r.ReadInt32()
		if err != nil || metaCount < 0 || metaCount > maxLocalizationContainerEntries {
			continue
		}
		if bestStart < 0 || remaining < bestRemaining {
			bestStart = candidate
			bestRemaining = remaining
			if remaining == 0 {
				break
			}
		}
	}

	if bestStart < 0 {
		return 0, fmt.Errorf("failed to detect string table serialized payload start")
	}
	return bestStart, nil
}

func rewriteStringTableSerializedPayload(asset *uasset.Asset, exportIndex int, payload *stringTableSerializedPayload) ([]byte, error) {
	if asset == nil || payload == nil {
		return nil, fmt.Errorf("string table payload is nil")
	}
	serialStart := payload.SerialStart
	serialEnd := payload.SerialEnd
	payloadFrom := payload.PayloadFrom
	if serialStart < 0 || payloadFrom < serialStart || serialEnd < payloadFrom || serialEnd > len(asset.Raw.Bytes) {
		return nil, fmt.Errorf("string table rewrite range is out of bounds")
	}

	order := packageByteOrder(asset)
	serializedTail := make([]byte, 0, 64+len(payload.MetadataRaw))
	serializedTail = appendFStringOrdered(serializedTail, payload.Namespace, order)
	serializedTail = appendInt32Ordered(serializedTail, int32(len(payload.Entries)), order)
	for _, entry := range payload.Entries {
		serializedTail = appendFStringOrdered(serializedTail, entry.Key, order)
		serializedTail = appendFStringOrdered(serializedTail, entry.Value, order)
	}
	serializedTail = append(serializedTail, payload.MetadataRaw...)

	oldPayload := asset.Raw.Bytes[serialStart:serialEnd]
	prefixLen := payloadFrom - serialStart
	newPayload := make([]byte, 0, prefixLen+len(serializedTail))
	newPayload = append(newPayload, oldPayload[:prefixLen]...)
	newPayload = append(newPayload, serializedTail...)

	return edit.RewriteAsset(asset, []edit.ExportMutation{{ExportIndex: exportIndex, Payload: newPayload}})
}

func appendInt32Ordered(dst []byte, v int32, order binary.ByteOrder) []byte {
	buf := make([]byte, 4)
	order.PutUint32(buf, uint32(v))
	return append(dst, buf...)
}

func appendFStringOrdered(dst []byte, s string, order binary.ByteOrder) []byte {
	if s == "" {
		return appendInt32Ordered(dst, 0, order)
	}
	ascii := true
	for _, r := range s {
		if r > 0x7f {
			ascii = false
			break
		}
	}
	if ascii {
		dst = appendInt32Ordered(dst, int32(len(s)+1), order)
		dst = append(dst, []byte(s)...)
		return append(dst, 0)
	}

	units := utf16.Encode([]rune(s))
	dst = appendInt32Ordered(dst, -int32(len(units)+1), order)
	for _, unit := range units {
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, unit)
		dst = append(dst, buf...)
	}
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, 0)
	return append(dst, buf...)
}

func rewriteStringTableGatherableData(
	asset *uasset.Asset,
	mutator func([]gatherableTextDataEntry) ([]gatherableTextDataEntry, bool, error),
) ([]byte, error) {
	if asset == nil {
		return nil, fmt.Errorf("asset is nil")
	}
	if asset.Summary.GatherableTextDataCount <= 0 || asset.Summary.GatherableTextDataOffset <= 0 {
		return append([]byte(nil), asset.Raw.Bytes...), nil
	}
	if mutator == nil {
		return append([]byte(nil), asset.Raw.Bytes...), nil
	}

	oldSection, sectionStart, sectionEnd, present := sectionByOffset(asset, int64(asset.Summary.GatherableTextDataOffset))
	if !present {
		return nil, fmt.Errorf(
			"gatherable text data section is not present (count=%d, offset=%d)",
			asset.Summary.GatherableTextDataCount,
			asset.Summary.GatherableTextDataOffset,
		)
	}
	entries, warnings := parseGatherableTextDataSection(asset)
	if len(warnings) > 0 {
		return nil, fmt.Errorf("cannot safely rewrite gatherable text data: %s", strings.Join(warnings, "; "))
	}
	nextEntries, changed, err := mutator(entries)
	if err != nil {
		return nil, err
	}
	nextEntries = coalesceGatherableTextDataEntries(nextEntries)
	if !changed {
		return append([]byte(nil), asset.Raw.Bytes...), nil
	}

	newSection, err := encodeGatherableTextDataSection(nextEntries, packageByteOrder(asset))
	if err != nil {
		return nil, fmt.Errorf("encode gatherable text data section: %w", err)
	}
	if bytes.Equal(oldSection, newSection) && int32(len(nextEntries)) == asset.Summary.GatherableTextDataCount {
		return append([]byte(nil), asset.Raw.Bytes...), nil
	}

	outBytes, err := edit.RewriteRawRange(asset, sectionStart, sectionEnd, newSection)
	if err != nil {
		return nil, fmt.Errorf("rewrite gatherable text data section: %w", err)
	}
	if int32(len(nextEntries)) == asset.Summary.GatherableTextDataCount {
		return outBytes, nil
	}
	if err := patchGatherableTextDataCount(outBytes, asset, int32(len(nextEntries))); err != nil {
		return nil, err
	}
	if err := edit.FinalizePackageBytes(outBytes, asset.Summary.FileVersionUE5); err != nil {
		return nil, fmt.Errorf("finalize gatherable text data count patch: %w", err)
	}
	return outBytes, nil
}

func coalesceGatherableTextDataEntries(entries []gatherableTextDataEntry) []gatherableTextDataEntry {
	if len(entries) < 2 {
		return entries
	}
	out := make([]gatherableTextDataEntry, 0, len(entries))
	for _, entry := range entries {
		merged := false
		for i := range out {
			if out[i].NamespaceName != entry.NamespaceName {
				continue
			}
			if out[i].SourceString != entry.SourceString {
				continue
			}
			if !reflect.DeepEqual(out[i].SourceStringMeta, entry.SourceStringMeta) {
				continue
			}
			out[i].SourceSiteContexts = append(out[i].SourceSiteContexts, entry.SourceSiteContexts...)
			merged = true
			break
		}
		if !merged {
			out = append(out, entry)
		}
	}
	return out
}

func patchGatherableTextDataCount(raw []byte, asset *uasset.Asset, count int32) error {
	if asset == nil {
		return fmt.Errorf("asset is nil")
	}
	order, byteSwap, err := detectPackageByteOrder(raw)
	if err != nil {
		return err
	}
	r := uasset.NewByteReaderWithByteSwapping(raw, byteSwap)
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	legacyVersion, err := r.ReadInt32()
	if err != nil {
		return err
	}
	if legacyVersion != -4 {
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	fileUE4, err := r.ReadInt32()
	if err != nil {
		return err
	}
	fileUE5, err := r.ReadInt32()
	if err != nil {
		return err
	}
	fileLicensee, err := r.ReadInt32()
	if err != nil {
		return err
	}
	if fileUE4 == 0 && fileUE5 == 0 && fileLicensee == 0 {
		fileUE5 = asset.Summary.FileVersionUE5
	}
	if fileUE5 >= ue5PackageSavedHash {
		if err := r.Skip(20); err != nil {
			return err
		}
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	if legacyVersion <= -2 {
		if err := skipSummaryCustomVersionsForRewrite(r, legacyVersion); err != nil {
			return err
		}
	}
	if fileUE5 < ue5PackageSavedHash {
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	if _, err := r.ReadFString(); err != nil {
		return err
	}
	packageFlags, err := r.ReadUint32()
	if err != nil {
		return err
	}
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if asset.Summary.SupportsSoftObjectPathListInSummary() {
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	if packageFlags&packageFlagFilterEditorOnly == 0 {
		if _, err := r.ReadFString(); err != nil {
			return err
		}
	}
	countPos := r.Offset()
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if countPos < 0 || countPos+4 > len(raw) {
		return fmt.Errorf("gatherable text data count position out of bounds")
	}
	order.PutUint32(raw[countPos:countPos+4], uint32(count))
	return nil
}

func applyLocalizationBulkTextRewrite(asset *uasset.Asset, opts uasset.ParseOptions, mutator func(history map[string]any) int) ([]byte, int, []string, error) {
	if asset == nil {
		return nil, 0, nil, fmt.Errorf("asset is nil")
	}

	workingAsset := asset
	workingBytes := append([]byte(nil), asset.Raw.Bytes...)
	changeCount := 0

	for exportIdx := 0; exportIdx < len(workingAsset.Exports); exportIdx++ {
		for {
			props := workingAsset.ParseExportProperties(exportIdx)
			if len(props.Warnings) > 0 {
				return nil, 0, nil, fmt.Errorf(
					"cannot safely rewrite localization in export %d: %s",
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
				updated, count := rewriteLocalizationTypedValue(p.TypeString(workingAsset.Names), decoded, mutator)
				if count == 0 {
					continue
				}

				valueJSON, err := marshalJSONValue(updated)
				if err != nil {
					return nil, 0, nil, fmt.Errorf("export %d %s: marshal rewritten value: %w", exportIdx+1, propName, err)
				}
				result, err := edit.BuildPropertySetMutation(workingAsset, exportIdx, propName, valueJSON)
				if err != nil {
					return nil, 0, nil, fmt.Errorf("export %d %s: %w", exportIdx+1, propName, err)
				}

				workingBytes, err = edit.RewriteAsset(workingAsset, []edit.ExportMutation{result.Mutation})
				if err != nil {
					return nil, 0, nil, fmt.Errorf("rewrite asset (export=%d property=%s): %w", exportIdx+1, propName, err)
				}
				workingAsset, err = uasset.ParseBytes(workingBytes, opts)
				if err != nil {
					return nil, 0, nil, fmt.Errorf("reparse rewritten asset (export=%d property=%s): %w", exportIdx+1, propName, err)
				}

				changeCount += count
				mutated = true
				break
			}
			if !mutated {
				break
			}
		}
	}

	gatherableBytes, gatherableCount, gatherableWarnings, err := applyGatherableLocalizationBulkRewrite(workingAsset, mutator)
	if err != nil {
		return nil, 0, nil, err
	}
	if len(gatherableWarnings) > 0 {
		return nil, 0, nil, fmt.Errorf("cannot safely rewrite gatherable text data: %s", strings.Join(gatherableWarnings, "; "))
	}
	changeCount += gatherableCount
	if !bytes.Equal(gatherableBytes, workingBytes) {
		workingBytes = gatherableBytes
		workingAsset, err = uasset.ParseBytes(workingBytes, opts)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("reparse rewritten asset (gatherable text data): %w", err)
		}
	}

	assetRegistryBytes, assetRegistryCount, err := rewriteAssetRegistryTextValuesWithDependencyOffset(workingAsset, mutator, 0, false)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("rewrite asset registry text values: %w", err)
	}
	changeCount += assetRegistryCount
	if !bytes.Equal(assetRegistryBytes, workingBytes) {
		workingBytes = assetRegistryBytes
	}

	return workingBytes, changeCount, nil, nil
}

func applyGatherableLocalizationBulkRewrite(asset *uasset.Asset, mutator func(history map[string]any) int) ([]byte, int, []string, error) {
	if asset == nil {
		return nil, 0, nil, fmt.Errorf("asset is nil")
	}
	if asset.Summary.GatherableTextDataCount <= 0 {
		return append([]byte(nil), asset.Raw.Bytes...), 0, nil, nil
	}

	sectionStart := int64(asset.Summary.GatherableTextDataOffset)
	oldSection, _, sectionEnd, present := sectionByOffset(asset, sectionStart)
	if !present {
		warnings := []string{
			fmt.Sprintf(
				"gatherable text data section is not present (count=%d, offset=%d)",
				asset.Summary.GatherableTextDataCount,
				asset.Summary.GatherableTextDataOffset,
			),
		}
		return append([]byte(nil), asset.Raw.Bytes...), 0, warnings, nil
	}

	entries, warnings := parseGatherableTextDataSection(asset)
	if len(entries) == 0 {
		return append([]byte(nil), asset.Raw.Bytes...), 0, warnings, nil
	}

	changeCount := 0
	changed := false
	for i := range entries {
		entry := &entries[i]
		updatedNamespace := entry.NamespaceName
		updatedSource := entry.SourceString
		for j := range entry.SourceSiteContexts {
			ctx := &entry.SourceSiteContexts[j]
			history := map[string]any{
				"historyType":     "Base",
				"historyTypeCode": uint8(0),
				"namespace":       entry.NamespaceName,
				"key":             ctx.KeyName,
				"sourceString":    entry.SourceString,
			}
			count := mutator(history)
			if count == 0 {
				continue
			}
			changeCount += count

			if ns, ok := history["namespace"].(string); ok && ns != updatedNamespace {
				updatedNamespace = ns
				changed = true
			}
			if source, ok := history["sourceString"].(string); ok && source != updatedSource {
				updatedSource = source
				changed = true
			}
			if key, ok := history["key"].(string); ok && key != ctx.KeyName {
				ctx.KeyName = key
				changed = true
			}
		}
		if updatedNamespace != entry.NamespaceName {
			entry.NamespaceName = updatedNamespace
		}
		if updatedSource != entry.SourceString {
			entry.SourceString = updatedSource
		}
	}
	if !changed {
		return append([]byte(nil), asset.Raw.Bytes...), 0, warnings, nil
	}

	newSection, err := encodeGatherableTextDataSection(entries, packageByteOrder(asset))
	if err != nil {
		return nil, 0, nil, err
	}
	if bytes.Equal(oldSection, newSection) {
		return append([]byte(nil), asset.Raw.Bytes...), 0, warnings, nil
	}

	outBytes, err := edit.RewriteRawRange(asset, sectionStart, sectionEnd, newSection)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("rewrite gatherable text data section: %w", err)
	}
	return outBytes, changeCount, warnings, nil
}

func encodeGatherableTextDataSection(entries []gatherableTextDataEntry, order binary.ByteOrder) ([]byte, error) {
	if len(entries) > maxLocalizationContainerEntries {
		return nil, fmt.Errorf("gatherable text data entry count too large: %d", len(entries))
	}
	out := make([]byte, 0, 512)
	for i, entry := range entries {
		out = appendFStringOrdered(out, entry.NamespaceName, order)
		out = appendFStringOrdered(out, entry.SourceString, order)
		var err error
		out, err = appendLocMetadataObjectOrdered(out, entry.SourceStringMeta, order, 0)
		if err != nil {
			return nil, fmt.Errorf("encode gatherable entry[%d] source metadata: %w", i, err)
		}

		if len(entry.SourceSiteContexts) > maxLocalizationContainerEntries {
			return nil, fmt.Errorf("gatherable entry[%d] source context count too large: %d", i, len(entry.SourceSiteContexts))
		}
		out = appendInt32Ordered(out, int32(len(entry.SourceSiteContexts)), order)
		for j, ctx := range entry.SourceSiteContexts {
			out = appendFStringOrdered(out, ctx.KeyName, order)
			out = appendFStringOrdered(out, ctx.SiteDescription, order)
			out = appendUBoolOrdered(out, ctx.IsEditorOnly, order)
			out = appendUBoolOrdered(out, ctx.IsOptional, order)

			out, err = appendLocMetadataObjectOrdered(out, ctx.InfoMetaData, order, 0)
			if err != nil {
				return nil, fmt.Errorf("encode gatherable entry[%d] source context[%d] info metadata: %w", i, j, err)
			}
			out, err = appendLocMetadataObjectOrdered(out, ctx.KeyMetaData, order, 0)
			if err != nil {
				return nil, fmt.Errorf("encode gatherable entry[%d] source context[%d] key metadata: %w", i, j, err)
			}
		}
	}
	return out, nil
}

func appendUBoolOrdered(dst []byte, value bool, order binary.ByteOrder) []byte {
	if value {
		return appendInt32Ordered(dst, 1, order)
	}
	return appendInt32Ordered(dst, 0, order)
}

func appendLocMetadataObjectOrdered(dst []byte, fields map[string]any, order binary.ByteOrder, depth int) ([]byte, error) {
	if depth >= maxLocMetadataDepth {
		return nil, fmt.Errorf("loc metadata object nesting exceeds %d", maxLocMetadataDepth)
	}
	if fields == nil {
		return appendInt32Ordered(dst, 0, order), nil
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	if len(keys) > maxLocalizationContainerEntries {
		return nil, fmt.Errorf("metadata value count too large: %d", len(keys))
	}
	sort.Strings(keys)
	dst = appendInt32Ordered(dst, int32(len(keys)), order)
	for _, key := range keys {
		dst = appendFStringOrdered(dst, key, order)
		var err error
		dst, err = appendLocMetadataValueOrdered(dst, fields[key], order, depth+1)
		if err != nil {
			return nil, fmt.Errorf("encode metadata value (%s): %w", key, err)
		}
	}
	return dst, nil
}

func appendLocMetadataValueOrdered(dst []byte, value any, order binary.ByteOrder, depth int) ([]byte, error) {
	if depth >= maxLocMetadataDepth {
		return nil, fmt.Errorf("loc metadata value nesting exceeds %d", maxLocMetadataDepth)
	}

	switch v := value.(type) {
	case string:
		dst = appendInt32Ordered(dst, locMetadataTypeString, order)
		dst = appendFStringOrdered(dst, v, order)
		return dst, nil
	case bool:
		dst = appendInt32Ordered(dst, locMetadataTypeBoolean, order)
		dst = appendUBoolOrdered(dst, v, order)
		return dst, nil
	case map[string]any:
		dst = appendInt32Ordered(dst, locMetadataTypeObject, order)
		return appendLocMetadataObjectOrdered(dst, v, order, depth+1)
	case []any:
		if len(v) > maxLocalizationContainerEntries {
			return nil, fmt.Errorf("metadata array count too large: %d", len(v))
		}
		dst = appendInt32Ordered(dst, locMetadataTypeArray, order)
		dst = appendInt32Ordered(dst, int32(len(v)), order)
		var err error
		for i, item := range v {
			dst, err = appendLocMetadataValueOrdered(dst, item, order, depth+1)
			if err != nil {
				return nil, fmt.Errorf("encode metadata array[%d]: %w", i, err)
			}
		}
		return dst, nil
	case []string:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
		return appendLocMetadataValueOrdered(dst, items, order, depth)
	default:
		return nil, fmt.Errorf("unsupported metadata value type: %T", value)
	}
}

func rewriteLocalizationTypedValue(typeName string, value any, mutator func(history map[string]any) int) (any, int) {
	rootType := propertyRootType(typeName)
	switch rootType {
	case "TextProperty":
		history, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(history)
		total := mutator(out)
		if nested, ok := history["formatText"].(map[string]any); ok {
			updatedNested, count := rewriteLocalizationTypedValue("TextProperty", nested, mutator)
			if count > 0 {
				out["formatText"] = updatedNested
				total += count
			}
		}
		if nested, ok := history["sourceText"].(map[string]any); ok {
			updatedNested, count := rewriteLocalizationTypedValue("TextProperty", nested, mutator)
			if count > 0 {
				out["sourceText"] = updatedNested
				total += count
			}
		}
		if total == 0 {
			return value, 0
		}
		return out, total
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
		total := 0
		for fieldName, fieldRaw := range fields {
			wrapper, ok := fieldRaw.(map[string]any)
			if !ok {
				continue
			}
			updatedWrapper, count := rewriteLocalizationWrappedValue(wrapper, mutator)
			if count == 0 {
				continue
			}
			fieldsOut[fieldName] = updatedWrapper
			total += count
		}
		if total == 0 {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		out["value"] = fieldsOut
		return out, total
	case "ArrayProperty", "SetProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		total := 0
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
				updatedWrapper, count := rewriteLocalizationWrappedValue(wrapper, mutator)
				if count == 0 {
					continue
				}
				itemsOut[i] = updatedWrapper
				total += count
				fieldChanged = true
			}
			if fieldChanged {
				out[field] = itemsOut
			}
		}
		if total == 0 {
			return value, 0
		}
		return out, total
	case "MapProperty":
		valueMap, ok := value.(map[string]any)
		if !ok {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		total := 0
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
					updatedKey, count := rewriteLocalizationWrappedValue(keyNode, mutator)
					if count > 0 {
						entryOut["key"] = updatedKey
						total += count
						entryChanged = true
					}
				}
				if valueNode, ok := entry["value"].(map[string]any); ok {
					updatedValue, count := rewriteLocalizationWrappedValue(valueNode, mutator)
					if count > 0 {
						entryOut["value"] = updatedValue
						total += count
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
				updatedWrapper, count := rewriteLocalizationWrappedValue(wrapper, mutator)
				if count == 0 {
					continue
				}
				removedOut[i] = updatedWrapper
				total += count
				fieldChanged = true
			}
			if fieldChanged {
				out["removed"] = removedOut
			}
		}
		if total == 0 {
			return value, 0
		}
		return out, total
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
		updatedWrapper, count := rewriteLocalizationWrappedValue(wrapper, mutator)
		if count == 0 {
			return value, 0
		}
		out := cloneAnyMapLocal(valueMap)
		out["value"] = updatedWrapper
		return out, count
	default:
		return value, 0
	}
}

func rewriteLocalizationWrappedValue(wrapper map[string]any, mutator func(history map[string]any) int) (map[string]any, int) {
	childType, _ := wrapper["type"].(string)
	childValue, ok := wrapper["value"]
	if !ok {
		return nil, 0
	}
	updatedValue, count := rewriteLocalizationTypedValue(childType, childValue, mutator)
	if count == 0 {
		return nil, 0
	}
	out := cloneAnyMapLocal(wrapper)
	out["value"] = updatedValue
	return out, count
}
