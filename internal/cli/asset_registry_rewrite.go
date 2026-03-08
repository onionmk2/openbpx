package cli

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/wilddogjp/openbpx/pkg/edit"
	"github.com/wilddogjp/openbpx/pkg/uasset"
)

const (
	assetRegistryTagFindInBlueprintsData            = "FiBData"
	assetRegistryTagUnversionedFindInBlueprintsData = "UnversionedFiBData"
)

type assetRegistrySectionData struct {
	DependencyDataOffset int64
	DependencyGap        int64
	DependencyBoundary   []byte
	Objects              []assetRegistryObjectData
	DependencyData       []byte
}

type assetRegistryObjectData struct {
	ObjectPath  string
	ObjectClass string
	Tags        []assetRegistryTagData
}

type assetRegistryTagData struct {
	Key   string
	Value string
}

type fibEncodedData struct {
	Version int32
	Lookup  []fibLookupEntry
	JSON    string
}

type fibLookupEntry struct {
	Key  int32
	Text fibLookupText
}

type fibLookupText struct {
	Raw                      []byte
	Flags                    int32
	HistoryTypeCode          uint8
	Namespace                string
	Key                      string
	SourceString             string
	HasCultureInvariantValue bool
	CultureInvariantString   string
	FormatText               *fibLookupText
	NamedArguments           []fibNamedArgument
	OrderedArguments         []fibFormatArgumentValue
	ArgumentData             []fibArgumentData
	SourceValue              *fibFormatArgumentValue
	HasFormatOptions         bool
	FormatOptionsRaw         []byte
	TargetCulture            string
	CurrencyCode             string
	SourceDateTimeTicks      int64
	DateStyleCode            uint8
	TimeStyleCode            uint8
	CustomPattern            string
	TimeZone                 string
	SourceText               *fibLookupText
	TransformTypeCode        uint8
	TableID                  uasset.NameRef
	TableIDName              string
	GeneratorType            uasset.NameRef
	GeneratorTypeName        string
	GeneratorPayload         []byte
	Names                    []uasset.NameEntry
}

type fibNamedArgument struct {
	Name  string
	Value fibFormatArgumentValue
}

type fibFormatArgumentValue struct {
	Raw      []byte
	TypeCode uint8
	Text     *fibLookupText
}

type fibArgumentData struct {
	Raw           []byte
	Name          string
	ValueTypeCode uint8
	Text          *fibLookupText
}

func rewriteAssetRegistryTextValues(asset *uasset.Asset, mutator func(history map[string]any) int) ([]byte, int, error) {
	return rewriteAssetRegistryTextValuesWithDependencyOffset(asset, mutator, 0, false)
}

func rewriteAssetRegistryTextValuesWithDependencyOffset(asset *uasset.Asset, mutator func(history map[string]any) int, dependencyOffsetOverride int64, hasDependencyOffsetOverride bool) ([]byte, int, error) {
	if asset == nil {
		return nil, 0, fmt.Errorf("asset is nil")
	}
	if mutator == nil {
		return append([]byte(nil), asset.Raw.Bytes...), 0, nil
	}

	section, sectionStart, sectionEnd, err := parseAssetRegistrySection(asset)
	if err != nil {
		return nil, 0, err
	}
	if section == nil {
		return append([]byte(nil), asset.Raw.Bytes...), 0, nil
	}

	changeCount := 0
	changed := false
	for objIdx := range section.Objects {
		obj := &section.Objects[objIdx]
		for tagIdx := range obj.Tags {
			tag := &obj.Tags[tagIdx]
			var nextValue string
			var count int
			switch tag.Key {
			case assetRegistryTagFindInBlueprintsData, assetRegistryTagUnversionedFindInBlueprintsData:
				var err error
				nextValue, count, err = rewriteFiBTagValue(asset, tag.Value, mutator)
				if err != nil {
					return nil, 0, fmt.Errorf("rewrite asset registry tag %s on %s: %w", tag.Key, obj.ObjectPath, err)
				}
			default:
				nextValue, count = rewritePlainAssetRegistryTagValue(tag.Value, mutator)
			}
			if count == 0 || nextValue == tag.Value {
				continue
			}
			tag.Value = nextValue
			changeCount += count
			changed = true
		}
	}
	if !changed {
		return append([]byte(nil), asset.Raw.Bytes...), 0, nil
	}

	newSection, err := encodeAssetRegistrySection(asset, section, sectionStart, dependencyOffsetOverride, hasDependencyOffsetOverride)
	if err != nil {
		return nil, 0, err
	}
	outBytes, err := edit.RewriteRawRange(asset, sectionStart, sectionEnd, newSection)
	if err != nil {
		return nil, 0, fmt.Errorf("rewrite asset registry section: %w", err)
	}
	if hasDependencyOffsetOverride {
		if err := patchAssetRegistryDependencyOffsetField(outBytes, asset, dependencyOffsetOverride); err != nil {
			return nil, 0, err
		}
	}
	return outBytes, changeCount, nil
}

func rewriteAssetRegistryStringReplace(asset *uasset.Asset, from, to string) ([]byte, int, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || from == to {
		return append([]byte(nil), asset.Raw.Bytes...), 0, nil
	}
	return rewriteAssetRegistryTextValues(asset, func(history map[string]any) int {
		return replaceHistoryStrings(history, from, to)
	})
}

func rewriteAssetRegistryValueChange(asset *uasset.Asset, propertyName string, rootValue, oldValue, newValue any) ([]byte, int, error) {
	if outBytes, count, handled, err := rewriteAssetRegistryStructuredValueChange(asset, propertyName, rootValue); err != nil {
		return nil, 0, err
	} else if handled {
		return outBytes, count, nil
	}
	requiresRewrite, err := assetRegistryContainsPropertySearch(asset, propertyName)
	if err != nil {
		return nil, 0, err
	}
	if requiresRewrite {
		return nil, 0, fmt.Errorf(
			"asset registry search data for property %q is present but this value type is not safely rewritable",
			strings.TrimSpace(propertyName),
		)
	}
	return append([]byte(nil), asset.Raw.Bytes...), 0, nil
}

func rewriteAssetRegistryStructuredValueChange(asset *uasset.Asset, propertyName string, rootValue any) ([]byte, int, bool, error) {
	if asset == nil {
		return nil, 0, false, fmt.Errorf("asset is nil")
	}
	propertyName = strings.TrimSpace(propertyName)
	if propertyName == "" || !supportsFiBSearchValue(rootValue) {
		return nil, 0, false, nil
	}

	section, sectionStart, sectionEnd, err := parseAssetRegistrySection(asset)
	if err != nil {
		return nil, 0, false, err
	}
	if section == nil {
		return append([]byte(nil), asset.Raw.Bytes...), 0, true, nil
	}

	changed := false
	changeCount := 0
	handledAny := false
	for objIdx := range section.Objects {
		obj := &section.Objects[objIdx]
		for tagIdx := range obj.Tags {
			tag := &obj.Tags[tagIdx]
			if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) &&
				!strings.EqualFold(tag.Key, assetRegistryTagUnversionedFindInBlueprintsData) {
				continue
			}

			nextValue, count, handled, err := rewriteStructuredFiBTagValue(asset, tag.Value, propertyName, rootValue)
			if err != nil {
				return nil, 0, false, fmt.Errorf("rewrite asset registry tag %s on %s: %w", tag.Key, obj.ObjectPath, err)
			}
			if !handled {
				continue
			}
			handledAny = true
			if nextValue == tag.Value {
				continue
			}
			tag.Value = nextValue
			changed = true
			changeCount += count
		}
	}
	if !handledAny {
		return append([]byte(nil), asset.Raw.Bytes...), 0, true, nil
	}
	if !changed {
		return append([]byte(nil), asset.Raw.Bytes...), 0, true, nil
	}

	newSection, err := encodeAssetRegistrySection(asset, section, sectionStart, 0, false)
	if err != nil {
		return nil, 0, true, err
	}
	outBytes, err := edit.RewriteRawRange(asset, sectionStart, sectionEnd, newSection)
	if err != nil {
		return nil, 0, true, fmt.Errorf("rewrite asset registry section: %w", err)
	}
	return outBytes, changeCount, true, nil
}

func rewriteStructuredFiBTagValue(asset *uasset.Asset, value string, propertyName string, rootValue any) (string, int, bool, error) {
	parsed, err := parseFiBData(asset, value)
	if err != nil {
		return "", 0, false, err
	}
	jsonRoot, err := decodeFiBJSON(parsed.JSON)
	if err != nil {
		return "", 0, false, err
	}

	editor := newFiBLookupEditor(parsed.Lookup, jsonRoot)
	changed, err := editor.rewriteProperty(propertyName, rootValue)
	if err != nil {
		return "", 0, true, err
	}
	if !changed {
		return value, 0, true, nil
	}

	jsonBytes, err := encodeOrderedFiBJSON(editor.JSONRoot)
	if err != nil {
		return "", 0, true, fmt.Errorf("marshal FiB JSON: %w", err)
	}
	parsed.Lookup = editor.Lookup
	parsed.JSON = string(jsonBytes)
	encoded, err := encodeFiBData(parsed)
	if err != nil {
		return "", 0, true, err
	}
	return encoded, 1, true, nil
}

