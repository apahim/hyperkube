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

package cincinnati

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeriveChannel(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		channelGroup string
		want         string
	}{
		{"stable 4.16", "4.16", "stable", "stable-4.16"},
		{"candidate 4.17", "4.17", "candidate", "candidate-4.17"},
		{"fast 4.15", "4.15", "fast", "fast-4.15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveChannel(tt.version, tt.channelGroup)
			if got != tt.want {
				t.Errorf("DeriveChannel(%q, %q) = %q, want %q", tt.version, tt.channelGroup, got, tt.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			{Version: "4.16.0", Payload: "quay.io/ocp-release:4.16.0-x86_64"},
			{Version: "4.16.1", Payload: "quay.io/ocp-release:4.16.1-x86_64"},
			{Version: "4.16.3", Payload: "quay.io/ocp-release:4.16.3-x86_64"},
			{Version: "4.16.2", Payload: "quay.io/ocp-release:4.16.2-x86_64"},
			{Version: "4.15.9", Payload: "quay.io/ocp-release:4.15.9-x86_64"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(graph)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)

	image, resolved, channel, err := client.ResolveVersion(context.Background(), "4.16", "stable", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if image != "quay.io/ocp-release:4.16.3-x86_64" {
		t.Errorf("image = %q, want latest 4.16.x payload", image)
	}
	if resolved != "4.16.3" {
		t.Errorf("resolvedVersion = %q, want %q", resolved, "4.16.3")
	}
	if channel != "stable-4.16" {
		t.Errorf("channel = %q, want %q", channel, "stable-4.16")
	}
}

func TestResolveVersion_NotFound(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			{Version: "4.15.9", Payload: "quay.io/ocp-release:4.15.9-x86_64"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(graph)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)

	_, _, _, err := client.ResolveVersion(context.Background(), "4.16", "stable", "amd64")
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
}

func TestResolveVersion_EmptyVersion(t *testing.T) {
	client := NewClient("http://unused", 5*time.Second)

	_, _, _, err := client.ResolveVersion(context.Background(), "", "stable", "amd64")
	if err == nil {
		t.Fatal("expected error for empty version, got nil")
	}
}

func TestResolveVersion_DefaultChannelGroup(t *testing.T) {
	var gotChannel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotChannel = r.URL.Query().Get("channel")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Graph{
			Nodes: []Node{
				{Version: "4.16.0", Payload: "quay.io/ocp-release:4.16.0"},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)

	_, _, _, err := client.ResolveVersion(context.Background(), "4.16", "", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotChannel != "stable-4.16" {
		t.Errorf("default channel = %q, want %q", gotChannel, "stable-4.16")
	}
}

func TestResolveVersion_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)

	_, _, _, err := client.ResolveVersion(context.Background(), "4.16", "stable", "amd64")
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"4.16.3", "4.16.1", 1},
		{"4.16.1", "4.16.3", -1},
		{"4.16.0", "4.16.0", 0},
		{"4.16.10", "4.16.9", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if (tt.want > 0 && got <= 0) || (tt.want < 0 && got >= 0) || (tt.want == 0 && got != 0) {
				t.Errorf("compareVersions(%q, %q) = %d, want sign matching %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
