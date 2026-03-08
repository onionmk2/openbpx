# UE 5.6/5.7 Implementation Notes (Verified Against Engine Source)

Verified against UE source under `<UE_SOURCE_ROOT>`.

## Verified Baseline (moved from README)

- `PACKAGE_FILE_TAG = 0x9E2A83C1` / `PACKAGE_FILE_TAG_SWAPPED = 0xC1832A9E`
  - `Runtime/Core/Public/UObject/ObjectVersion.h`
- `EUnrealEngineObjectUE5Version` range is `1000..1018`
  - `Runtime/Core/Public/UObject/ObjectVersion.h`
- Core serialization order of `FPackageFileSummary` (SavedHash, Name/Import/Export, MetaData, DataResource, etc.)
  - `Runtime/CoreUObject/Private/UObject/PackageFileSummary.cpp`
- Serialization order and condition branches for `FObjectImport` / `FObjectExport`
  - `Runtime/CoreUObject/Private/UObject/ObjectResource.cpp`
- `FObjectExport.ScriptSerializationStart/EndOffset` is serialized only when
  `!UseUnversionedPropertySerialization() && UEVer >= SCRIPT_SERIALIZATION_OFFSET`
  - `Runtime/CoreUObject/Private/UObject/ObjectResource.cpp` (`operator<<(FStructuredArchive::FSlot, FObjectExport&)`)
  - `Runtime/CoreUObject/Private/UObject/SavePackage2.cpp` normalizes by subtracting `SerialOffset`
    (`ScriptSerialization*Offset -= SerialOffset`)
- `UseUnversionedPropertySerialization` is decided from `PKG_UnversionedProperties` at load time
  - `Runtime/CoreUObject/Private/UObject/LinkerLoad.cpp` (`SetUseUnversionedPropertySerialization`)
- Main fields of `FBPVariableDescription`
  - `Runtime/Engine/Classes/Engine/Blueprint.h`
- Main fields of `FEdGraphPinType`
  - `Runtime/Engine/Classes/EdGraph/EdGraphPin.h`

## Corrections vs Older Notes (drift prevention)

- One NameMap entry is not `bIsWide` + bytes; it is serialized as `FString` (sign indicates wide) + two hashes
  - `Runtime/Core/Private/UObject/UnrealNames.cpp` (`operator<<(FNameEntrySerialized&)`)
- In UE 5.6 (`PROPERTY_TAG_COMPLETE_TYPE_NAME` onward), `FPropertyTag` is primarily driven by `TypeName + Flags`, not legacy type-specific field lists
  - `Runtime/CoreUObject/Private/UObject/PropertyTag.cpp`
- Modern `FSoftObjectPath` is `FTopLevelAssetPath + SubPath`, but includes old-version/custom-version compatibility branches
  - `Runtime/CoreUObject/Private/UObject/SoftObjectPath.cpp`
- `LegacyUE3Version=864` is a common saved value, not a universal invariant
- `FString` compatibility logic treats `SaveNum == MIN_int32` as corrupted input
  - `Runtime/Core/Private/Internationalization/TextKey.cpp` (`LoadKeyString`)
- `UDataTable::LoadStructData` / `SaveStructData` uses `FStructuredArchiveArray` with leading `NumRows`
  - `Runtime/Engine/Private/DataTable.cpp`

## Additional Findings from 2026-02-28 Audit

- `FPropertyTag` uses legacy format (`LoadPropertyTagNoFullType`) when `UEVer < PROPERTY_TAG_COMPLETE_TYPE_NAME(1012)`
  - `Runtime/CoreUObject/Private/UObject/PropertyTag.cpp` (`operator<<(FStructuredArchive::FSlot, FPropertyTag&)`)
  - BPX policy: parser targets UE 5.6 `TypeName` node format; non-strict legacy reads should emit explicit misread-prevention warnings.

- `FPropertyTag` flags (`EPropertyTagFlags`) and extension flags (`EPropertyTagExtension`)
  - `HasArrayIndex=0x01`, `HasPropertyGuid=0x02`, `HasPropertyExtensions=0x04`, `HasBinaryOrNativeSerialize=0x08`, `BoolTrue=0x10`, `SkippedSerialize=0x20`
  - `OverridableInformation=0x02`
  - Reference: `Runtime/CoreUObject/Private/UObject/PropertyTag.cpp`

