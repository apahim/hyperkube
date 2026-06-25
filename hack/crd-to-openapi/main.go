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
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

type crdResource struct {
	Kind      string
	ListKind  string
	Plural    string
	Scope     string
	Schema    map[string]any
	HasStatus bool
}

func main() {
	var crdDir, output, title, version, kinds string
	flag.StringVar(&crdDir, "crd-dir", "config/crd/bases", "Directory containing CRD YAML files")
	flag.StringVar(&output, "output", "api/openapi/spec.yaml", "Output file path")
	flag.StringVar(&title, "title", "GCP HCP Backend API", "API title")
	flag.StringVar(&version, "version", "v1alpha1", "API version")
	flag.StringVar(&kinds, "kinds", "", "Comma-separated list of kinds to include (default: all)")
	flag.Parse()

	resources, err := loadCRDs(crdDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading CRDs: %v\n", err)
		os.Exit(1)
	}

	if kinds != "" {
		allowed := make(map[string]bool)
		for k := range strings.SplitSeq(kinds, ",") {
			allowed[strings.TrimSpace(k)] = true
		}
		var filtered []crdResource
		for _, r := range resources {
			if allowed[r.Kind] {
				filtered = append(filtered, r)
			}
		}
		resources = filtered
	}

	spec := buildOpenAPISpec(resources, title, version)

	data, err := yaml.Marshal(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(output, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated OpenAPI spec: %s (%d resources)\n", output, len(resources))
}

func loadCRDs(dir string) ([]crdResource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	var resources []crdResource
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", entry.Name(), err)
		}

		var crd map[string]any
		if err := yaml.Unmarshal(data, &crd); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}

		res, err := extractResource(crd)
		if err != nil {
			return nil, fmt.Errorf("extracting from %s: %w", entry.Name(), err)
		}
		resources = append(resources, res)
	}

	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Kind < resources[j].Kind
	})

	return resources, nil
}

func extractResource(crd map[string]any) (crdResource, error) {
	spec := mustMap(crd, "spec")
	names := mustMap(spec, "names")

	scope, _ := spec["scope"].(string)

	versions, ok := spec["versions"].([]any)
	if !ok || len(versions) == 0 {
		return crdResource{}, fmt.Errorf("no versions found")
	}

	v := versions[0].(map[string]any)
	schema := mustMap(mustMap(v, "schema"), "openAPIV3Schema")

	hasStatus := false
	if subs, ok := v["subresources"].(map[string]any); ok {
		_, hasStatus = subs["status"]
	}

	cleanedSchema := cleanSchema(schema)

	delete(cleanedSchema["properties"].(map[string]any), "apiVersion")
	delete(cleanedSchema["properties"].(map[string]any), "kind")
	delete(cleanedSchema["properties"].(map[string]any), "metadata")

	if req, ok := cleanedSchema["required"].([]any); ok {
		var filtered []any
		for _, r := range req {
			s, _ := r.(string)
			if s != "apiVersion" && s != "kind" && s != "metadata" {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			cleanedSchema["required"] = filtered
		} else {
			delete(cleanedSchema, "required")
		}
	}

	kind, _ := names["kind"].(string)
	listKind, _ := names["listKind"].(string)
	plural, _ := names["plural"].(string)

	return crdResource{
		Kind:      kind,
		ListKind:  listKind,
		Plural:    plural,
		Scope:     scope,
		Schema:    cleanedSchema,
		HasStatus: hasStatus,
	}, nil
}

func cleanSchema(schema map[string]any) map[string]any {
	out := make(map[string]any)

	for k, v := range schema {
		if strings.HasPrefix(k, "x-kubernetes-") {
			continue
		}

		switch k {
		case "properties":
			if props, ok := v.(map[string]any); ok {
				cleaned := make(map[string]any)
				for pk, pv := range props {
					if pm, ok := pv.(map[string]any); ok {
						cleaned[pk] = cleanSchema(pm)
					} else {
						cleaned[pk] = pv
					}
				}
				out[k] = cleaned
			}
		case "items":
			if items, ok := v.(map[string]any); ok {
				out[k] = cleanSchema(items)
			} else {
				out[k] = v
			}
		case "additionalProperties":
			if ap, ok := v.(map[string]any); ok {
				out[k] = cleanSchema(ap)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}

	return out
}

func buildOpenAPISpec(resources []crdResource, title, version string) map[string]any {
	schemas := make(map[string]any)
	paths := make(map[string]any)

	conditionSchema := map[string]any{
		"type":        "object",
		"description": "Standard Kubernetes condition",
		"properties": map[string]any{
			"type":               map[string]any{"type": "string", "description": "Type of condition"},
			"status":             map[string]any{"type": "string", "enum": []any{"True", "False", "Unknown"}},
			"reason":             map[string]any{"type": "string", "description": "Programmatic reason for the condition"},
			"message":            map[string]any{"type": "string", "description": "Human-readable message"},
			"lastTransitionTime": map[string]any{"type": "string", "format": "date-time"},
			"observedGeneration": map[string]any{"type": "integer", "format": "int64"},
		},
		"required": []any{"type", "status", "reason", "message", "lastTransitionTime"},
	}
	schemas["Condition"] = conditionSchema

	metadataSchema := map[string]any{
		"type":        "object",
		"description": "Standard object metadata",
		"properties": map[string]any{
			"name":              map[string]any{"type": "string", "description": "Name of the resource"},
			"namespace":         map[string]any{"type": "string", "description": "Namespace of the resource"},
			"resourceVersion":   map[string]any{"type": "string", "description": "Resource version for optimistic concurrency"},
			"creationTimestamp": map[string]any{"type": "string", "format": "date-time", "description": "Creation timestamp", "readOnly": true},
			"labels":            map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			"annotations":       map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		},
	}
	schemas["ObjectMeta"] = metadataSchema

	for _, res := range resources {
		schemas[res.Kind] = buildResourceSchema(res)
		schemas[res.ListKind] = buildListSchema(res)
		addPaths(paths, res, version)
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       title,
			"version":     version,
			"description": "REST API for managing HyperShift hosted clusters on GCP",
		},
		"paths": paths,
		"components": map[string]any{
			"schemas": schemas,
		},
	}
}

func buildResourceSchema(res crdResource) map[string]any {
	schema := map[string]any{
		"type":        "object",
		"description": getDescription(res.Schema),
	}

	props := map[string]any{
		"metadata": map[string]any{"$ref": "#/components/schemas/ObjectMeta"},
	}

	if srcProps, ok := res.Schema["properties"].(map[string]any); ok {
		maps.Copy(props, srcProps)
	}

	schema["properties"] = props

	if req, ok := res.Schema["required"].([]any); ok {
		schema["required"] = req
	}

	return schema
}

func buildListSchema(res crdResource) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": fmt.Sprintf("A list of %s resources", res.Kind),
		"properties": map[string]any{
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"continue":        map[string]any{"type": "string", "description": "Token for the next page of results"},
					"resourceVersion": map[string]any{"type": "string"},
				},
			},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"$ref": "#/components/schemas/" + res.Kind,
				},
			},
		},
		"required": []any{"items"},
	}
}