func isAssetRegistryTextHistoryValue(raw any) bool {
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := m["historyType"]; ok {
		return true
	}
	if _, ok := m["historyTypeCode"]; ok {
		return true
	}
	if _, ok := m["cultureInvariantString"]; ok {
		return true
	}
	if _, ok := m["sourceString"]; ok {
		return true
	}
	return false
}

func supportsStructuredFiBSearchValue(raw any) bool {
	kind, _, ok := structuredFiBValueFields(raw)
	return ok && kind != ""
}

func supportsFiBSearchValue(raw any) bool {
	if supportsStructuredFiBSearchValue(raw) {
		return true
	}
	_, ok := assetRegistryScalarSearchValue(raw)
	return ok
}

func structuredFiBValueFields(raw any) (kind string, fields map[string]any, ok bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return "", nil, false
	}
	if structType, ok := m["structType"].(string); ok {
		fieldMap, _ := m["value"].(map[string]any)
		switch strings.ToLower(strings.TrimSpace(structType)) {
		case "vector", "vector3d":
			return "vector", fieldMap, len(fieldMap) != 0
		case "linearcolor":
			return "linearcolor", fieldMap, len(fieldMap) != 0
		case "transform":
			return "transform", fieldMap, len(fieldMap) != 0
		default:
			if inner, ok := m["value"].(map[string]any); ok {
				return structuredFiBValueFields(inner)
			}
			return "", nil, false
		}
	}
	if inner, ok := m["value"].(map[string]any); ok {
		return structuredFiBValueFields(inner)
	}
	if _, ok := m["x"]; ok {
		return "vector", m, true
	}
	if _, ok := m["X"]; ok {
		return "vector", m, true
	}
	return "", nil, false
}

type orderedJSONField struct {
	Key   string
	Value orderedJSONValue
}

type orderedJSONObject struct {
	Fields []orderedJSONField
}

type orderedJSONValue any

type fiBLookupEditor struct {
	Lookup    []fibLookupEntry
	JSONRoot  orderedJSONValue
	textByKey map[int32]string
}

func newFiBLookupEditor(entries []fibLookupEntry, jsonRoot orderedJSONValue) *fiBLookupEditor {
	lookup := make([]fibLookupEntry, len(entries))
	copy(lookup, entries)
	editor := &fiBLookupEditor{
		Lookup:   lookup,
		JSONRoot: jsonRoot,
	}
	editor.rebuildTextIndex()
	return editor
}

func (e *fiBLookupEditor) rebuildTextIndex() {
	e.textByKey = make(map[int32]string, len(e.Lookup))
	for _, entry := range e.Lookup {
		switch entry.Text.HistoryTypeCode {
		case 0:
			e.textByKey[entry.Key] = strings.TrimSpace(entry.Text.SourceString)
		case 255:
			e.textByKey[entry.Key] = strings.TrimSpace(entry.Text.CultureInvariantString)
		}
	}
}

func (e *fiBLookupEditor) rewriteProperty(propertyName string, rootValue any) (bool, error) {
	obj, ok := e.findPropertyObject(e.JSONRoot, strings.TrimSpace(propertyName))
	if !ok {
		return false, nil
	}

	currentValue, hasCurrentValue := obj.value("11")
	if nextScalar, ok := assetRegistryScalarSearchValue(rootValue); ok {
		return e.rewriteScalarProperty(obj, hasCurrentValue, currentValue, nextScalar)
	}

	kind, fields, ok := structuredFiBValueFields(rootValue)
	if !ok {
		return false, nil
	}
	currentObjectValue, ok := obj.objectValue("11")
	if !ok {
		return false, nil
	}

	switch kind {
	case "vector":
		componentKeys := e.lookupLabelKeys(currentObjectValue)
		for _, label := range []string{"x", "y", "z"} {
			if _, ok := componentKeys[label]; ok {
				continue
			}
			if key, ok := e.findKeyByLabel(label); ok {
				componentKeys[label] = key
			}
		}
		nextValue, err := buildStructuredNumericJSON(componentKeys, []string{"x", "y", "z"}, fields)
		if err != nil {
			return false, err
		}
		if orderedJSONEqual(currentObjectValue, nextValue) {
			return false, nil
		}
		obj.set("11", nextValue)
		return true, nil

	case "linearcolor":
		componentKeys := e.lookupLabelKeys(currentObjectValue)
		template, ok := e.baseTemplateForKeys(componentKeys)
		if !ok {
			return false, nil
		}
		componentKeys, err := e.ensureOrderedLabels([]string{"r", "g", "b", "a"}, componentKeys, template)
		if err != nil {
			return false, err
		}
		obj, ok = e.findPropertyObject(e.JSONRoot, strings.TrimSpace(propertyName))
		if !ok {
			return false, nil
		}
		currentObjectValue, ok = obj.objectValue("11")
		if !ok {
			return false, nil
		}
		nextValue, err := buildStructuredNumericJSON(componentKeys, []string{"r", "g", "b", "a"}, fields)
		if err != nil {
			return false, err
		}
		if orderedJSONEqual(currentObjectValue, nextValue) {
			return false, nil
		}
		obj.set("11", nextValue)
		return true, nil

	case "transform":
		fieldKeys := e.lookupLabelKeys(currentObjectValue)
		template, ok := e.baseTemplateForKeys(fieldKeys)
		if !ok {
			return false, nil
		}
		fieldKeys, err := e.ensureOrderedLabels([]string{"rotation", "translation", "scale3d"}, fieldKeys, template)
		if err != nil {
			return false, err
		}
		obj, ok = e.findPropertyObject(e.JSONRoot, strings.TrimSpace(propertyName))
		if !ok {
			return false, nil
		}
		currentObjectValue, ok = obj.objectValue("11")
		if !ok {
			return false, nil
		}

		componentKeys := map[string]int32{}
		for _, label := range []string{"x", "y", "z", "w"} {
			if key, ok := e.findKeyByLabel(label); ok {
				componentKeys[label] = key
			}
		}
		nextValue, err := buildTransformSearchJSON(fieldKeys, componentKeys, fields)
		if err != nil {
			return false, err
		}
		if orderedJSONEqual(currentObjectValue, nextValue) {
			return false, nil
		}
		obj.set("11", nextValue)
		return true, nil
	}

	return false, nil
}

func (e *fiBLookupEditor) rewriteScalarProperty(obj *orderedJSONObject, hasCurrentValue bool, currentValue, nextValue orderedJSONValue) (bool, error) {
	if obj == nil {
		return false, nil
	}
	if currentRef, ok := currentValue.(string); ok {
		if lookupKey, ok := parseJSONLookupRef(currentRef); ok {
			nextText, ok := nextValue.(string)
			if !ok {
				return false, nil
			}
			return e.setLookupTextValue(lookupKey, nextText)
		}
	}
	if hasCurrentValue && orderedJSONEqual(currentValue, nextValue) {
		return false, nil
	}
	if nextBool, ok := nextValue.(bool); ok && !nextBool {
		obj.delete("11")
		return hasCurrentValue, nil
	}
	obj.set("11", nextValue)
	return true, nil
}

func (e *fiBLookupEditor) setLookupTextValue(key int32, value string) (bool, error) {
	for i := range e.Lookup {
		if e.Lookup[i].Key != key {
			continue
		}
		text := e.Lookup[i].Text
		switch text.HistoryTypeCode {
		case 0:
			if text.SourceString == value {
				return false, nil
			}
			text.SourceString = value
		case 255:
			if text.CultureInvariantString == value {
				return false, nil
			}
			text.HasCultureInvariantValue = true
			text.CultureInvariantString = value
		default:
			return false, fmt.Errorf("FiB lookup key %d uses unsupported text history type %d", key, text.HistoryTypeCode)
		}
		e.Lookup[i].Text = text
		e.rebuildTextIndex()
		return true, nil
	}
	return false, fmt.Errorf("FiB lookup key %d not found", key)
}

