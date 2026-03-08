package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/wilddogjp/openbpx/pkg/uasset"
)

type materialAssetReference struct {
	Kind       string `json:"kind"`
	SourcePath string `json:"sourcePath"`
	Index      int32  `json:"index,omitempty"`
	Resolved   string `json:"resolved,omitempty"`
	ClassName  string `json:"className,omitempty"`
	ObjectName string `json:"objectName,omitempty"`
	Package    string `json:"packageName,omitempty"`
	TargetPath string `json:"targetPath,omitempty"`
	AssetName  string `json:"assetName,omitempty"`
	SubPath    string `json:"subPath,omitempty"`
}

var materialParameterPropertyGroups = map[string]string{
	"ScalarParameterValues":                "scalar",
	"VectorParameterValues":                "vector",
	"DoubleVectorParameterValues":          "doubleVector",
	"TextureParameterValues":               "texture",
	"TextureCollectionParameterValues":     "textureCollection",
	"RuntimeVirtualTextureParameterValues": "runtimeVirtualTexture",
	"SparseVolumeTextureParameterValues":   "sparseVolumeTexture",
	"FontParameterValues":                  "font",
	"UserSceneTextureOverrides":            "userSceneTextureOverride",
}

func runMaterial(args []string, stdout, stderr io.Writer) int {
	return dispatchSubcommand(
		args,
		stdout,
		stderr,
		"usage: bpx material <read> ...",
		"unknown material command: %s\n",
		subcommandSpec{Name: "read", Run: runMaterialRead},
	)
}