- Class-level vs property-level overridable extensions are asymmetric:
  - class-level (`EClassSerializationControlExtension::OverridableSerializationInformation`) stores only `OverridableOperation (uint8)`
    - `Runtime/CoreUObject/Private/UObject/Class.cpp` (`UStruct::SerializeVersionedTaggedProperties`)
  - property-level (`EPropertyTagExtension::OverridableInformation`) stores
    `OverriddenPropertyOperation (uint8)` + `ExperimentalOverridableLogic (bool)`
    - `Runtime/CoreUObject/Private/UObject/PropertyTag.cpp` (`SerializePropertyExtensions`)

- Conditional fields in `FPackageFileSummary`:
  - `LocalizationID` omitted when `PKG_FilterEditorOnly`
  - `PersistentGUID` omitted when `PKG_FilterEditorOnly`
  - `MetaDataOffset` added at `>= 1014 (PROPERTY_TAG_EXTENSION_AND_OVERRIDABLE_SERIALIZATION)`
  - `CellExport/CellImport` added at `>= 1015 (VERSE_CELLS)`
  - Reference: `Runtime/CoreUObject/Private/UObject/PackageFileSummary.cpp`

- `FSoftObjectPath` still carries legacy compatibility branches in addition to modern `FTopLevelAssetPath + SubPath`
  - `Runtime/CoreUObject/Private/UObject/SoftObjectPath.cpp`
  - BPX policy: parser accepts `FileVersionUE5=1000..1018`; legacy branches outside that window are rejected.

## 2026-03-05 UE 5.7.3 delta notes (vs 5.6.1)

- `EUnrealEngineObjectUE5Version` adds `IMPORT_TYPE_HIERARCHIES` after `OS_SUB_OBJECT_SHADOW_SERIALIZATION`.
  - `Runtime/Core/Public/UObject/ObjectVersion.h` (`enum class EUnrealEngineObjectUE5Version`)
- `FPackageFileSummary` adds:
  - `ImportTypeHierarchiesCount`
  - `ImportTypeHierarchiesOffset`
  - `Runtime/CoreUObject/Public/UObject/PackageFileSummary.h` (`struct FPackageFileSummary`)
- Summary serialization gates these two fields at `FileVersionUE >= IMPORT_TYPE_HIERARCHIES`.
  - `Runtime/CoreUObject/Private/UObject/PackageFileSummary.cpp` (`operator<<(FStructuredArchive::FSlot, FPackageFileSummary&)`)

## 2026-03-06 unversioned summary fallback alignment

- UE loader marks package summary as unversioned when all version fields are zero and then applies `GPackageFileUEVersion` / `GPackageFileLicenseeUEVersion`.
  - `Runtime/CoreUObject/Private/UObject/PackageFileSummary.cpp` (`operator<<(FStructuredArchive::FSlot, FPackageFileSummary&)`)
- BPX now mirrors this behavior by attempting unversioned parse with latest supported UE5 version first (`1018`), then retrying `1017` only when needed for legacy layout compatibility.
- Retry trigger uses summary alignment (`NameOffset == SummarySize`) to avoid silently selecting a mismatched layout branch.

## 2026-03-01 FText Equivalence Notes

### UE 5.6 facts (source-backed)

- `FText` persistence is handled by `operator<<(FStructuredArchive::FSlot, FText&)`
  - `Runtime/Core/Private/Internationalization/Text.cpp`
  - `HistoryType=None` adds `bHasCultureInvariantString` + `CultureInvariantString`
  - concrete history class is selected by `switch (ETextHistoryType)`
- `ETextHistoryType` spans `Base(0)`..`TextGenerator(12)` plus `None(-1)`; `AsCultureInvariant` is not a history type
  - `Runtime/Core/Private/Internationalization/TextHistory.h`