func (e *fiBLookupEditor) findPropertyObject(node orderedJSONValue, propertyName string) (*orderedJSONObject, bool) {
	switch current := node.(type) {
	case []orderedJSONValue:
		for _, item := range current {
			if found, ok := e.findPropertyObject(item, propertyName); ok {
				return found, true
			}
		}
	case *orderedJSONObject:
		if rawName, ok := current.stringValue("1"); ok {
			if key, ok := parseJSONLookupRef(rawName); ok && strings.EqualFold(strings.TrimSpace(e.textByKey[key]), propertyName) {
				if _, ok := current.value("11"); ok {
					return current, true
				}
			}
		}
		for _, field := range current.Fields {
			if found, ok := e.findPropertyObject(field.Value, propertyName); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func (e *fiBLookupEditor) lookupLabelKeys(rawValue *orderedJSONObject) map[string]int32 {
	out := make(map[string]int32, len(rawValue.Fields))
	for _, field := range rawValue.Fields {
		key, ok := parseJSONLookupRef(field.Key)
		if !ok {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(e.textByKey[key]))
		if label == "" {
			continue
		}
		out[label] = key
	}
	return out
}

func (e *fiBLookupEditor) findKeyByLabel(label string) (int32, bool) {
	for _, entry := range e.Lookup {
		if strings.EqualFold(strings.TrimSpace(e.textByKey[entry.Key]), label) {
			return entry.Key, true
		}
	}
	return 0, false
}

func (e *fiBLookupEditor) baseTemplateForKeys(keys map[string]int32) (fibLookupText, bool) {
	for _, key := range keys {
		for _, entry := range e.Lookup {
			if entry.Key == key {
				return cloneLookupTextTemplate(entry.Text), true
			}
		}
	}
	for _, entry := range e.Lookup {
		if entry.Text.HistoryTypeCode == 0 {
			return cloneLookupTextTemplate(entry.Text), true
		}
	}
	return fibLookupText{}, false
}

func cloneLookupTextTemplate(src fibLookupText) fibLookupText {
	out := src
	out.Raw = nil
	out.Namespace = ""
	out.Key = ""
	out.SourceString = ""
	out.CultureInvariantString = ""
	out.FormatText = nil
	out.NamedArguments = nil
	out.OrderedArguments = nil
	out.ArgumentData = nil
	out.SourceValue = nil
	out.SourceText = nil
	out.FormatOptionsRaw = nil
	out.GeneratorPayload = nil
	out.HistoryTypeCode = 0
	return out
}

func (e *fiBLookupEditor) ensureOrderedLabels(canonical []string, keys map[string]int32, template fibLookupText) (map[string]int32, error) {
	out := make(map[string]int32, len(keys)+len(canonical))
	for label, key := range keys {
		out[label] = key
	}
	for i, label := range canonical {
		if _, ok := out[label]; ok {
			continue
		}
		beforeKey := e.maxLookupKey() + 1
		for _, later := range canonical[i+1:] {
			if key, ok := out[later]; ok && key < beforeKey {
				beforeKey = key
			}
		}
		if err := e.insertBaseLabelsBefore(beforeKey, []string{label}, template); err != nil {
			return nil, err
		}
		for existingLabel, key := range out {
			if key >= beforeKey {
				out[existingLabel] = key + 1
			}
		}
		out[label] = beforeKey
	}
	return out, nil
}

func (e *fiBLookupEditor) maxLookupKey() int32 {
	maxKey := int32(-1)
	for _, entry := range e.Lookup {
		if entry.Key > maxKey {
			maxKey = entry.Key
		}
	}
	return maxKey
}

func (e *fiBLookupEditor) insertBaseLabelsBefore(beforeKey int32, labels []string, template fibLookupText) error {
	if len(labels) == 0 {
		return nil
	}
	delta := int32(len(labels))
	e.JSONRoot = shiftLookupRefsInJSON(e.JSONRoot, beforeKey, delta)
	for i := range e.Lookup {
		if e.Lookup[i].Key >= beforeKey {
			e.Lookup[i].Key += delta
		}
	}

	insertAt := len(e.Lookup)
	for i, entry := range e.Lookup {
		if entry.Key >= beforeKey {
			insertAt = i
			break
		}
	}
	inserted := make([]fibLookupEntry, 0, len(labels))
	for i, label := range labels {
		nextText := cloneLookupTextTemplate(template)
		nextText.SourceString = label
		inserted = append(inserted, fibLookupEntry{
			Key:  beforeKey + int32(i),
			Text: nextText,
		})
	}

	lookup := make([]fibLookupEntry, 0, len(e.Lookup)+len(inserted))
	lookup = append(lookup, e.Lookup[:insertAt]...)
	lookup = append(lookup, inserted...)
	lookup = append(lookup, e.Lookup[insertAt:]...)
	e.Lookup = lookup
	e.rebuildTextIndex()
	return nil
}

func shiftLookupRefsInJSON(node orderedJSONValue, threshold, delta int32) orderedJSONValue {
	switch current := node.(type) {
	case *orderedJSONObject:
		fields := make([]orderedJSONField, 0, len(current.Fields))
		for _, field := range current.Fields {
			fields = append(fields, orderedJSONField{
				Key:   shiftLookupRefString(field.Key, threshold, delta),
				Value: shiftLookupRefsInJSON(field.Value, threshold, delta),
			})
		}
		return &orderedJSONObject{Fields: fields}
	case []orderedJSONValue:
		out := make([]orderedJSONValue, len(current))
		for i, value := range current {
			out[i] = shiftLookupRefsInJSON(value, threshold, delta)
		}
		return out
	case string:
		return shiftLookupRefString(current, threshold, delta)
	default:
		return node
	}
}

func shiftLookupRefString(raw string, threshold, delta int32) string {
	key, ok := parseJSONLookupRef(raw)
	if !ok || key < threshold {
		return raw
	}
	return strconv.FormatInt(int64(key+delta), 10)
}

func parseJSONLookupRef(raw string) (int32, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

func buildStructuredNumericJSON(keys map[string]int32, order []string, fields map[string]any) (*orderedJSONObject, error) {
	out := &orderedJSONObject{Fields: make([]orderedJSONField, 0, len(order))}
	for _, label := range order {
		key, ok := keys[label]
		if !ok {
			continue
		}
		number, ok := jsonNumberFromAny(unwrapStructuredSearchField(fields, label))
		if !ok || isZeroJSONNumber(number) {
			continue
		}
		out.Fields = append(out.Fields, orderedJSONField{
			Key:   strconv.FormatInt(int64(key), 10),
			Value: number,
		})
	}
	return out, nil
}

func buildTransformSearchJSON(fieldKeys, componentKeys map[string]int32, fields map[string]any) (*orderedJSONObject, error) {
	out := &orderedJSONObject{Fields: make([]orderedJSONField, 0, 3)}

	if rotationRaw, ok := selectStructuredSearchField(fields, "Rotation", "rotation"); ok {
		rotationFields := unwrapStructuredSearchStruct(rotationRaw)
		rotationValue, err := buildStructuredNumericJSON(componentKeys, []string{"x", "y", "z", "w"}, rotationFields)
		if err != nil {
			return nil, err
		}
		if len(rotationValue.Fields) > 0 {
			out.Fields = append(out.Fields, orderedJSONField{
				Key:   strconv.FormatInt(int64(fieldKeys["rotation"]), 10),
				Value: rotationValue,
			})
		}
	}
	if translationRaw, ok := selectStructuredSearchField(fields, "Translation", "translation"); ok {
		translationFields := unwrapStructuredSearchStruct(translationRaw)
		translationValue, err := buildStructuredNumericJSON(componentKeys, []string{"x", "y", "z"}, translationFields)
		if err != nil {
			return nil, err
		}
		if len(translationValue.Fields) > 0 {
			out.Fields = append(out.Fields, orderedJSONField{
				Key:   strconv.FormatInt(int64(fieldKeys["translation"]), 10),
				Value: translationValue,
			})
		}
	}
	if scaleRaw, ok := selectStructuredSearchField(fields, "Scale3D", "scale3D", "scale3d"); ok {
		scaleFields := unwrapStructuredSearchStruct(scaleRaw)
		scaleValue, err := buildStructuredNumericJSON(componentKeys, []string{"x", "y", "z"}, scaleFields)
		if err != nil {
			return nil, err
		}
		if len(scaleValue.Fields) > 0 {
			out.Fields = append(out.Fields, orderedJSONField{
				Key:   strconv.FormatInt(int64(fieldKeys["scale3d"]), 10),
				Value: scaleValue,
			})
		}
	}

	return out, nil
}

func selectStructuredSearchField(fields map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func unwrapStructuredSearchStruct(raw any) map[string]any {
	if _, fields, ok := structuredFiBValueFields(raw); ok {
		return fields
	}
	if m, ok := raw.(map[string]any); ok {
		if inner, exists := m["value"]; exists {
			return unwrapStructuredSearchStruct(inner)
		}
		return m
	}
	return nil
}

func unwrapStructuredSearchField(fields map[string]any, label string) any {
	title := label
	if len(title) > 0 {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	value, ok := selectStructuredSearchField(fields, label, title)
	if !ok {
		return nil
	}
	return unwrapStructuredSearchValue(value)
}

func unwrapStructuredSearchValue(raw any) any {
	current := raw
	for {
		wrapped, ok := current.(map[string]any)
		if !ok {
			return current
		}
		inner, exists := wrapped["value"]
		if !exists {
			return current
		}
		current = inner
	}
}

func jsonNumberFromAny(raw any) (json.Number, bool) {
	if raw == nil {
		return "", false
	}
	if number, ok := raw.(json.Number); ok {
		return number, true
	}
	value, ok := formatAssetRegistryNumber(raw)
	if !ok {
		return "", false
	}
	return json.Number(value), true
}

func isZeroJSONNumber(number json.Number) bool {
	raw := strings.TrimSpace(number.String())
	if raw == "" {
		return true
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return false
	}
	return f == 0
}

func decodeFiBJSON(raw string) (orderedJSONValue, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return decodeOrderedJSONValue(dec)
}

func decodeOrderedJSONValue(dec *json.Decoder) (orderedJSONValue, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			obj := &orderedJSONObject{Fields: make([]orderedJSONField, 0, 8)}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("ordered JSON object key is not string")
				}
				child, err := decodeOrderedJSONValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Fields = append(obj.Fields, orderedJSONField{Key: key, Value: child})
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			out := make([]orderedJSONValue, 0, 8)
			for dec.More() {
				child, err := decodeOrderedJSONValue(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, child)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter: %q", value)
		}
	default:
		return value, nil
	}
}

func encodeOrderedFiBJSON(value orderedJSONValue) ([]byte, error) {
	var b strings.Builder
	if err := writeOrderedJSONValue(&b, value); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func writeOrderedJSONValue(dst *strings.Builder, value orderedJSONValue) error {
	switch current := value.(type) {
	case *orderedJSONObject:
		dst.WriteByte('{')
		for i, field := range current.Fields {
			if i > 0 {
				dst.WriteByte(',')
			}
			keyBytes, err := json.Marshal(field.Key)
			if err != nil {
				return err
			}
			dst.Write(keyBytes)
			dst.WriteByte(':')
			if err := writeOrderedJSONValue(dst, field.Value); err != nil {
				return err
			}
		}
		dst.WriteByte('}')
	case []orderedJSONValue:
		dst.WriteByte('[')
		for i, item := range current {
			if i > 0 {
				dst.WriteByte(',')
			}
			if err := writeOrderedJSONValue(dst, item); err != nil {
				return err
			}
		}
		dst.WriteByte(']')
	case string, bool, nil, json.Number:
		buf, err := json.Marshal(current)
		if err != nil {
			return err
		}
		dst.Write(buf)
	default:
		buf, err := json.Marshal(current)
		if err != nil {
			return err
		}
		dst.Write(buf)
	}
	return nil
}

func orderedJSONEqual(left, right orderedJSONValue) bool {
	leftBytes, err := encodeOrderedFiBJSON(left)
	if err != nil {
		return false
	}
	rightBytes, err := encodeOrderedFiBJSON(right)
	if err != nil {
		return false
	}
	return string(leftBytes) == string(rightBytes)
}

func (o *orderedJSONObject) value(key string) (orderedJSONValue, bool) {
	if o == nil {
		return nil, false
	}
	for _, field := range o.Fields {
		if field.Key == key {
			return field.Value, true
		}
	}
	return nil, false
}

func (o *orderedJSONObject) stringValue(key string) (string, bool) {
	value, ok := o.value(key)
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func (o *orderedJSONObject) objectValue(key string) (*orderedJSONObject, bool) {
	value, ok := o.value(key)
	if !ok {
		return nil, false
	}
	obj, ok := value.(*orderedJSONObject)
	return obj, ok
}

func (o *orderedJSONObject) set(key string, value orderedJSONValue) {
	if o == nil {
		return
	}
	for i := range o.Fields {
		if o.Fields[i].Key == key {
			o.Fields[i].Value = value
			return
		}
	}
	o.Fields = append(o.Fields, orderedJSONField{Key: key, Value: value})
}

func (o *orderedJSONObject) delete(key string) {
	if o == nil {
		return
	}
	for i := range o.Fields {
		if o.Fields[i].Key != key {
			continue
		}
		o.Fields = append(o.Fields[:i], o.Fields[i+1:]...)
		return
	}
}

func assetRegistryScalarSearchValue(raw any) (orderedJSONValue, bool) {
	if text, ok := raw.(string); ok {
		return text, strings.TrimSpace(text) != ""
	}
	if number, ok := jsonNumberFromAny(raw); ok {
		return number, true
	}
	switch value := raw.(type) {
	case nil:
		return nil, false
	case bool:
		return value, true
	case map[string]any:
		if isAssetRegistryTextHistoryValue(value) {
			for _, key := range []string{"cultureInvariantString", "value", "sourceString"} {
				if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
					return text, true
				}
			}
			return nil, false
		}
		if name, ok := value["name"].(string); ok {
			return name, true
		}
		if _, ok := value["enumType"]; ok {
			if enumValue, ok := value["value"].(string); ok {
				return enumValue, true
			}
		}
	}
	return nil, false
}

func assetRegistryContainsPropertySearch(asset *uasset.Asset, propertyName string) (bool, error) {
	if asset == nil {
		return false, fmt.Errorf("asset is nil")
	}
	propertyName = strings.TrimSpace(propertyName)
	if propertyName == "" {
		return false, nil
	}

	section, _, _, err := parseAssetRegistrySection(asset)
	if err != nil {
		return false, err
	}
	if section == nil {
		return false, nil
	}

	for _, obj := range section.Objects {
		for _, tag := range obj.Tags {
			if !strings.EqualFold(tag.Key, assetRegistryTagFindInBlueprintsData) &&
				!strings.EqualFold(tag.Key, assetRegistryTagUnversionedFindInBlueprintsData) {
				continue
			}
			parsed, err := parseFiBData(asset, tag.Value)
			if err != nil {
				return false, fmt.Errorf("parse asset registry tag %s on %s: %w", tag.Key, obj.ObjectPath, err)
			}
			jsonRoot, err := decodeFiBJSON(parsed.JSON)
			if err != nil {
				return false, fmt.Errorf("decode asset registry tag %s on %s: %w", tag.Key, obj.ObjectPath, err)
			}
			editor := newFiBLookupEditor(parsed.Lookup, jsonRoot)
			if _, ok := editor.findPropertyObject(editor.JSONRoot, propertyName); ok {
				return true, nil
			}
		}
	}
	return false, nil
}

func formatAssetRegistryNumber(raw any) (string, bool) {
	switch v := raw.(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case int:
		return strconv.Itoa(v), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case string:
		return strings.TrimSpace(v), v != ""
	default:
		return "", false
	}
}

func shortenEnumPair(oldValue, newValue string) (string, string, bool) {
	oldShort, oldOK := strings.CutPrefix(oldValue, enumPrefix(oldValue))
	newShort, newOK := strings.CutPrefix(newValue, enumPrefix(newValue))
	if !oldOK || !newOK || oldShort == "" || newShort == "" {
		return "", "", false
	}
	return oldShort, newShort, true
}

func enumPrefix(value string) string {
	if idx := strings.LastIndex(value, "::"); idx >= 0 {
		return value[:idx+2]
	}
	return ""
}

func parseAssetRegistrySection(asset *uasset.Asset) (*assetRegistrySectionData, int64, int64, error) {
	if asset == nil {
		return nil, 0, 0, fmt.Errorf("asset is nil")
	}
	sectionStart := int64(asset.Summary.AssetRegistryDataOffset)
	sectionBytes, begin, end, present := sectionByOffset(asset, sectionStart)
	if !present {
		return nil, 0, 0, nil
	}

	reader := uasset.NewByteReaderWithByteSwapping(sectionBytes, asset.Summary.UsesByteSwappedSerialization())
	dependencyOffset, err := reader.ReadInt64()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read asset registry dependency offset: %w", err)
	}
	objectCount, err := reader.ReadInt32()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read asset registry object count: %w", err)
	}
	if objectCount < 0 {
		return nil, 0, 0, fmt.Errorf("invalid asset registry object count: %d", objectCount)
	}

	objects := make([]assetRegistryObjectData, 0, objectCount)
	for i := int32(0); i < objectCount; i++ {
		objectPath, err := reader.ReadFString()
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read asset registry object[%d] path: %w", i, err)
		}
		objectClass, err := reader.ReadFString()
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read asset registry object[%d] class: %w", i, err)
		}
		tagCount, err := reader.ReadInt32()
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read asset registry object[%d] tag count: %w", i, err)
		}
		if tagCount < 0 {
			return nil, 0, 0, fmt.Errorf("invalid asset registry tag count: %d", tagCount)
		}
		tags := make([]assetRegistryTagData, 0, tagCount)
		for tagIdx := int32(0); tagIdx < tagCount; tagIdx++ {
			key, err := reader.ReadFString()
			if err != nil {
				return nil, 0, 0, fmt.Errorf("read asset registry object[%d] tag[%d] key: %w", i, tagIdx, err)
			}
			value, err := reader.ReadFString()
			if err != nil {
				return nil, 0, 0, fmt.Errorf("read asset registry object[%d] tag[%d] value: %w", i, tagIdx, err)
			}
			tags = append(tags, assetRegistryTagData{Key: key, Value: value})
		}
		objects = append(objects, assetRegistryObjectData{
			ObjectPath:  objectPath,
			ObjectClass: objectClass,
			Tags:        tags,
		})
	}

	objectEnd := reader.Offset()
	dependencyStart := objectEnd
	if dependencyOffset > 0 {
		rel := dependencyOffset - begin
		if rel < 0 || rel > int64(len(sectionBytes)) {
			return nil, 0, 0, fmt.Errorf("asset registry dependency offset out of range: %d", dependencyOffset)
		}
		dependencyStart = int(rel)
	} else if candidateStart, ok := findAssetRegistryDependencyStart(sectionBytes, objectEnd, packageByteOrder(asset)); ok {
		dependencyStart = candidateStart
		dependencyOffset = begin + int64(candidateStart)
	}

	boundaryStart := objectEnd
	boundaryEnd := dependencyStart
	if dependencyStart < objectEnd {
		boundaryStart = dependencyStart
		boundaryEnd = objectEnd
	}
	dependencyBoundary := append([]byte(nil), sectionBytes[boundaryStart:boundaryEnd]...)
	dependencyData := append([]byte(nil), sectionBytes[dependencyStart:]...)
	return &assetRegistrySectionData{
		DependencyDataOffset: dependencyOffset,
		DependencyGap:        int64(objectEnd - dependencyStart),
		DependencyBoundary:   dependencyBoundary,
		Objects:              objects,
		DependencyData:       dependencyData,
	}, begin, end, nil
}