func addPaths(paths map[string]any, res crdResource, version string) {
	tag := res.Kind

	var collectionPath, itemPath string
	if res.Scope == "Namespaced" {
		collectionPath = fmt.Sprintf("/%s/namespaces/{namespace}/%s", version, res.Plural)
		itemPath = fmt.Sprintf("/%s/namespaces/{namespace}/%s/{name}", version, res.Plural)
	} else {
		collectionPath = fmt.Sprintf("/%s/%s", version, res.Plural)
		itemPath = fmt.Sprintf("/%s/%s/{name}", version, res.Plural)
	}

	namespaceParam := map[string]any{
		"name":        "namespace",
		"in":          "path",
		"required":    true,
		"description": "Namespace of the resource",
		"schema":      map[string]any{"type": "string"},
	}
	nameParam := map[string]any{
		"name":        "name",
		"in":          "path",
		"required":    true,
		"description": fmt.Sprintf("Name of the %s", res.Kind),
		"schema":      map[string]any{"type": "string"},
	}

	var collectionParams, itemParams []any
	if res.Scope == "Namespaced" {
		collectionParams = []any{namespaceParam}
		itemParams = []any{namespaceParam, nameParam}
	} else {
		itemParams = []any{nameParam}
	}

	collectionOps := map[string]any{
		"get": map[string]any{
			"summary":     fmt.Sprintf("List %s resources", res.Kind),
			"operationId": fmt.Sprintf("list%s", res.Kind),
			"tags":        []any{tag},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + res.ListKind},
						},
					},
				},
			},
		},
		"post": map[string]any{
			"summary":     fmt.Sprintf("Create a %s", res.Kind),
			"operationId": fmt.Sprintf("create%s", res.Kind),
			"tags":        []any{tag},
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
					},
				},
			},
			"responses": map[string]any{
				"201": map[string]any{
					"description": "Created",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
						},
					},
				},
				"409": map[string]any{"description": "Conflict - resource already exists"},
			},
		},
	}
	if len(collectionParams) > 0 {
		collectionOps["parameters"] = collectionParams
	}

	itemOps := map[string]any{
		"parameters": itemParams,
		"get": map[string]any{
			"summary":     fmt.Sprintf("Get a %s", res.Kind),
			"operationId": fmt.Sprintf("get%s", res.Kind),
			"tags":        []any{tag},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
						},
					},
				},
				"404": map[string]any{"description": "Not found"},
			},
		},
		"put": map[string]any{
			"summary":     fmt.Sprintf("Update a %s", res.Kind),
			"operationId": fmt.Sprintf("update%s", res.Kind),
			"tags":        []any{tag},
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
						},
					},
				},
				"404": map[string]any{"description": "Not found"},
				"409": map[string]any{"description": "Conflict - resource version mismatch"},
			},
		},
		"delete": map[string]any{
			"summary":     fmt.Sprintf("Delete a %s", res.Kind),
			"operationId": fmt.Sprintf("delete%s", res.Kind),
			"tags":        []any{tag},
			"responses": map[string]any{
				"200": map[string]any{"description": "OK"},
				"404": map[string]any{"description": "Not found"},
			},
		},
	}

	paths[collectionPath] = collectionOps
	paths[itemPath] = itemOps

	if res.HasStatus {
		statusPath := itemPath + "/status"
		statusOps := map[string]any{
			"parameters": itemParams,
			"get": map[string]any{
				"summary":     fmt.Sprintf("Get status of a %s", res.Kind),
				"operationId": fmt.Sprintf("get%sStatus", res.Kind),
				"tags":        []any{tag},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OK",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
							},
						},
					},
					"404": map[string]any{"description": "Not found"},
				},
			},
			"put": map[string]any{
				"summary":     fmt.Sprintf("Update status of a %s", res.Kind),
				"operationId": fmt.Sprintf("update%sStatus", res.Kind),
				"tags":        []any{tag},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
						},
					},
				},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OK",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/" + res.Kind},
							},
						},
					},
					"404": map[string]any{"description": "Not found"},
				},
			},
		}
		paths[statusPath] = statusOps
	}
}

func getDescription(schema map[string]any) string {
	if d, ok := schema["description"].(string); ok {
		return d
	}
	return ""
}

func mustMap(m map[string]any, key string) map[string]any {
	v, ok := m[key].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return v
}