- history payload formats are defined in each `FTextHistory_*::Serialize`
  - `Runtime/Core/Private/Internationalization/TextHistory.cpp`
  - `Named/Ordered/ArgumentFormat`: `FormatText(FText)` + args array/map
  - `AsNumber/AsPercent/AsCurrency`: `FFormatArgumentValue` + `bHasFormatOptions` + `FNumberFormattingOptions` + `CultureName`
  - `AsDate/AsTime/AsDateTime`: `FDateTime` + style/timezone/culture (`AsDateTime` has `CustomPattern` branch)
  - `Transform`: `SourceText(FText)` + `TransformType`
  - `StringTableEntry`: `TableId(FName)` + `Key(FTextKey.SerializeAsString)`
  - `TextGenerator`: `GeneratorTypeID(FName)` + `GeneratorContents(TArray<uint8>)`
- argument binary formats are defined by `FFormatArgumentValue` / `FFormatArgumentData` stream operators
  - `Runtime/Core/Private/Internationalization/Text.cpp`

### Remaining work for full equivalence

1. Full reconstruction of history payload
- Status: implemented (`pkg/uasset/value_decode.go`)

2. Localized display resolution for `Base` (`Namespace/Key/SourceHash` lookup in `.locres`)
- Status: not implemented
- Basis: `FTextLocalizationManager::GetDisplayString`, `FindDisplayString_Internal`
- Reference: `Runtime/Core/Private/Internationalization/TextLocalizationManager.cpp`

3. `.locres` loading behavior (version differences, `SourceStringHash`, precedence conflicts)
- Status: not implemented
- Basis: `FTextLocalizationResource::LoadFromArchive`, `ShouldReplaceEntry`
- Reference: `Runtime/Core/Private/Internationalization/TextLocalizationResource.cpp`

4. StringTable redirect / load-policy parity
- Status: not implemented
- Basis: `FTextHistory_StringTableEntry::FStringTableReferenceData::*`
- Reference: `Runtime/Core/Private/Internationalization/TextHistory.cpp`

5. Re-evaluation for generated histories (number/date/transform/format)
- Status: not implemented (payload reconstruction only)
- Basis: `BuildLocalizedDisplayString` / `BuildInvariantDisplayString`
- Reference: `Runtime/Core/Private/Internationalization/TextHistory.cpp`

### Notes

- Exact display-string parity with UE Editor requires `.locres` and full culture-resolution chain; raw `.uasset` decode alone is insufficient.
- `TextGenerator` depends on runtime registration (`FText::RegisterTextGenerator`), so generic CLI tooling is limited to payload-level visibility.

### 2026-03-01 localization audit fixes

- `localization resolve` now verifies `SourceStringHash` during `.locres` resolution.
  - `Runtime/Core/Private/Internationalization/TextLocalizationManager.cpp` (`FindDisplayString_Internal`)
  - `Runtime/Core/Public/Internationalization/TextLocalizationResource.h` (`HashString`)
- Added structured decode for `GatherableTextData`, integrated into `localization read/query/resolve`.
  - `Runtime/Core/Public/Internationalization/GatherableTextData.h`
  - `Runtime/Core/Private/Internationalization/GatherableTextData.cpp`
  - `Runtime/Core/Private/Internationalization/InternationalizationMetadata.cpp`

## 2026-03-01 write/prop/var implementation notes

### UE 5.6 alignment points

- Recompute `FObjectExport::SerialOffset/SerialSize` and update ExportMap when export payload is rewritten.
  - `Runtime/CoreUObject/Private/UObject/ObjectResource.cpp`
- Preserve export-relative `ScriptSerializationStart/EndOffset`; adjust `End` by payload size delta.
  - `Runtime/CoreUObject/Private/UObject/SavePackage2.cpp`
- Shift summary section offsets according to export payload deltas (header patcher approach).
  - `Developer/AssetTools/Private/AssetHeaderPatcher.cpp`
- Recompute `FPropertyTag` size on updates and write header back; for `BoolProperty`, update tag flag (`BoolTrue`) rather than value bytes.
  - `Runtime/CoreUObject/Private/UObject/Class.cpp`, `Runtime/CoreUObject/Private/UObject/PropertyTag.cpp`

### Known current limits

- `var list` now extracts `VarName` from raw tagged properties even when `NewVariables` appears as `rawBase64`.
  - `Runtime/CoreUObject/Private/UObject/Class.cpp` (tagged-property terminator `NAME_None`)