func findAssetRegistryDependencyStart(sectionBytes []byte, objectEnd int, order binary.ByteOrder) (int, bool) {
	for _, tailLen := range []int{21, 20, 16} {
		start := len(sectionBytes) - tailLen
		if start < objectEnd || start < 0 || start+4 > len(sectionBytes) {
			continue
		}
		head := order.Uint32(sectionBytes[start : start+4])
		if head > 1<<20 {
			continue
		}
		return start, true
	}
	return 0, false
}

func encodeAssetRegistrySection(asset *uasset.Asset, section *assetRegistrySectionData, sectionStart int64, dependencyOffsetOverride int64, hasDependencyOffsetOverride bool) ([]byte, error) {
	if asset == nil {
		return nil, fmt.Errorf("asset is nil")
	}
	if section == nil {
		return nil, fmt.Errorf("asset registry section is nil")
	}

	order := packageByteOrder(asset)
	out := make([]byte, 0, 256)
	dependencyOffsetPos := len(out)
	out = appendInt64Ordered(out, 0, order)
	out = appendInt32Ordered(out, int32(len(section.Objects)), order)

	for _, obj := range section.Objects {
		out = appendFStringOrdered(out, obj.ObjectPath, order)
		out = appendFStringOrdered(out, obj.ObjectClass, order)
		out = appendInt32Ordered(out, int32(len(obj.Tags)), order)
		for _, tag := range obj.Tags {
			out = appendFStringOrdered(out, tag.Key, order)
			out = appendFStringOrdered(out, tag.Value, order)
		}
	}

	targetRel := len(out)
	dependencyOffset := sectionStart + int64(targetRel)
	preserveExistingLayout := hasDependencyOffsetOverride || section.DependencyGap != 0 || len(section.DependencyBoundary) > 0
	if hasDependencyOffsetOverride {
		rel := dependencyOffsetOverride - sectionStart
		if rel < 0 || rel > int64(^uint(0)>>1) {
			return nil, fmt.Errorf("asset registry dependency offset relative position out of range: %d", rel)
		}
		targetRel = int(rel)
		dependencyOffset = dependencyOffsetOverride
	} else if preserveExistingLayout {
		targetRel = len(out) - int(section.DependencyGap)
		if targetRel < 0 || targetRel > len(out)+len(section.DependencyBoundary) {
			return nil, fmt.Errorf("asset registry dependency offset relative position out of range: %d", targetRel)
		}
		dependencyOffset = sectionStart + int64(targetRel)
	}
	if !preserveExistingLayout {
		out = append(out, section.DependencyData...)
	} else {
		switch {
		case len(out) < targetRel:
			gapLen := targetRel - len(out)
			if gapLen > len(section.DependencyBoundary) {
				out = append(out, section.DependencyBoundary...)
				out = append(out, make([]byte, gapLen-len(section.DependencyBoundary))...)
			} else {
				out = append(out, section.DependencyBoundary[:gapLen]...)
			}
			out = append(out, section.DependencyData...)
		case len(out) > targetRel:
			overlap := len(out) - targetRel
			if overlap > len(section.DependencyData) {
				return nil, fmt.Errorf("asset registry dependency overlap out of range: overlap=%d data=%d", overlap, len(section.DependencyData))
			}
			out = append(out, section.DependencyData[overlap:]...)
		default:
			out = append(out, section.DependencyData...)
		}
	}
	if dependencyOffsetPos+8 > len(out) {
		return nil, fmt.Errorf("asset registry dependency offset patch out of range")
	}
	order.PutUint64(out[dependencyOffsetPos:dependencyOffsetPos+8], uint64(dependencyOffset))
	return out, nil
}

