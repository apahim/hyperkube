package placement

import (
	"testing"
)

func TestClusterMapDocID(t *testing.T) {
	tests := []struct {
		namespace string
		name      string
		want      string
	}{
		{"my-project", "my-cluster", "my-project:my-cluster"},
		{"ns", "c", "ns:c"},
		{"project-alpha", "cluster-1", "project-alpha:cluster-1"},
	}
	for _, tt := range tests {
		got := clusterMapDocID(tt.namespace, tt.name)
		if got != tt.want {
			t.Errorf("clusterMapDocID(%q, %q) = %q, want %q", tt.namespace, tt.name, got, tt.want)
		}
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int64
		ok   bool
	}{
		{"int64", int64(42), 42, true},
		{"float64", float64(37), 37, true},
		{"int", int(10), 10, true},
		{"string", "nope", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toInt64(tt.val)
			if ok != tt.ok || got != tt.want {
				t.Errorf("toInt64(%v) = (%d, %v), want (%d, %v)", tt.val, got, ok, tt.want, tt.ok)
			}
		})
	}
}