- Full decode of `FBPVariableDescription.VarType` (`FEdGraphPinType`) is still pending; fallback path currently returns empty declaration type to avoid misreads.

## 2026-03-07 audit follow-up fixes

- Blueprint variable rename now derives `FriendlyName` using UE-equivalent `FName::NameToDisplayString` rules instead of a BPX-only identifier splitter.
  - `Editor/UnrealEd/Private/Kismet2/BlueprintEditorUtils.cpp` (`FBlueprintEditorUtils::RenameMemberVariable`)
  - `Runtime/Core/Private/UObject/UnrealNames.cpp` (`FName::NameToDisplayString`)
- `BoolProperty` omission on CDO rewrites is now gated by actual `RF_ClassDefaultObject` and Blueprint declaration presence, rather than object/class-name string heuristics.
  - `Runtime/CoreUObject/Public/UObject/ObjectMacros.h` (`RF_ClassDefaultObject`)
  - `Runtime/CoreUObject/Private/UObject/Class.cpp` (`UStruct::SerializeVersionedTaggedProperties`)
- Plain-string `TextProperty` updates now follow editor `FText::FromString` semantics; persistent save strips transient conversion bits, so saved flags end up zero.
  - `Runtime/Core/Private/Internationalization/Text.cpp` (`FText::FromString`, `operator<<(FStructuredArchive::FSlot, FText&)`)

## 2026-03-02 level var-list / var-set notes (placed-object variables)

- `.umap` placed-object resolution uses `FObjectExport::OuterIndex`, targeting exports under `PersistentLevel`.
  - `Runtime/CoreUObject/Private/UObject/SavePackage2.cpp`
- `ULevel` actor references are serialized in `ULevel::Serialize`; objects under `PersistentLevel` carry level context.
  - `Runtime/Engine/Private/Level.cpp` (`ULevel::Serialize`)
- Variable read/write reuses tagged-property paths aligned with `UStruct::SerializeVersionedTaggedProperties`.
  - `Runtime/CoreUObject/Private/UObject/Class.cpp`

## 2026-03-02 LevelViewportInfo support notes

- `FLevelViewportInfo` is a UE-defined struct with custom serialization via `operator<<`.
  - `Runtime/Engine/Classes/Engine/World.h` (`struct FLevelViewportInfo`, `operator<<`)
- `UWorld::Serialize` reads/writes it through `EditorViews` (`TArray<FLevelViewportInfo>`).
  - `Runtime/Engine/Private/World.cpp` (`UWorld::Serialize`)
- BPX now decodes `StructProperty(LevelViewportInfo)` via explicit UE-defined custom-serializer handling.

## 2026-03-02 UE BlueprintType custom serializer expansion

### Extraction criteria

- UE-defined `USTRUCT(BlueprintType)` with `TStructOpsTypeTraits` custom serializer flags (`WithSerializer=true` or `WithStructuredSerializer=true`).
- Source basis:
  - `Runtime/CoreUObject/Private/UObject/Property.cpp`
  - `Runtime/Engine/Public/PerQualityLevelProperties.h`
  - `Runtime/Engine/Classes/GameFramework/OnlineReplStructs.h`
  - `Runtime/Engine/Public/Animation/AnimTypes.h`
  - `Runtime/Engine/Public/Animation/AnimCurveTypes.h`
  - `Runtime/Engine/Classes/Animation/AttributeCurve.h`

### Added read/write coverage

- `FPerQualityLevelInt`, `FPerQualityLevelFloat`
  - `Runtime/Engine/Private/PerQualityLevelProperties.cpp` (`FPerQualityLevelProperty::StreamArchive`)
- `FUniqueNetIdRepl`
  - `Runtime/Engine/Private/OnlineReplStructs.cpp` (`operator<<(FArchive&, FUniqueNetIdRepl&)`)
- `FFrameNumber`
  - `Runtime/CoreUObject/Private/UObject/Property.cpp` (`TStructOpsTypeTraits<FFrameNumber>`)
- Generic tagged re-encode path for custom structs
  - Keeps `typeNodes/flags` on struct field wrappers and reconstructs `FPropertyTag` on write
  - Enables leaf edits in tagged custom structs such as `FAnimNotifyEvent`, `FAnimSyncMarker`, `FAnimCurveBase`, `FFloatCurve`, `FTransformCurve`