func patchAssetRegistryDependencyOffsetField(raw []byte, asset *uasset.Asset, dependencyOffset int64) error {
	if asset == nil {
		return fmt.Errorf("asset is nil")
	}
	fieldPos := int(asset.Summary.AssetRegistryDataOffset)
	if fieldPos <= 0 || fieldPos+8 > len(raw) {
		return fmt.Errorf("asset registry dependency offset field out of bounds: %d", asset.Summary.AssetRegistryDataOffset)
	}
	order := packageByteOrder(asset)
	order.PutUint64(raw[fieldPos:fieldPos+8], uint64(dependencyOffset))
	return nil
}

func rewritePlainAssetRegistryTagValue(value string, mutator func(history map[string]any) int) (string, int) {
	history := map[string]any{
		"historyType":               "None",
		"historyTypeCode":           int32(255),
		"hasCultureInvariantString": true,
		"cultureInvariantString":    value,
		"value":                     value,
	}
	count := mutator(history)
	if count == 0 {
		return value, 0
	}
	if cultureInvariant, ok := history["cultureInvariantString"].(string); ok {
		return cultureInvariant, count
	}
	if out, ok := history["value"].(string); ok {
		return out, count
	}
	return value, 0
}

func replaceHistoryStrings(history map[string]any, from, to string) int {
	if history == nil || strings.TrimSpace(from) == "" || from == to {
		return 0
	}
	changed := 0
	for _, field := range []string{"namespace", "key", "sourceString", "cultureInvariantString", "value", "tableIdName"} {
		raw, ok := history[field]
		if !ok {
			continue
		}
		current, ok := raw.(string)
		if !ok {
			continue
		}
		replaced, count := replaceAllWithCount(current, from, to)
		if count == 0 {
			continue
		}
		history[field] = replaced
		changed += count
	}
	return changed
}

func rewriteFiBTagValue(asset *uasset.Asset, value string, mutator func(history map[string]any) int) (string, int, error) {
	parsed, err := parseFiBData(asset, value)
	if err != nil {
		return "", 0, err
	}

	total := 0
	changed := false
	for i := range parsed.Lookup {
		entry := &parsed.Lookup[i]
		count := entry.Text.rewrite(mutator)
		if count > 0 {
			total += count
			changed = true
		}
	}
	if !changed {
		return value, 0, nil
	}

	encoded, err := encodeFiBData(parsed)
	if err != nil {
		return "", 0, err
	}
	return encoded, total, nil
}

func parseFiBData(asset *uasset.Asset, value string) (*fibEncodedData, error) {
	runes := []rune(value)
	if len(runes) < 8 {
		return nil, fmt.Errorf("FiBData is too short")
	}

	versionBytes, err := ueEncodedRunesToBytes(runes[:4])
	if err != nil {
		return nil, err
	}
	version := int32(binary.LittleEndian.Uint32(versionBytes))

	sizeBytes, err := ueEncodedRunesToBytes(runes[4:8])
	if err != nil {
		return nil, err
	}
	lookupLen := int(binary.LittleEndian.Uint32(sizeBytes))
	if lookupLen < 0 || 8+lookupLen > len(runes) {
		return nil, fmt.Errorf("FiBData lookup length out of range: %d", lookupLen)
	}

	lookupBytes, err := ueEncodedRunesToBytes(runes[8 : 8+lookupLen])
	if err != nil {
		return nil, err
	}
	lookup, err := parseFiBLookupTable(asset, lookupBytes)
	if err != nil {
		return nil, err
	}

	return &fibEncodedData{
		Version: version,
		Lookup:  lookup,
		JSON:    string(runes[8+lookupLen:]),
	}, nil
}

func encodeFiBData(data *fibEncodedData) (string, error) {
	if data == nil {
		return "", fmt.Errorf("FiBData is nil")
	}

	lookupBytes, err := encodeFiBLookupTable(data.Lookup)
	if err != nil {
		return "", err
	}
	versionEncoded := ueBytesToEncodedString(appendInt32Ordered(nil, data.Version, binary.LittleEndian))
	sizeEncoded := ueBytesToEncodedString(appendInt32Ordered(nil, int32(len(lookupBytes)), binary.LittleEndian))
	lookupEncoded := ueBytesToEncodedString(lookupBytes)
	return versionEncoded + sizeEncoded + lookupEncoded + data.JSON, nil
}

