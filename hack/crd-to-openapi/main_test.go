/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildHiddenPaths(t *testing.T) {
	dir := t.TempDir()

	src := `package v1alpha1

type FooSpec struct {
	Visible string   ` + "`" + `json:"visible"` + "`" + `
	Hidden  string   ` + "`" + `json:"hidden" openapi:"hidden"` + "`" + `
	Nested  BarSpec  ` + "`" + `json:"nested"` + "`" + `
	Deep    *BazSpec ` + "`" + `json:"deep"` + "`" + `
}

type BarSpec struct {
	BarField    string ` + "`" + `json:"barField"` + "`" + `
	BarHidden   string ` + "`" + `json:"barHidden" openapi:"hidden"` + "`" + `
}

type BazSpec struct {
	BazField string ` + "`" + `json:"bazField"` + "`" + `
}

type FooStatus struct {
	StatusHidden string ` + "`" + `json:"statusHidden" openapi:"hidden"` + "`" + `
	StatusField  string ` + "`" + `json:"statusField"` + "`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := buildHiddenPaths(dir, []string{"Foo"})
	if err != nil {
		t.Fatal(err)
	}

	hidden, ok := result["Foo"]
	if !ok {
		t.Fatal("expected hidden paths for Foo")
	}

	expected := map[string]bool{
		"spec.hidden":           true,
		"spec.nested.barHidden": true,
		"status.statusHidden":   true,
	}

	for path := range expected {
		if !hidden[path] {
			t.Errorf("expected %q to be hidden", path)
		}
	}

	notExpected := []string{
		"spec.visible",
		"spec.nested.barField",
		"spec.deep.bazField",
		"status.statusField",
	}
	for _, path := range notExpected {
		if hidden[path] {
			t.Errorf("did not expect %q to be hidden", path)
		}
	}
}

func TestCleanSchema_HiddenFields(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"visible": map[string]any{"type": "string"},
			"hidden":  map[string]any{"type": "string"},
			"nested": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keep":   map[string]any{"type": "string"},
					"remove": map[string]any{"type": "string"},
				},
				"required": []any{"keep", "remove"},
			},
		},
		"required": []any{"visible", "hidden"},
	}

	hiddenPaths := map[string]bool{
		"spec.hidden":        true,
		"spec.nested.remove": true,
	}

	result := cleanSchema(schema, hiddenPaths, "spec")

	props := result["properties"].(map[string]any)
	if _, ok := props["visible"]; !ok {
		t.Error("expected 'visible' to be present")
	}
	if _, ok := props["hidden"]; ok {
		t.Error("expected 'hidden' to be removed")
	}

	// Check that 'hidden' was removed from required.
	req := result["required"].([]any)
	for _, r := range req {
		if r.(string) == "hidden" {
			t.Error("expected 'hidden' to be removed from required")
		}
	}

	// Check nested field removal.
	nested := props["nested"].(map[string]any)
	nestedProps := nested["properties"].(map[string]any)
	if _, ok := nestedProps["keep"]; !ok {
		t.Error("expected nested 'keep' to be present")
	}
	if _, ok := nestedProps["remove"]; ok {
		t.Error("expected nested 'remove' to be removed")
	}

	// Check nested required was updated.
	nestedReq := nested["required"].([]any)
	for _, r := range nestedReq {
		if r.(string) == "remove" {
			t.Error("expected 'remove' to be removed from nested required")
		}
	}
}

func TestCleanSchema_NoHiddenPaths(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"field1": map[string]any{"type": "string"},
			"field2": map[string]any{"type": "integer"},
		},
		"required": []any{"field1"},
	}

	result := cleanSchema(schema, nil, "")
	props := result["properties"].(map[string]any)
	if len(props) != 2 {
		t.Errorf("expected 2 properties, got %d", len(props))
	}
}

func TestBuildHiddenPaths_AllFieldsHidden(t *testing.T) {
	dir := t.TempDir()

	src := `package v1alpha1

type AllHiddenSpec struct {
	A string ` + "`" + `json:"a" openapi:"hidden"` + "`" + `
	B string ` + "`" + `json:"b" openapi:"hidden"` + "`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := buildHiddenPaths(dir, []string{"AllHidden"})
	if err != nil {
		t.Fatal(err)
	}

	hidden := result["AllHidden"]
	if !hidden["spec.a"] || !hidden["spec.b"] {
		t.Error("expected both fields to be hidden")
	}
}