### Explicit boundaries

- User-defined custom-serialized structs are out of scope.
- Updates requiring NameMap insertion/rewrite for missing enum names are out of current write scope.

## 2026-03-02 NameMap read/write notes (`add` / `set` / `remove`)

- `FNameEntrySerialized` persistence format is `FString` + `NonCasePreservingHash(uint16)` + `CasePreservingHash(uint16)`.
  - `Runtime/Core/Private/UObject/UnrealNames.cpp` (`operator<<(FArchive&, FNameEntrySerialized&)`)
- UE load path may treat stored hashes as dummy-read values and rely on runtime hash computation.
  - Same source as above
- Hash computations use `FCrc::StrCrc32` (case-preserving) and `FCrc::Strihash_DEPRECATED` (non-case)
  - `Runtime/Core/Public/Misc/Crc.h`, `Runtime/Core/Private/Misc/Crc.cpp`
- `NamesReferencedFromExportDataCount` reserve rules constrain removable NameMap indices.
  - `Runtime/CoreUObject/Private/UObject/SavePackage2.cpp`
  - `Runtime/CoreUObject/Private/UObject/LinkerSave.cpp` (`operator<<(FName&)`)
- Summary fields typically affected by NameMap operations:
  - `NameCount`, `NameOffset`, `Generations.Last().NameCount`, `NamesReferencedFromExportDataCount` (clamp if needed)
  - `Runtime/CoreUObject/Private/UObject/PackageFileSummary.cpp`

## 2026-03-02 Niagara `TMap<FNiagaraVariableBase, FNiagaraVariant>` decode notes

- `FNiagaraVariableBase` is not fully tagged-struct serialized; it custom-serializes `Name(FName)` followed by `FNiagaraTypeDefinitionHandle`.
  - `Plugins/FX/Niagara/Source/Niagara/Private/NiagaraModule.cpp`
    - `FNiagaraVariableBase::Serialize`
    - `operator<<(FArchive&, FNiagaraTypeDefinitionHandle&)`
- `FNiagaraTypeDefinition` itself serializes tagged properties for `ClassStructOrEnum / UnderlyingType / Flags`.
  - `Plugins/FX/Niagara/Source/Niagara/Private/NiagaraModule.cpp` (`FNiagaraTypeDefinition::Serialize`)
- `FNiagaraVariant` can be reconstructed from tagged fields `Object / DataInterface / Bytes / CurrentMode`.
  - `Plugins/FX/Niagara/Source/Niagara/Public/NiagaraVariant.h`

## 2026-03-04 Material CLI notes (`material read`)

- Material instance parent and override arrays are serialized on `UMaterialInstance`.
  - `Runtime/Engine/Public/Materials/MaterialInstance.h`
    - `Parent`
    - `ScalarParameterValues`, `VectorParameterValues`, `DoubleVectorParameterValues`
    - `TextureParameterValues`, `TextureCollectionParameterValues`
    - `RuntimeVirtualTextureParameterValues`, `SparseVolumeTextureParameterValues`
    - `FontParameterValues`

- Parameter identity semantics come from `FMaterialParameterInfo` (`Name`, `Association`, `Index`).
  - `Runtime/Engine/Public/MaterialTypes.h`
    - `struct FMaterialParameterInfo`
    - `enum EMaterialParameterAssociation`

- Custom-node source snippets are serialized as `UMaterialExpressionCustom::Code` plus related fields.
  - `Runtime/Engine/Public/Materials/MaterialExpressionCustom.h`
    - `Code`
    - `Inputs`, `AdditionalOutputs`, `AdditionalDefines`, `IncludeFilePaths`

- Full translated material HLSL is generated by UE material compilation, not stored as one complete source blob in `.uasset`.
  - `Runtime/Engine/Private/Materials/HLSLMaterialTranslator.cpp`
    - `FHLSLMaterialTranslator::Translate`
    - `FHLSLMaterialTranslator::CustomExpression`
  - `Runtime/Engine/Private/Materials/Material.cpp`
    - `UMaterial::CompilePropertyEx`