func parseFiBLookupTable(asset *uasset.Asset, data []byte) ([]fibLookupEntry, error) {
	reader := uasset.NewByteReader(data)
	count, err := reader.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read FiB lookup count: %w", err)
	}
	if count < 0 {
		return nil, fmt.Errorf("invalid FiB lookup count: %d", count)
	}
	entries := make([]fibLookupEntry, 0, count)
	for i := int32(0); i < count; i++ {
		key, err := reader.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("read FiB lookup key[%d]: %w", i, err)
		}
		text, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return nil, fmt.Errorf("read FiB lookup text[%d]: %w", i, err)
		}
		entries = append(entries, fibLookupEntry{Key: key, Text: text})
	}
	if reader.Remaining() != 0 {
		return nil, fmt.Errorf("FiB lookup trailing bytes: %d", reader.Remaining())
	}
	return entries, nil
}

func encodeFiBLookupTable(entries []fibLookupEntry) ([]byte, error) {
	out := appendInt32Ordered(nil, int32(len(entries)), binary.LittleEndian)
	for _, entry := range entries {
		out = appendInt32Ordered(out, entry.Key, binary.LittleEndian)
		textBytes, err := encodeFiBLookupText(entry.Text)
		if err != nil {
			return nil, err
		}
		out = append(out, textBytes...)
	}
	return out, nil
}

func parseFiBLookupText(reader *uasset.ByteReader, asset *uasset.Asset, data []byte) (fibLookupText, error) {
	start := reader.Offset()
	flags, err := reader.ReadInt32()
	if err != nil {
		return fibLookupText{}, err
	}
	historyType, err := reader.ReadUint8()
	if err != nil {
		return fibLookupText{}, err
	}

	text := fibLookupText{
		Flags:           flags,
		HistoryTypeCode: historyType,
	}
	if asset != nil {
		text.Names = asset.Names
	}
	switch historyType {
	case 255:
		hasInvariant, err := reader.ReadUBool()
		if err != nil {
			return fibLookupText{}, err
		}
		text.HasCultureInvariantValue = hasInvariant
		if hasInvariant {
			value, err := reader.ReadFString()
			if err != nil {
				return fibLookupText{}, err
			}
			text.CultureInvariantString = value
		}
	case 0:
		namespace, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		key, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		source, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.Namespace = namespace
		text.Key = key
		text.SourceString = source
	case 1:
		formatText, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return fibLookupText{}, err
		}
		argCount, err := reader.ReadInt32()
		if err != nil {
			return fibLookupText{}, err
		}
		if argCount < 0 {
			return fibLookupText{}, fmt.Errorf("invalid FiB named argument count: %d", argCount)
		}
		text.FormatText = &formatText
		text.NamedArguments = make([]fibNamedArgument, 0, argCount)
		for i := int32(0); i < argCount; i++ {
			name, err := reader.ReadFString()
			if err != nil {
				return fibLookupText{}, err
			}
			value, err := parseFiBFormatArgumentValue(reader, asset, data)
			if err != nil {
				return fibLookupText{}, fmt.Errorf("read named argument[%d]: %w", i, err)
			}
			text.NamedArguments = append(text.NamedArguments, fibNamedArgument{Name: name, Value: value})
		}
	case 2:
		formatText, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return fibLookupText{}, err
		}
		argCount, err := reader.ReadInt32()
		if err != nil {
			return fibLookupText{}, err
		}
		if argCount < 0 {
			return fibLookupText{}, fmt.Errorf("invalid FiB ordered argument count: %d", argCount)
		}
		text.FormatText = &formatText
		text.OrderedArguments = make([]fibFormatArgumentValue, 0, argCount)
		for i := int32(0); i < argCount; i++ {
			value, err := parseFiBFormatArgumentValue(reader, asset, data)
			if err != nil {
				return fibLookupText{}, fmt.Errorf("read ordered argument[%d]: %w", i, err)
			}
			text.OrderedArguments = append(text.OrderedArguments, value)
		}
	case 3:
		formatText, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return fibLookupText{}, err
		}
		argCount, err := reader.ReadInt32()
		if err != nil {
			return fibLookupText{}, err
		}
		if argCount < 0 {
			return fibLookupText{}, fmt.Errorf("invalid FiB argument-data count: %d", argCount)
		}
		text.FormatText = &formatText
		text.ArgumentData = make([]fibArgumentData, 0, argCount)
		for i := int32(0); i < argCount; i++ {
			value, err := parseFiBArgumentData(reader, asset, data)
			if err != nil {
				return fibLookupText{}, fmt.Errorf("read argument-data[%d]: %w", i, err)
			}
			text.ArgumentData = append(text.ArgumentData, value)
		}
	case 4, 5:
		sourceValue, err := parseFiBFormatArgumentValue(reader, asset, data)
		if err != nil {
			return fibLookupText{}, err
		}
		hasFormatOptions, err := reader.ReadUBool()
		if err != nil {
			return fibLookupText{}, err
		}
		text.SourceValue = &sourceValue
		text.HasFormatOptions = hasFormatOptions
		if hasFormatOptions {
			formatOptionsRaw, err := reader.ReadBytes(25)
			if err != nil {
				return fibLookupText{}, err
			}
			text.FormatOptionsRaw = append([]byte(nil), formatOptionsRaw...)
		}
		targetCulture, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.TargetCulture = targetCulture
	case 6:
		currencyCode, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		sourceValue, err := parseFiBFormatArgumentValue(reader, asset, data)
		if err != nil {
			return fibLookupText{}, err
		}
		hasFormatOptions, err := reader.ReadUBool()
		if err != nil {
			return fibLookupText{}, err
		}
		text.CurrencyCode = currencyCode
		text.SourceValue = &sourceValue
		text.HasFormatOptions = hasFormatOptions
		if hasFormatOptions {
			formatOptionsRaw, err := reader.ReadBytes(25)
			if err != nil {
				return fibLookupText{}, err
			}
			text.FormatOptionsRaw = append([]byte(nil), formatOptionsRaw...)
		}
		targetCulture, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.TargetCulture = targetCulture
	case 7:
		sourceDateTimeTicks, err := reader.ReadInt64()
		if err != nil {
			return fibLookupText{}, err
		}
		dateStyleCode, err := reader.ReadUint8()
		if err != nil {
			return fibLookupText{}, err
		}
		timeZone, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		targetCulture, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.SourceDateTimeTicks = sourceDateTimeTicks
		text.DateStyleCode = dateStyleCode
		text.TimeZone = timeZone
		text.TargetCulture = targetCulture
	case 8:
		sourceDateTimeTicks, err := reader.ReadInt64()
		if err != nil {
			return fibLookupText{}, err
		}
		timeStyleCode, err := reader.ReadUint8()
		if err != nil {
			return fibLookupText{}, err
		}
		timeZone, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		targetCulture, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.SourceDateTimeTicks = sourceDateTimeTicks
		text.TimeStyleCode = timeStyleCode
		text.TimeZone = timeZone
		text.TargetCulture = targetCulture
	case 9:
		sourceDateTimeTicks, err := reader.ReadInt64()
		if err != nil {
			return fibLookupText{}, err
		}
		dateStyleCode, err := reader.ReadUint8()
		if err != nil {
			return fibLookupText{}, err
		}
		timeStyleCode, err := reader.ReadUint8()
		if err != nil {
			return fibLookupText{}, err
		}
		text.SourceDateTimeTicks = sourceDateTimeTicks
		text.DateStyleCode = dateStyleCode
		text.TimeStyleCode = timeStyleCode
		if int8(dateStyleCode) == 5 {
			customPattern, err := reader.ReadFString()
			if err != nil {
				return fibLookupText{}, err
			}
			text.CustomPattern = customPattern
		}
		timeZone, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		targetCulture, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.TimeZone = timeZone
		text.TargetCulture = targetCulture
	case 10:
		sourceText, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return fibLookupText{}, err
		}
		transformTypeCode, err := reader.ReadUint8()
		if err != nil {
			return fibLookupText{}, err
		}
		text.SourceText = &sourceText
		text.TransformTypeCode = transformTypeCode
	case 11:
		nameCount := 1 << 30
		if asset != nil {
			nameCount = len(asset.Names)
		}
		tableID, err := reader.ReadNameRef(nameCount)
		if err != nil {
			return fibLookupText{}, err
		}
		key, err := reader.ReadFString()
		if err != nil {
			return fibLookupText{}, err
		}
		text.TableID = tableID
		text.Key = key
		if asset != nil {
			text.TableIDName = tableID.Display(asset.Names)
		}
	case 12:
		nameCount := 1 << 30
		if asset != nil {
			nameCount = len(asset.Names)
		}
		generatorType, err := reader.ReadNameRef(nameCount)
		if err != nil {
			return fibLookupText{}, err
		}
		text.GeneratorType = generatorType
		if asset != nil {
			text.GeneratorTypeName = generatorType.Display(asset.Names)
		}
		isNone := asset != nil && generatorType.IsNone(asset.Names)
		if !isNone {
			count, err := reader.ReadInt32()
			if err != nil {
				return fibLookupText{}, err
			}
			if count < 0 {
				return fibLookupText{}, fmt.Errorf("invalid FiB generator payload size: %d", count)
			}
			payload, err := reader.ReadBytes(int(count))
			if err != nil {
				return fibLookupText{}, err
			}
			text.GeneratorPayload = append([]byte(nil), payload...)
		}
	default:
		return fibLookupText{}, fmt.Errorf("unsupported FiB text history type: %d", historyType)
	}
	text.Raw = append([]byte(nil), data[start:reader.Offset()]...)
	return text, nil
}