func runMaterialRead(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("material read", stderr)
	opts := registerCommonFlags(fs)
	exportIndex := fs.Int("export", 0, "optional 1-based material export index")
	includeHLSL := fs.Bool("include-hlsl", true, "include HLSL-related custom expression details")
	childrenRoot := fs.String("children-root", "", "optional directory to scan for child material instances")
	parentToken := fs.String("parent", "", "optional parent token override for child scan")
	pattern := fs.String("pattern", "*.uasset", "glob pattern for child scan")
	recursive := fs.Bool("recursive", true, "scan child directory recursively")
	limit := fs.Int("limit", 0, "optional child result limit (0 = unlimited)")
	if err := parseFlagSet(fs, args); err != nil {
		return 1
	}
	if fs.NArg() != 1 || *exportIndex < 0 {
		fmt.Fprintln(stderr, "usage: bpx material read <file.uasset> [--export <n>] [--include-hlsl] [--children-root <directory>] [--parent <token>] [--pattern \"*.uasset\"] [--recursive] [--limit <n>]")
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

	inspectPayload, err := buildMaterialInspectPayload(file, asset, *exportIndex)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	resp := map[string]any{
		"file":            file,
		"exportFilter":    *exportIndex,
		"readVersion":     1,
		"inspect":         inspectPayload,
		"hlslIncluded":    *includeHLSL,
		"childrenScanned": false,
	}

	if *includeHLSL {
		hlslPayload, err := buildMaterialHLSLPayload(file, asset, *exportIndex)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		resp["hlsl"] = hlslPayload
	}

	childrenRootValue := strings.TrimSpace(*childrenRoot)
	if childrenRootValue != "" {
		token := strings.TrimSpace(*parentToken)
		if token == "" {
			derived, err := deriveMaterialReadParentToken(inspectPayload, *exportIndex)
			if err != nil {
				fmt.Fprintf(stderr, "error: %v\n", err)
				return 1
			}
			token = derived
		}

		childrenPayload, err := buildMaterialChildrenPayload(childrenRootValue, *opts, *pattern, *recursive, token, *limit)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		resp["childrenScanned"] = true
		resp["childrenRoot"] = childrenRootValue
		resp["childrenParentFilter"] = token
		resp["children"] = childrenPayload
	}

	return printJSON(stdout, resp)
}

func buildMaterialInspectPayload(file string, asset *uasset.Asset, exportFilter int) (map[string]any, error) {
	targets, err := materialTargetExports(asset, exportFilter)
	if err != nil {
		return nil, err
	}

	materials := make([]map[string]any, 0, len(targets))
	for _, idx := range targets {
		materials = append(materials, buildMaterialInspectEntry(asset, idx))
	}

	resp := map[string]any{
		"file":          file,
		"exportFilter":  exportFilter,
		"materialCount": len(materials),
		"materials":     materials,
	}
	if len(materials) == 0 {
		resp["note"] = "no material exports found"
	}
	return resp, nil
}

func buildMaterialChildrenPayload(rootDir string, parseOptions uasset.ParseOptions, pattern string, recursive bool, parentToken string, limit int) (map[string]any, error) {
	files, err := uasset.CollectAssetFiles(rootDir, pattern, recursive)
	if err != nil {
		return nil, err
	}

	token := strings.TrimSpace(parentToken)
	parseFailures := make([]map[string]string, 0, 8)
	matches := make([]map[string]any, 0, 16)
	materialInstanceCount := 0
	truncated := false

scanFiles:
	for _, file := range files {
		asset, err := uasset.ParseFile(file, parseOptions)
		if err != nil {
			parseFailures = append(parseFailures, map[string]string{"file": file, "error": err.Error()})
			continue
		}
		for i, exp := range asset.Exports {
			className := asset.ResolveClassName(exp)
			if !isMaterialInstanceClassName(className) {
				continue
			}
			materialInstanceCount++
			props := asset.ParseExportProperties(i)
			parent := materialParentFromProperties(asset, props.Properties)
			if parent == nil {
				continue
			}
			if !materialReferenceMatchesToken(parent, token) {
				continue
			}

			item := map[string]any{
				"file":        file,
				"packageName": asset.Summary.PackageName,
				"export":      i + 1,
				"objectName":  exp.ObjectName.Display(asset.Names),
				"className":   className,
				"parent":      parent,
			}
			if len(props.Warnings) > 0 {
				item["warnings"] = append([]string(nil), props.Warnings...)
			}
			matches = append(matches, item)
			if limit > 0 && len(matches) >= limit {
				truncated = true
				break scanFiles
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		leftFile, _ := matches[i]["file"].(string)
		rightFile, _ := matches[j]["file"].(string)
		if leftFile != rightFile {
			return leftFile < rightFile
		}
		leftExport, _ := matches[i]["export"].(int)
		rightExport, _ := matches[j]["export"].(int)
		return leftExport < rightExport
	})

	return map[string]any{
		"directory":             rootDir,
		"pattern":               pattern,
		"recursive":             recursive,
		"parentFilter":          token,
		"limit":                 limit,
		"fileCount":             len(files),
		"materialInstanceCount": materialInstanceCount,
		"matchCount":            len(matches),
		"truncated":             truncated,
		"parseFailCount":        len(parseFailures),
		"parseFailures":         parseFailures,
		"matches":               matches,
	}, nil
}

func buildMaterialHLSLPayload(file string, asset *uasset.Asset, exportFilter int) (map[string]any, error) {
	targets, err := materialTargetExports(asset, exportFilter)
	if err != nil {
		return nil, err
	}
	targetSet := make(map[int]struct{}, len(targets))
	for _, idx := range targets {
		targetSet[idx] = struct{}{}
	}

	expressionsByOwner := map[int][]map[string]any{}
	unboundExpressions := make([]map[string]any, 0, 4)
	customExpressionCount := 0
	hlslSnippetCount := 0

	for i, exp := range asset.Exports {
		className := asset.ResolveClassName(exp)
		if !isMaterialCustomExpressionClassName(className) {
			continue
		}
		ownerIdx, ownerOK := materialOwnerExportIndex(asset, i)
		if len(targetSet) > 0 {
			if !ownerOK {
				continue
			}
			if _, ok := targetSet[ownerIdx]; !ok {
				continue
			}
		}

		item := buildMaterialCustomExpressionEntry(asset, i, ownerIdx, ownerOK)
		if code, _ := item["code"].(string); strings.TrimSpace(code) != "" {
			hlslSnippetCount++
		}
		customExpressionCount++
		if ownerOK {
			expressionsByOwner[ownerIdx] = append(expressionsByOwner[ownerIdx], item)
		} else {
			unboundExpressions = append(unboundExpressions, item)
		}
	}

	materials := make([]map[string]any, 0, len(targets))
	for _, idx := range targets {
		exp := asset.Exports[idx]
		customExpressions := expressionsByOwner[idx]
		if customExpressions == nil {
			customExpressions = []map[string]any{}
		}
		materialEntry := map[string]any{
			"export":                idx + 1,
			"objectName":            exp.ObjectName.Display(asset.Names),
			"className":             asset.ResolveClassName(exp),
			"customExpressionCount": len(expressionsByOwner[idx]),
			"customExpressions":     customExpressions,
		}
		materials = append(materials, materialEntry)
	}

	resp := map[string]any{
		"file":                  file,
		"exportFilter":          exportFilter,
		"materialCount":         len(materials),
		"customExpressionCount": customExpressionCount,
		"hlslAvailable":         hlslSnippetCount > 0,
		"materials":             materials,
		"note":                  "`UMaterialExpressionCustom::Code` snippets are stored in assets and shown here. Full translated material HLSL is generated at compile time via FHLSLMaterialTranslator::Translate / UMaterial::CompilePropertyEx and is not serialized as a complete source blob in `.uasset`.",
	}
	if len(unboundExpressions) > 0 {
		resp["unboundCustomExpressions"] = unboundExpressions
	}
	if hlslSnippetCount == 0 {
		resp["hint"] = "no custom-expression HLSL snippets were found in this package"
	}

	return resp, nil
}

func deriveMaterialReadParentToken(inspectPayload map[string]any, exportFilter int) (string, error) {
	materialsRaw, ok := inspectPayload["materials"].([]map[string]any)
	if !ok {
		itemsAny, ok := inspectPayload["materials"].([]any)
		if !ok {
			return "", fmt.Errorf("material read: cannot derive parent token from inspect payload")
		}
		items := make([]map[string]any, 0, len(itemsAny))
		for _, raw := range itemsAny {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			items = append(items, item)
		}
		materialsRaw = items
	}
	if len(materialsRaw) == 0 {
		return "", fmt.Errorf("material read: no material exports found; set --parent explicitly")
	}
	if len(materialsRaw) > 1 && exportFilter == 0 {
		return "", fmt.Errorf("material read: multiple material exports found; set --export or --parent explicitly")
	}
	objectName, _ := materialsRaw[0]["objectName"].(string)
	objectName = strings.TrimSpace(objectName)
	if objectName == "" {
		return "", fmt.Errorf("material read: cannot derive parent token from objectName; set --parent explicitly")
	}
	return objectName, nil
}

func buildMaterialInspectEntry(asset *uasset.Asset, exportIndex int) map[string]any {
	exp := asset.Exports[exportIndex]
	props := asset.ParseExportProperties(exportIndex)
	parameterGroups := map[string]any{}
	parameterCount := 0

	for _, p := range props.Properties {
		propName := p.Name.Display(asset.Names)
		group, ok := materialParameterPropertyGroup(propName)
		if !ok {
			continue
		}
		decoded, ok := asset.DecodePropertyValue(p)
		if !ok {
			continue
		}
		entries := materialDecodeParameterEntries(asset, decoded, propName)
		parameterGroups[group] = entries
		parameterCount += len(entries)
	}

	refs := materialCollectReferencesFromProperties(asset, props.Properties)
	entry := map[string]any{
		"export":              exportIndex + 1,
		"objectName":          exp.ObjectName.Display(asset.Names),
		"className":           asset.ResolveClassName(exp),
		"parent":              materialParentFromProperties(asset, props.Properties),
		"parameterGroups":     parameterGroups,
		"parameterGroupCount": len(parameterGroups),
		"parameterCount":      parameterCount,
		"assetReferenceCount": len(refs),
		"assetReferences":     refs,
	}
	if len(props.Warnings) > 0 {
		entry["warnings"] = append([]string(nil), props.Warnings...)
	}
	return entry
}

func buildMaterialCustomExpressionEntry(asset *uasset.Asset, exportIndex, ownerIndex int, ownerOK bool) map[string]any {
	exp := asset.Exports[exportIndex]
	props := asset.ParseExportProperties(exportIndex)
	out := map[string]any{
		"export":     exportIndex + 1,
		"objectName": exp.ObjectName.Display(asset.Names),
		"className":  asset.ResolveClassName(exp),
	}
	if ownerOK {
		owner := asset.Exports[ownerIndex]
		out["owner"] = map[string]any{
			"export":     ownerIndex + 1,
			"objectName": owner.ObjectName.Display(asset.Names),
			"className":  asset.ResolveClassName(owner),
		}
	}

	for _, p := range props.Properties {
		propName := p.Name.Display(asset.Names)
		decoded, ok := asset.DecodePropertyValue(p)
		if !ok {
			continue
		}
		switch {
		case strings.EqualFold(propName, "Code"):
			if code := firstDecodedString(decoded); code != "" {
				out["code"] = code
				out["codeLineCount"] = strings.Count(code, "\n") + 1
			}
		case strings.EqualFold(propName, "Description"):
			if description := firstDecodedString(decoded); description != "" {
				out["description"] = description
			}
		case strings.EqualFold(propName, "OutputType"):
			if outputType := firstDecodedString(decoded); outputType != "" {
				out["outputType"] = outputType
			} else if numeric, ok := materialAnyToInt32(decoded); ok {
				out["outputType"] = numeric
			}
		case strings.EqualFold(propName, "IncludeFilePaths"):
			includes := materialDecodeStringArray(decoded)
			out["includeFilePathCount"] = len(includes)
			out["includeFilePaths"] = includes
		case strings.EqualFold(propName, "Inputs"):
			inputs := materialDecodeCustomInputs(asset, decoded, "Inputs")
			out["inputCount"] = len(inputs)
			out["inputs"] = inputs
		case strings.EqualFold(propName, "AdditionalOutputs"):
			outputs := materialDecodeCustomOutputs(decoded)
			out["additionalOutputCount"] = len(outputs)
			out["additionalOutputs"] = outputs
		case strings.EqualFold(propName, "AdditionalDefines"):
			defines := materialDecodeCustomDefines(decoded)
			out["defineCount"] = len(defines)
			out["defines"] = defines
		}
	}

	if len(props.Warnings) > 0 {
		out["warnings"] = append([]string(nil), props.Warnings...)
	}
	return out
}

func materialTargetExports(asset *uasset.Asset, exportFilter int) ([]int, error) {
	if asset == nil {
		return nil, fmt.Errorf("asset is nil")
	}
	if exportFilter > 0 {
		idx, err := asset.ResolveExportIndex(exportFilter)
		if err != nil {
			return nil, err
		}
		className := asset.ResolveClassName(asset.Exports[idx])
		if !isMaterialAssetExportClassName(className) {
			return nil, fmt.Errorf("export %d is not a material export (class=%s)", exportFilter, className)
		}
		return []int{idx}, nil
	}

	targets := make([]int, 0, 8)
	for i, exp := range asset.Exports {
		if isMaterialAssetExportClassName(asset.ResolveClassName(exp)) {
			targets = append(targets, i)
		}
	}
	return targets, nil
}

func isMaterialAssetExportClassName(className string) bool {
	low := strings.ToLower(strings.TrimSpace(className))
	if low == "" {
		return false
	}
	if strings.HasPrefix(low, "materialexpression") {
		return false
	}
	if strings.Contains(low, "materialinstanceeditoronlydata") {
		return false
	}
	if strings.HasPrefix(low, "material") {
		return true
	}
	return strings.Contains(low, "materialinstance")
}

func isMaterialInstanceClassName(className string) bool {
	low := strings.ToLower(strings.TrimSpace(className))
	if low == "" {
		return false
	}
	if strings.Contains(low, "materialinstanceeditoronlydata") {
		return false
	}
	return strings.Contains(low, "materialinstance")
}

func isMaterialCustomExpressionClassName(className string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(className)), "materialexpressioncustom")
}

