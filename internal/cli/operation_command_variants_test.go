package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationFixtureCommandVariants(t *testing.T) {
	type fixtureVariant struct {
		command string
		expect  string
	}
	required := map[string]fixtureVariant{
		"write_roundtrip":                                {command: "write", expect: "byte_equal"},
		"write_roundtrip_umap":                           {command: "write", expect: "byte_equal"},
		"prop_add_fixture_int":                           {command: "prop add", expect: "byte_equal"},
		"prop_remove_fixture_int":                        {command: "prop remove", expect: "byte_equal"},
		"dt_add_row_values_scalar":                       {command: "datatable add-row", expect: "byte_equal"},
		"dt_add_row_values_mixed":                        {command: "datatable add-row", expect: "byte_equal"},
		"dt_remove_row_base":                             {command: "datatable remove-row", expect: "byte_equal"},
		"dt_update_int":                                  {command: "datatable update-row", expect: "byte_equal"},
		"name_add":                                       {command: "name add", expect: "byte_equal"},
		"name_add_hash_override":                         {command: "name add", expect: "byte_equal"},
		"name_set":                                       {command: "name set", expect: "byte_equal"},
		"name_set_hash_override":                         {command: "name set", expect: "byte_equal"},
		"name_remove":                                    {command: "name remove", expect: "byte_equal"},
		"name_remove_non_tail_reject":                    {command: "name remove", expect: "error_equal"},
		"name_remove_referenced_reject":                  {command: "name remove", expect: "error_equal"},
		"metadata_set_category":                          {command: "metadata set-root", expect: "byte_equal"},
		"metadata_set_object":                            {command: "metadata set-object", expect: "byte_equal"},
		"metadata_set_object_unicode":                    {command: "metadata set-object", expect: "byte_equal"},
		"enum_write_value":                               {command: "enum write-value", expect: "byte_equal"},
		"enum_write_value_numeric":                       {command: "enum write-value", expect: "byte_equal"},
		"enum_write_value_missing":                       {command: "enum write-value", expect: "error_equal"},
		"package_set_flags":                              {command: "package set-flags", expect: "byte_equal"},
		"package_set_flags_raw":                          {command: "package set-flags", expect: "byte_equal"},
		"stringtable_write_entry":                        {command: "stringtable write-entry", expect: "byte_equal"},
		"stringtable_write_entry_unicode":                {command: "stringtable write-entry", expect: "byte_equal"},
		"var_set_default_empty":                          {command: "var set-default", expect: "byte_equal"},
		"var_set_default_unicode":                        {command: "var set-default", expect: "byte_equal"},
		"var_set_default_long":                           {command: "var set-default", expect: "byte_equal"},
		"var_rename_simple":                              {command: "var rename", expect: "byte_equal"},
		"var_rename_unicode":                             {command: "var rename", expect: "byte_equal"},
		"var_rename_with_refs":                           {command: "var rename", expect: "byte_equal"},
		"ref_rewrite_single":                             {command: "ref rewrite", expect: "byte_equal"},
		"ref_rewrite_multi":                              {command: "ref rewrite", expect: "byte_equal"},
		"stringtable_remove_entry":                       {command: "stringtable remove-entry", expect: "byte_equal"},
		"stringtable_set_namespace":                      {command: "stringtable set-namespace", expect: "byte_equal"},
		"localization_set_source":                        {command: "localization set-source", expect: "byte_equal"},
		"localization_set_source_unicode":                {command: "localization set-source", expect: "byte_equal"},
		"localization_set_id":                            {command: "localization set-id", expect: "byte_equal"},
		"localization_set_id_base_text":                  {command: "localization set-id", expect: "byte_equal"},
		"localization_set_stringtable_ref":               {command: "localization set-stringtable-ref", expect: "byte_equal"},
		"localization_set_stringtable_ref_missing_table": {command: "localization set-stringtable-ref", expect: "error_equal"},
		"localization_rewrite_namespace":                 {command: "localization rewrite-namespace", expect: "byte_equal"},
		"localization_rekey":                             {command: "localization rekey", expect: "byte_equal"},
		"level_var_set":                                  {command: "level var-set", expect: "error_equal"},
		"level_var_set_export_selector":                  {command: "level var-set", expect: "error_equal"},
		"level_var_set_path_selector":                    {command: "level var-set", expect: "error_equal"},
	}

	roots := goldenFixtureRoots(t, "operations")
	if len(roots) == 0 {
		t.Fatalf("no operations fixture roots found")
	}

	for _, root := range roots {
		root := root
		t.Run(filepath.Base(root), func(t *testing.T) {
			operationsDir := filepath.Join(root, "operations")
			for name, variant := range required {
				specPath := filepath.Join(operationsDir, name, "operation.json")
				body, err := os.ReadFile(specPath)
				if err != nil {
					t.Fatalf("read %s operation spec: %v", name, err)
				}
				var spec operationSpec
				if err := json.Unmarshal(body, &spec); err != nil {
					t.Fatalf("parse %s operation spec: %v", name, err)
				}
				if got := strings.TrimSpace(spec.Command); got != variant.command {
					t.Fatalf("%s command mismatch: got %q want %q", name, got, variant.command)
				}
				if got := strings.TrimSpace(spec.Expect); got != variant.expect {
					t.Fatalf("%s expect mismatch: got %q want %q", name, got, variant.expect)
				}
				if len(spec.Args) == 0 {
					t.Fatalf("%s has empty args", name)
				}
			}
		})
	}
}
