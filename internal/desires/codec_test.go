package desires

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestRawExtRoundTrip(t *testing.T) {
	original := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "test",
			"namespace": "default",
		},
		"data": map[string]any{
			"key": "value",
		},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	ext := &runtime.RawExtension{Raw: raw}

	m, err := rawExtToMap(ext)
	if err != nil {
		t.Fatalf("rawExtToMap: %v", err)
	}
	if m["apiVersion"] != "v1" {
		t.Fatalf("expected apiVersion=v1, got %v", m["apiVersion"])
	}

	ext2, err := mapToRawExt(m)
	if err != nil {
		t.Fatalf("mapToRawExt: %v", err)
	}

	var roundTripped map[string]any
	if err := json.Unmarshal(ext2.Raw, &roundTripped); err != nil {
		t.Fatal(err)
	}
	if roundTripped["kind"] != "ConfigMap" {
		t.Fatalf("expected kind=ConfigMap, got %v", roundTripped["kind"])
	}
	data := roundTripped["data"].(map[string]any)
	if data["key"] != "value" {
		t.Fatalf("expected data.key=value, got %v", data["key"])
	}
}

func TestRawExtNilHandling(t *testing.T) {
	m, err := rawExtToMap(nil)
	if err != nil {
		t.Fatal(err)
	}
	if m != nil {
		t.Fatal("expected nil map for nil RawExtension")
	}

	ext, err := mapToRawExt(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ext != nil {
		t.Fatal("expected nil RawExtension for nil value")
	}
}

func TestKubeContentWriteMap_ApplyDesire(t *testing.T) {
	raw := []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)
	desire := &ApplyDesire{
		Spec: ApplyDesireSpec{
			KubeContent: &runtime.RawExtension{Raw: raw},
		},
	}

	m, err := kubeContentWriteMap(desire)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m[rawExtFieldSpecKubeContent]; !ok {
		t.Fatal("expected spec_kubeContent in write map")
	}
	if _, ok := m[rawExtFieldStatusKubeContent]; ok {
		t.Fatal("did not expect status_kubeContent for ApplyDesire")
	}
}

func TestKubeContentWriteMap_ReadDesire(t *testing.T) {
	raw := []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)
	desire := &ReadDesire{
		Status: ReadDesireStatus{
			KubeContent: &runtime.RawExtension{Raw: raw},
		},
	}

	m, err := kubeContentWriteMap(desire)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m[rawExtFieldStatusKubeContent]; !ok {
		t.Fatal("expected status_kubeContent in write map")
	}
	if _, ok := m[rawExtFieldSpecKubeContent]; ok {
		t.Fatal("did not expect spec_kubeContent for ReadDesire")
	}
}

func TestKubeContentReadFromSnapshot(t *testing.T) {
	data := map[string]any{
		rawExtFieldSpecKubeContent: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
		},
	}

	desire := &ApplyDesire{}
	if err := kubeContentReadFromSnapshot(desire, data); err != nil {
		t.Fatal(err)
	}

	ext := desire.GetSpecKubeContent()
	if ext == nil {
		t.Fatal("expected non-nil spec KubeContent")
	}

	var m map[string]any
	if err := json.Unmarshal(ext.Raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["kind"] != "ConfigMap" {
		t.Fatalf("expected kind=ConfigMap, got %v", m["kind"])
	}
}