func materialParameterPropertyGroup(propertyName string) (string, bool) {
	for name, group := range materialParameterPropertyGroups {
		if strings.EqualFold(name, propertyName) {
			return group, true
		}
	}
	return "", false
}

func materialDecodeParameterEntries(asset *uasset.Asset, decoded any, sourcePath string) []map[string]any {
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil
	}
	rawItems, ok := asAnySlice(root["value"])
	if !ok {
		return nil
	}
	entries := make([]map[string]any, 0, len(rawItems))
	for i, rawItem := range rawItems {
		entry := map[string]any{"index": i}
		wrapper, _ := rawItem.(map[string]any)
		entryValue := rawItem
		if wrapper != nil {
			if entryType, _ := wrapper["type"].(string); entryType != "" {
				entry["entryType"] = entryType
			}
			if wrappedValue, exists := wrapper["value"]; exists {
				entryValue = wrappedValue
			}
		}

		if structType, fields, ok := materialStructFields(entryValue); ok {
			if structType != "" {
				entry["structType"] = structType
			}
			name, association, layerIndex := materialParseParameterInfo(fields["ParameterInfo"])
			if name != "" {
				entry["parameterName"] = name
			}
			if association != "" {
				entry["association"] = association
			}
			if layerIndex != nil {
				entry["layerIndex"] = *layerIndex
			}
			if value, ok := materialExtractWrappedValue(fields["ParameterValue"]); ok {
				entry["value"] = value
			}
			if expressionGUID := materialExtractExpressionGUID(fields["ExpressionGUID"]); expressionGUID != "" {
				entry["expressionGuid"] = expressionGUID
			}
		}

		if _, hasValue := entry["value"]; !hasValue {
			entry["value"] = entryValue
		}

		refs := materialCollectReferences(asset, entryValue, indexPropertyPath(sourcePath, i))
		if len(refs) > 0 {
			entry["assetReferences"] = refs
		}
		entries = append(entries, entry)
	}
	return entries
}