func encodeFiBLookupText(text fibLookupText) ([]byte, error) {
	out := appendInt32Ordered(nil, text.Flags, binary.LittleEndian)
	out = append(out, text.HistoryTypeCode)
	switch text.HistoryTypeCode {
	case 255:
		if text.HasCultureInvariantValue {
			out = appendUBoolOrdered(out, true, binary.LittleEndian)
			out = appendFStringOrdered(out, text.CultureInvariantString, binary.LittleEndian)
		} else {
			out = appendUBoolOrdered(out, false, binary.LittleEndian)
		}
	case 0:
		out = appendTextKeyStringOrdered(out, text.Namespace, binary.LittleEndian)
		out = appendTextKeyStringOrdered(out, text.Key, binary.LittleEndian)
		out = appendFStringOrdered(out, text.SourceString, binary.LittleEndian)
	case 1:
		if text.FormatText == nil {
			return nil, fmt.Errorf("FiB NamedFormat missing format text")
		}
		formatBytes, err := encodeFiBLookupText(*text.FormatText)
		if err != nil {
			return nil, err
		}
		out = append(out, formatBytes...)
		out = appendInt32Ordered(out, int32(len(text.NamedArguments)), binary.LittleEndian)
		for _, arg := range text.NamedArguments {
			out = appendFStringOrdered(out, arg.Name, binary.LittleEndian)
			argBytes, err := encodeFiBFormatArgumentValue(arg.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, argBytes...)
		}
	case 2:
		if text.FormatText == nil {
			return nil, fmt.Errorf("FiB OrderedFormat missing format text")
		}
		formatBytes, err := encodeFiBLookupText(*text.FormatText)
		if err != nil {
			return nil, err
		}
		out = append(out, formatBytes...)
		out = appendInt32Ordered(out, int32(len(text.OrderedArguments)), binary.LittleEndian)
		for _, arg := range text.OrderedArguments {
			argBytes, err := encodeFiBFormatArgumentValue(arg)
			if err != nil {
				return nil, err
			}
			out = append(out, argBytes...)
		}
	case 3:
		if text.FormatText == nil {
			return nil, fmt.Errorf("FiB ArgumentFormat missing format text")
		}
		formatBytes, err := encodeFiBLookupText(*text.FormatText)
		if err != nil {
			return nil, err
		}
		out = append(out, formatBytes...)
		out = appendInt32Ordered(out, int32(len(text.ArgumentData)), binary.LittleEndian)
		for _, arg := range text.ArgumentData {
			argBytes, err := encodeFiBArgumentData(arg)
			if err != nil {
				return nil, err
			}
			out = append(out, argBytes...)
		}
	case 4, 5:
		if text.SourceValue == nil {
			return nil, fmt.Errorf("FiB %s missing source value", fibHistoryTypeName(text.HistoryTypeCode))
		}
		sourceBytes, err := encodeFiBFormatArgumentValue(*text.SourceValue)
		if err != nil {
			return nil, err
		}
		out = append(out, sourceBytes...)
		out = appendUBoolOrdered(out, text.HasFormatOptions, binary.LittleEndian)
		if text.HasFormatOptions {
			if len(text.FormatOptionsRaw) != 25 {
				return nil, fmt.Errorf("FiB %s format options size: got %d want 25", fibHistoryTypeName(text.HistoryTypeCode), len(text.FormatOptionsRaw))
			}
			out = append(out, text.FormatOptionsRaw...)
		}
		out = appendFStringOrdered(out, text.TargetCulture, binary.LittleEndian)
	case 6:
		if text.SourceValue == nil {
			return nil, fmt.Errorf("FiB AsCurrency missing source value")
		}
		out = appendFStringOrdered(out, text.CurrencyCode, binary.LittleEndian)
		sourceBytes, err := encodeFiBFormatArgumentValue(*text.SourceValue)
		if err != nil {
			return nil, err
		}
		out = append(out, sourceBytes...)
		out = appendUBoolOrdered(out, text.HasFormatOptions, binary.LittleEndian)
		if text.HasFormatOptions {
			if len(text.FormatOptionsRaw) != 25 {
				return nil, fmt.Errorf("FiB AsCurrency format options size: got %d want 25", len(text.FormatOptionsRaw))
			}
			out = append(out, text.FormatOptionsRaw...)
		}
		out = appendFStringOrdered(out, text.TargetCulture, binary.LittleEndian)
	case 7:
		out = appendInt64Ordered(out, text.SourceDateTimeTicks, binary.LittleEndian)
		out = append(out, text.DateStyleCode)
		out = appendFStringOrdered(out, text.TimeZone, binary.LittleEndian)
		out = appendFStringOrdered(out, text.TargetCulture, binary.LittleEndian)
	case 8:
		out = appendInt64Ordered(out, text.SourceDateTimeTicks, binary.LittleEndian)
		out = append(out, text.TimeStyleCode)
		out = appendFStringOrdered(out, text.TimeZone, binary.LittleEndian)
		out = appendFStringOrdered(out, text.TargetCulture, binary.LittleEndian)
	case 9:
		out = appendInt64Ordered(out, text.SourceDateTimeTicks, binary.LittleEndian)
		out = append(out, text.DateStyleCode)
		out = append(out, text.TimeStyleCode)
		if int8(text.DateStyleCode) == 5 {
			out = appendFStringOrdered(out, text.CustomPattern, binary.LittleEndian)
		}
		out = appendFStringOrdered(out, text.TimeZone, binary.LittleEndian)
		out = appendFStringOrdered(out, text.TargetCulture, binary.LittleEndian)
	case 10:
		if text.SourceText == nil {
			return nil, fmt.Errorf("FiB Transform missing source text")
		}
		sourceBytes, err := encodeFiBLookupText(*text.SourceText)
		if err != nil {
			return nil, err
		}
		out = append(out, sourceBytes...)
		out = append(out, text.TransformTypeCode)
	case 11:
		out = append(out, encodeNameRef(text.TableID, binary.LittleEndian)...)
		out = appendFStringOrdered(out, text.Key, binary.LittleEndian)
	case 12:
		out = append(out, encodeNameRef(text.GeneratorType, binary.LittleEndian)...)
		isNone := len(text.Names) > 0 && text.GeneratorType.IsNone(text.Names)
		if !isNone {
			out = appendInt32Ordered(out, int32(len(text.GeneratorPayload)), binary.LittleEndian)
			out = append(out, text.GeneratorPayload...)
		}
	default:
		return nil, fmt.Errorf("unsupported FiB text history type: %d", text.HistoryTypeCode)
	}
	return out, nil
}

func parseFiBFormatArgumentValue(reader *uasset.ByteReader, asset *uasset.Asset, data []byte) (fibFormatArgumentValue, error) {
	start := reader.Offset()
	typeCode, err := reader.ReadUint8()
	if err != nil {
		return fibFormatArgumentValue{}, err
	}

	arg := fibFormatArgumentValue{TypeCode: typeCode}
	switch typeCode {
	case 0, 1, 3, 5:
		if _, err := reader.ReadBytes(8); err != nil {
			return fibFormatArgumentValue{}, err
		}
	case 2:
		if _, err := reader.ReadBytes(4); err != nil {
			return fibFormatArgumentValue{}, err
		}
	case 4:
		text, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return fibFormatArgumentValue{}, err
		}
		arg.Text = &text
	default:
		return fibFormatArgumentValue{}, fmt.Errorf("unsupported FiB format argument type: %d", typeCode)
	}
	arg.Raw = append([]byte(nil), data[start:reader.Offset()]...)
	return arg, nil
}

func encodeFiBFormatArgumentValue(arg fibFormatArgumentValue) ([]byte, error) {
	if arg.TypeCode != 4 {
		if len(arg.Raw) == 0 {
			return nil, fmt.Errorf("FiB format argument type %d missing raw bytes", arg.TypeCode)
		}
		return append([]byte(nil), arg.Raw...), nil
	}
	if arg.Text == nil {
		if len(arg.Raw) != 0 {
			return append([]byte(nil), arg.Raw...), nil
		}
		return nil, fmt.Errorf("FiB Text argument missing text payload")
	}
	textBytes, err := encodeFiBLookupText(*arg.Text)
	if err != nil {
		return nil, err
	}
	out := []byte{arg.TypeCode}
	out = append(out, textBytes...)
	return out, nil
}

