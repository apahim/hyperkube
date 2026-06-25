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
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.openshift.com/api/upgrades_info/v1/graph"
	DefaultArch    = "amd64"
)

// VersionResolver resolves an OCP version to a release image.
type VersionResolver interface {
	ResolveVersion(ctx context.Context, version, channelGroup, arch string) (image string, resolvedVersion string, channel string, err error)
}

// Node represents a single version node in the Cincinnati graph.
type Node struct {
	Version string `json:"version"`
	Payload string `json:"payload"`
}

// Graph represents the Cincinnati update graph response.
type Graph struct {
	Nodes []Node    `json:"nodes"`
	Edges [][]int64 `json:"edges"`
}

// Client queries the Cincinnati update service to resolve OCP versions to release images.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Cincinnati client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// ResolveVersion queries Cincinnati for the latest patch version matching the
// given major.minor version prefix and returns its release image pullspec.
// For example, version "4.16" with channelGroup "stable" queries channel
// "stable-4.16" and returns the highest patch version found (e.g. "4.16.3").
func (c *Client) ResolveVersion(ctx context.Context, version, channelGroup, arch string) (image string, resolvedVersion string, channel string, err error) {
	if version == "" {
		return "", "", "", fmt.Errorf("version is required")
	}
	if channelGroup == "" {
		channelGroup = "stable"
	}
	if arch == "" {
		arch = DefaultArch
	}

	channel = DeriveChannel(version, channelGroup)

	graph, err := c.fetchGraph(ctx, channel, arch)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch Cincinnati graph for channel %s: %w", channel, err)
	}

	prefix := version + "."
	var best *Node
	for i := range graph.Nodes {
		n := &graph.Nodes[i]
		if !strings.HasPrefix(n.Version, prefix) {
			continue
		}
		if best == nil || compareVersions(n.Version, best.Version) > 0 {
			best = n
		}
	}

	if best == nil {
		return "", "", "", fmt.Errorf("no version matching %q found in channel %s", version, channel)
	}
	if best.Payload == "" {
		return "", "", "", fmt.Errorf("version %s found in channel %s but has no payload image", best.Version, channel)
	}

	return best.Payload, best.Version, channel, nil
}

// DeriveChannel builds the Cincinnati channel name from a major.minor version
// and a channel group. For example: "4.16", "stable" → "stable-4.16".
func DeriveChannel(version, channelGroup string) string {
	return fmt.Sprintf("%s-%s", channelGroup, version)
}

// compareVersions does a simple numeric comparison of dot-separated version
// strings. Returns >0 if a > b, <0 if a < b, 0 if equal.
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := max(len(bParts), len(aParts))

	for i := range maxLen {
		var aVal, bVal int
		if i < len(aParts) {
			aVal, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bVal, _ = strconv.Atoi(bParts[i])
		}
		if aVal != bVal {
			return aVal - bVal
		}
	}
	return 0
}

// fetchGraph fetches the Cincinnati update graph for the given channel and architecture.
func (c *Client) fetchGraph(ctx context.Context, channel, arch string) (*Graph, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("channel", channel)
	q.Set("arch", arch)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cincinnati returned status %d: %s", resp.StatusCode, string(body))
	}

	var graph Graph
	if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
		return nil, fmt.Errorf("failed to decode Cincinnati response: %w", err)
	}

	return &graph, nil
}