func materialParseParameterInfo(raw any) (string, string, *int32) {
	_, fields, ok := materialStructFields(raw)
	if !ok {
		return "", "", nil
	}
	name := firstDecodedString(materialUnwrapFieldValue(fields["Name"]))
	association := firstDecodedString(materialUnwrapFieldValue(fields["Association"]))
	if association == "" {
		if rawAssociation, ok := materialExtractWrappedValue(fields["Association"]); ok {
			if numeric, ok := materialAnyToInt32(rawAssociation); ok {
				association = materialAssociationName(numeric)
			}
		}
	}
	layerIndex := (*int32)(nil)
	if rawIndex, ok := materialExtractWrappedValue(fields["Index"]); ok {
		if numeric, ok := materialAnyToInt32(rawIndex); ok {
			v := numeric
			layerIndex = &v
		}
	}
	return name, association, layerIndex
}

func materialAssociationName(value int32) string {
	switch value {
	case 0:
		return "LayerParameter"
	case 1:
		return "BlendParameter"
	case 2:
		return "GlobalParameter"
	default:
		return strconv.Itoa(int(value))
	}
}

func materialExtractExpressionGUID(raw any) string {
	unwrapped := materialUnwrapFieldValue(raw)
	if value, ok := unwrapped.(string); ok {
		return strings.TrimSpace(value)
	}
	return firstDecodedString(unwrapped)
}