func parseFiBArgumentData(reader *uasset.ByteReader, asset *uasset.Asset, data []byte) (fibArgumentData, error) {
	start := reader.Offset()
	name, err := reader.ReadFString()
	if err != nil {
		return fibArgumentData{}, err
	}
	typeCode, err := reader.ReadUint8()
	if err != nil {
		return fibArgumentData{}, err
	}

	arg := fibArgumentData{Name: name, ValueTypeCode: typeCode}
	switch typeCode {
	case 0, 3:
		if _, err := reader.ReadBytes(8); err != nil {
			return fibArgumentData{}, err
		}
	case 2:
		if _, err := reader.ReadBytes(4); err != nil {
			return fibArgumentData{}, err
		}
	case 4:
		text, err := parseFiBLookupText(reader, asset, data)
		if err != nil {
			return fibArgumentData{}, err
		}
		arg.Text = &text
	case 5:
		if _, err := reader.ReadUint8(); err != nil {
			return fibArgumentData{}, err
		}
	default:
		return fibArgumentData{}, fmt.Errorf("unsupported FiB argument-data type: %d", typeCode)
	}
	arg.Raw = append([]byte(nil), data[start:reader.Offset()]...)
	return arg, nil
}

func encodeFiBArgumentData(arg fibArgumentData) ([]byte, error) {
	if arg.ValueTypeCode != 4 {
		if len(arg.Raw) == 0 {
			return nil, fmt.Errorf("FiB argument-data type %d missing raw bytes", arg.ValueTypeCode)
		}
		return append([]byte(nil), arg.Raw...), nil
	}
	if arg.Text == nil {
		if len(arg.Raw) != 0 {
			return append([]byte(nil), arg.Raw...), nil
		}
		return nil, fmt.Errorf("FiB argument-data Text missing text payload")
	}
	textBytes, err := encodeFiBLookupText(*arg.Text)
	if err != nil {
		return nil, err
	}
	out := appendFStringOrdered(nil, arg.Name, binary.LittleEndian)
	out = append(out, arg.ValueTypeCode)
	out = append(out, textBytes...)
	return out, nil
}

func fibHistoryTypeName(code uint8) string {
	switch code {
	case 0:
		return "Base"
	case 1:
		return "NamedFormat"
	case 2:
		return "OrderedFormat"
	case 3:
		return "ArgumentFormat"
	case 4:
		return "AsNumber"
	case 5:
		return "AsPercent"
	case 6:
		return "AsCurrency"
	case 7:
		return "AsDate"
	case 8:
		return "AsTime"
	case 9:
		return "AsDateTime"
	case 10:
		return "Transform"
	case 11:
		return "StringTableEntry"
	case 12:
		return "TextGenerator"
	case 255:
		return "None"
	default:
		return fmt.Sprintf("Unknown(%d)", code)
	}
}

func appendTextKeyStringOrdered(dst []byte, s string, order binary.ByteOrder) []byte {
	if s != "" {
		return appendFStringOrdered(dst, s, order)
	}
	dst = appendInt32Ordered(dst, 1, order)
	return append(dst, 0)
}

func (t fibLookupText) historyMap() (map[string]any, bool) {
	switch t.HistoryTypeCode {
	case 255:
		history := map[string]any{
			"flags":                     t.Flags,
			"historyType":               "None",
			"historyTypeCode":           int32(255),
			"hasCultureInvariantString": t.HasCultureInvariantValue,
		}
		if t.HasCultureInvariantValue {
			history["cultureInvariantString"] = t.CultureInvariantString
			history["value"] = t.CultureInvariantString
		}
		return history, true
	case 0:
		return map[string]any{
			"flags":                  t.Flags,
			"historyType":            "Base",
			"historyTypeCode":        int32(0),
			"namespace":              t.Namespace,
			"key":                    t.Key,
			"sourceString":           t.SourceString,
			"cultureInvariantString": t.SourceString,
			"value":                  t.SourceString,
		}, true
	case 11:
		history := map[string]any{
			"flags":           t.Flags,
			"historyType":     "StringTableEntry",
			"historyTypeCode": int32(11),
			"key":             t.Key,
		}
		if t.TableIDName != "" {
			history["tableIdName"] = t.TableIDName
		}
		return history, true
	default:
		return nil, false
	}
}

func (t *fibLookupText) applyHistoryMap(history map[string]any) bool {
	if t == nil || history == nil {
		return false
	}
	changed := false
	switch t.HistoryTypeCode {
	case 255:
		if raw, ok := history["hasCultureInvariantString"]; ok {
			if hasInvariant, ok := raw.(bool); ok && hasInvariant != t.HasCultureInvariantValue {
				t.HasCultureInvariantValue = hasInvariant
				changed = true
			}
		}
		if raw, ok := history["cultureInvariantString"]; ok {
			if value, ok := raw.(string); ok && value != t.CultureInvariantString {
				t.CultureInvariantString = value
				t.HasCultureInvariantValue = true
				changed = true
			}
		} else if raw, ok := history["value"]; ok {
			if value, ok := raw.(string); ok && value != t.CultureInvariantString {
				t.CultureInvariantString = value
				t.HasCultureInvariantValue = true
				changed = true
			}
		}
	case 0:
		if raw, ok := history["namespace"]; ok {
			if value, ok := raw.(string); ok && value != t.Namespace {
				t.Namespace = value
				changed = true
			}
		}
		if raw, ok := history["key"]; ok {
			if value, ok := raw.(string); ok && value != t.Key {
				t.Key = value
				changed = true
			}
		}
		if raw, ok := history["sourceString"]; ok {
			if value, ok := raw.(string); ok && value != t.SourceString {
				t.SourceString = value
				changed = true
			}
		} else if raw, ok := history["cultureInvariantString"]; ok {
			if value, ok := raw.(string); ok && value != t.SourceString {
				t.SourceString = value
				changed = true
			}
		} else if raw, ok := history["value"]; ok {
			if value, ok := raw.(string); ok && value != t.SourceString {
				t.SourceString = value
				changed = true
			}
		}
	case 11:
		if raw, ok := history["key"]; ok {
			if value, ok := raw.(string); ok && value != t.Key {
				t.Key = value
				changed = true
			}
		}
		if raw, ok := history["tableIdName"]; ok {
			if value, ok := raw.(string); ok && value != "" && value != t.TableIDName && len(t.Names) != 0 {
				ref, err := resolveDisplayNameRef(t.Names, value)
				if err == nil {
					t.TableID = ref
					t.TableIDName = value
					changed = true
				}
			}
		}
	}
	return changed
}

func (t *fibLookupText) rewrite(mutator func(history map[string]any) int) int {
	if t == nil || mutator == nil {
		return 0
	}

	total := 0
	if history, ok := t.historyMap(); ok {
		if count := mutator(history); count > 0 && t.applyHistoryMap(history) {
			total += count
		}
	}

	switch t.HistoryTypeCode {
	case 1:
		if t.FormatText != nil {
			total += t.FormatText.rewrite(mutator)
		}
		for i := range t.NamedArguments {
			total += t.NamedArguments[i].Value.rewrite(mutator)
		}
	case 2:
		if t.FormatText != nil {
			total += t.FormatText.rewrite(mutator)
		}
		for i := range t.OrderedArguments {
			total += t.OrderedArguments[i].rewrite(mutator)
		}
	case 3:
		if t.FormatText != nil {
			total += t.FormatText.rewrite(mutator)
		}
		for i := range t.ArgumentData {
			total += t.ArgumentData[i].rewrite(mutator)
		}
	case 4, 5, 6:
		if t.SourceValue != nil {
			total += t.SourceValue.rewrite(mutator)
		}
	case 10:
		if t.SourceText != nil {
			total += t.SourceText.rewrite(mutator)
		}
	}
	return total
}

func (v *fibFormatArgumentValue) rewrite(mutator func(history map[string]any) int) int {
	if v == nil || v.Text == nil {
		return 0
	}
	return v.Text.rewrite(mutator)
}

func (v *fibArgumentData) rewrite(mutator func(history map[string]any) int) int {
	if v == nil || v.Text == nil {
		return 0
	}
	return v.Text.rewrite(mutator)
}

func ueBytesToEncodedString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b) + 1
	}
	return string(runes)
}

func ueEncodedRunesToBytes(runes []rune) ([]byte, error) {
	out := make([]byte, len(runes))
	for i, r := range runes {
		if r <= 0 || r > 256 {
			return nil, fmt.Errorf("encoded FiB rune out of range: %d", r)
		}
		out[i] = byte(r - 1)
	}
	return out, nil
}

func appendInt64Ordered(dst []byte, v int64, order binary.ByteOrder) []byte {
	buf := make([]byte, 8)
	order.PutUint64(buf, uint64(v))
	return append(dst, buf...)
}