func materialExtractWrappedValue(raw any) (any, bool) {
	wrapper, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	value, exists := wrapper["value"]
	if !exists {
		return nil, false
	}
	return materialUnwrapFieldValue(value), true
}

func materialUnwrapFieldValue(raw any) any {
	wrapper, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	if _, hasType := wrapper["type"]; hasType {
		if value, exists := wrapper["value"]; exists {
			return materialUnwrapFieldValue(value)
		}
	}
	return raw
}

func materialStructFields(raw any) (string, map[string]any, bool) {
	value := materialUnwrapFieldValue(raw)
	m, ok := value.(map[string]any)
	if !ok {
		return "", nil, false
	}
	fields, ok := m["value"].(map[string]any)
	if !ok {
		return "", nil, false
	}
	structType, _ := m["structType"].(string)
	return structType, fields, true
}

func materialDecodeStringArray(decoded any) []string {
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil
	}
	items, ok := asAnySlice(root["value"])
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		wrapper, _ := item.(map[string]any)
		value := item
		if wrapper != nil {
			if wrapped, exists := wrapper["value"]; exists {
				value = wrapped
			}
		}
		text := firstDecodedString(value)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func materialDecodeCustomInputs(asset *uasset.Asset, decoded any, sourcePath string) []map[string]any {
	items := materialDecodeStructArray(decoded)
	out := make([]map[string]any, 0, len(items))
	for i, fields := range items {
		entry := map[string]any{"index": i}
		if name := firstDecodedString(materialUnwrapFieldValue(fields["InputName"])); name != "" {
			entry["name"] = name
		}
		if inputValue, ok := materialExtractWrappedValue(fields["Input"]); ok {
			entry["input"] = inputValue
			refs := materialCollectReferences(asset, inputValue, joinPropertyPath(indexPropertyPath(sourcePath, i), "Input"))
			entry["hasConnection"] = len(refs) > 0
			if len(refs) > 0 {
				entry["assetReferences"] = refs
			}
		}
		out = append(out, entry)
	}
	return out
}

func materialDecodeCustomOutputs(decoded any) []map[string]any {
	items := materialDecodeStructArray(decoded)
	out := make([]map[string]any, 0, len(items))
	for i, fields := range items {
		entry := map[string]any{"index": i}
		if name := firstDecodedString(materialUnwrapFieldValue(fields["OutputName"])); name != "" {
			entry["name"] = name
		}
		if outputType, ok := materialExtractWrappedValue(fields["OutputType"]); ok {
			if text := firstDecodedString(outputType); text != "" {
				entry["type"] = text
			} else {
				entry["type"] = outputType
			}
		}
		out = append(out, entry)
	}
	return out
}

func materialDecodeCustomDefines(decoded any) []map[string]any {
	items := materialDecodeStructArray(decoded)
	out := make([]map[string]any, 0, len(items))
	for i, fields := range items {
		entry := map[string]any{"index": i}
		if name := firstDecodedString(materialUnwrapFieldValue(fields["DefineName"])); name != "" {
			entry["name"] = name
		}
		if value := firstDecodedString(materialUnwrapFieldValue(fields["DefineValue"])); value != "" {
			entry["value"] = value
		}
		out = append(out, entry)
	}
	return out
}

func materialDecodeStructArray(decoded any) []map[string]any {
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil
	}
	items, ok := asAnySlice(root["value"])
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		wrapper, ok := item.(map[string]any)
		if !ok {
			continue
		}
		value, exists := wrapper["value"]
		if !exists {
			continue
		}
		_, fields, ok := materialStructFields(value)
		if !ok {
			continue
		}
		out = append(out, fields)
	}
	return out
}

func materialParentFromProperties(asset *uasset.Asset, props []uasset.PropertyTag) map[string]any {
	for _, p := range props {
		if !strings.EqualFold(p.Name.Display(asset.Names), "Parent") {
			continue
		}
		decoded, ok := asset.DecodePropertyValue(p)
		if !ok {
			continue
		}
		refs := materialCollectReferences(asset, decoded, "Parent")
		for _, ref := range refs {
			if ref.Kind == "package-index" || ref.Kind == "soft-object-path" {
				return materialReferenceSummaryMap(ref)
			}
		}
	}
	return nil
}

func materialCollectReferencesFromProperties(asset *uasset.Asset, props []uasset.PropertyTag) []materialAssetReference {
	all := make([]materialAssetReference, 0, 16)
	for _, p := range props {
		decoded, ok := asset.DecodePropertyValue(p)
		if !ok {
			continue
		}
		name := p.Name.Display(asset.Names)
		all = append(all, materialCollectReferences(asset, decoded, name)...)
	}
	return materialDeduplicateReferences(all)
}

func materialCollectReferences(asset *uasset.Asset, value any, sourcePath string) []materialAssetReference {
	collected := make([]materialAssetReference, 0, 4)
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			if ref, ok := materialParsePackageIndexReference(asset, t, path); ok {
				collected = append(collected, ref)
			}
			if ref, ok := materialParseSoftObjectPathReference(t, path); ok {
				collected = append(collected, ref)
			}
			for key, child := range t {
				walk(child, joinPropertyPath(path, key))
			}
		case []any:
			for i, child := range t {
				walk(child, indexPropertyPath(path, i))
			}
		case []map[string]any:
			for i, child := range t {
				walk(child, indexPropertyPath(path, i))
			}
		}
	}
	walk(value, sourcePath)
	return materialDeduplicateReferences(collected)
}

func materialParsePackageIndexReference(asset *uasset.Asset, m map[string]any, sourcePath string) (materialAssetReference, bool) {
	idxRaw, hasIndex := m["index"]
	resolvedRaw, hasResolved := m["resolved"]
	if !hasIndex || !hasResolved {
		return materialAssetReference{}, false
	}
	idx, ok := materialAnyToInt32(idxRaw)
	if !ok || idx == 0 {
		return materialAssetReference{}, false
	}
	resolved, ok := resolvedRaw.(string)
	if !ok || strings.TrimSpace(resolved) == "" || strings.EqualFold(strings.TrimSpace(resolved), "null") {
		return materialAssetReference{}, false
	}

	ref := materialAssetReference{
		Kind:       "package-index",
		SourcePath: sourcePath,
		Index:      idx,
		Resolved:   resolved,
	}

	if idx > 0 {
		exportIdx := int(idx) - 1
		if exportIdx >= 0 && exportIdx < len(asset.Exports) {
			exp := asset.Exports[exportIdx]
			ref.ObjectName = exp.ObjectName.Display(asset.Names)
			ref.ClassName = asset.ResolveClassName(exp)
			if asset.Summary.PackageName != "" {
				ref.Package = asset.Summary.PackageName
				if ref.ObjectName != "" {
					ref.TargetPath = asset.Summary.PackageName + "." + ref.ObjectName
				}
			}
		}
		return ref, true
	}

	importIdx := int(-idx) - 1
	if importIdx >= 0 && importIdx < len(asset.Imports) {
		imp := asset.Imports[importIdx]
		ref.ObjectName = imp.ObjectName.Display(asset.Names)
		ref.ClassName = imp.ClassName.Display(asset.Names)
		packageName := imp.PackageName.Display(asset.Names)
		if packageName != "" && !strings.EqualFold(packageName, "None") {
			ref.Package = packageName
		}
		targetPath := resolveImportTargetPath(asset, imp)
		if targetPath != "" && !strings.EqualFold(targetPath, "None") {
			ref.TargetPath = targetPath
		}
	}
	return ref, true
}

func materialParseSoftObjectPathReference(m map[string]any, sourcePath string) (materialAssetReference, bool) {
	packageName, hasPackage := m["packageName"].(string)
	assetName, hasAsset := m["assetName"].(string)
	subPath, _ := m["subPath"].(string)
	if !hasPackage && !hasAsset {
		return materialAssetReference{}, false
	}
	if strings.TrimSpace(packageName) == "" && strings.TrimSpace(assetName) == "" {
		return materialAssetReference{}, false
	}

	ref := materialAssetReference{
		Kind:       "soft-object-path",
		SourcePath: sourcePath,
		Package:    strings.TrimSpace(packageName),
		AssetName:  strings.TrimSpace(assetName),
		SubPath:    strings.TrimSpace(subPath),
	}
	ref.TargetPath = materialBuildSoftObjectPath(ref.Package, ref.AssetName, ref.SubPath)
	return ref, true
}

func materialBuildSoftObjectPath(packageName, assetName, subPath string) string {
	out := strings.TrimSpace(packageName)
	assetName = strings.TrimSpace(assetName)
	subPath = strings.TrimSpace(subPath)
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
	return out
}

func materialAnyToInt32(v any) (int32, bool) {
	parsed, err := parseInt32Any(v)
	if err == nil {
		return parsed, true
	}
	return 0, false
}

func materialDeduplicateReferences(refs []materialAssetReference) []materialAssetReference {
	if len(refs) == 0 {
		return nil
	}
	out := make([]materialAssetReference, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref.Kind == "" {
			continue
		}
		key := strings.Join([]string{
			ref.Kind,
			ref.SourcePath,
			strconv.Itoa(int(ref.Index)),
			ref.Resolved,
			ref.ClassName,
			ref.ObjectName,
			ref.Package,
			ref.TargetPath,
			ref.AssetName,
			ref.SubPath,
		}, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourcePath != out[j].SourcePath {
			return out[i].SourcePath < out[j].SourcePath
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Resolved != out[j].Resolved {
			return out[i].Resolved < out[j].Resolved
		}
		if out[i].TargetPath != out[j].TargetPath {
			return out[i].TargetPath < out[j].TargetPath
		}
		if out[i].ObjectName != out[j].ObjectName {
			return out[i].ObjectName < out[j].ObjectName
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func materialReferenceMatchesToken(parent map[string]any, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return true
	}
	for _, key := range []string{"kind", "resolved", "className", "objectName", "packageName", "targetPath", "assetName", "subPath"} {
		if value, _ := parent[key].(string); containsFold(value, token) {
			return true
		}
	}
	if index, ok := parent["index"].(int32); ok {
		if containsFold(strconv.Itoa(int(index)), token) {
			return true
		}
	}
	if index, ok := parent["index"].(int); ok {
		if containsFold(strconv.Itoa(index), token) {
			return true
		}
	}
	return false
}

func materialOwnerExportIndex(asset *uasset.Asset, exportIndex int) (int, bool) {
	if asset == nil || exportIndex < 0 || exportIndex >= len(asset.Exports) {
		return 0, false
	}
	visited := map[int]struct{}{}
	current := exportIndex
	for {
		if _, exists := visited[current]; exists {
			return 0, false
		}
		visited[current] = struct{}{}
		outer := asset.Exports[current].OuterIndex
		if outer <= 0 {
			return 0, false
		}
		next := outer.ResolveIndex()
		if next < 0 || next >= len(asset.Exports) {
			return 0, false
		}
		if isMaterialAssetExportClassName(asset.ResolveClassName(asset.Exports[next])) {
			return next, true
		}
		current = next
	}
}

func materialReferenceSummaryMap(ref materialAssetReference) map[string]any {
	out := map[string]any{
		"kind": ref.Kind,
	}
	if ref.Index != 0 {
		out["index"] = ref.Index
	}
	if ref.Resolved != "" {
		out["resolved"] = ref.Resolved
	}
	if ref.ClassName != "" {
		out["className"] = ref.ClassName
	}
	if ref.ObjectName != "" {
		out["objectName"] = ref.ObjectName
	}
	if ref.Package != "" {
		out["packageName"] = ref.Package
	}
	if ref.TargetPath != "" {
		out["targetPath"] = ref.TargetPath
	}
	if ref.AssetName != "" {
		out["assetName"] = ref.AssetName
	}
	if ref.SubPath != "" {
		out["subPath"] = ref.SubPath
	}
	return out
}
