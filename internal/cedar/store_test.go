package cedar

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	store, err := NewStore(fakeClient, "cedar-policies")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreListTemplates(t *testing.T) {
	store := newTestStore(t)
	templates := store.ListTemplates()
	if len(templates) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(templates))
	}

	names := make(map[string]bool)
	for _, tmpl := range templates {
		names[tmpl.Name] = true
	}
	for _, want := range []string{"full-access", "read-clusters", "write-clusters"} {
		if !names[want] {
			t.Errorf("missing template %q", want)
		}
	}
}

func TestStoreGetTemplate(t *testing.T) {
	store := newTestStore(t)

	tmpl, ok := store.GetTemplate("read-clusters")
	if !ok {
		t.Fatal("expected to find read-clusters template")
	}
	if tmpl.PolicyText == "" {
		t.Error("expected non-empty policy text")
	}

	_, ok = store.GetTemplate("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent template")
	}
}

func TestStoreCreateAndListAttachments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	att, err := store.CreateAttachment(ctx, "my-project", "read-clusters", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if att.ID == "" {
		t.Error("expected non-empty attachment ID")
	}
	if att.TemplateName != "read-clusters" {
		t.Errorf("template: got %q, want %q", att.TemplateName, "read-clusters")
	}

	attachments, err := store.ListAttachments(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
}

func TestStoreCreateAttachment_InvalidTemplate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.CreateAttachment(ctx, "my-project", "nonexistent", "alice")
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestStoreDeleteAttachment(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	att, err := store.CreateAttachment(ctx, "my-project", "read-clusters", "alice")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteAttachment(ctx, "my-project", att.ID); err != nil {
		t.Fatal(err)
	}

	attachments, err := store.ListAttachments(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 0 {
		t.Fatalf("expected 0 attachments after delete, got %d", len(attachments))
	}
}

func TestStoreDeleteAttachment_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.DeleteAttachment(ctx, "my-project", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent attachment")
	}
}

func TestResolvePolicies_NoAttachments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result, err := store.ResolvePolicies(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestResolvePolicies_WithAttachment(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.CreateAttachment(ctx, "my-project", "read-clusters", "alice")
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ResolvePolicies(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}

	if result == "" {
		t.Fatal("expected non-empty resolved policies")
	}
	if !contains(result, `HCP::User::"alice"`) {
		t.Error("resolved policy should contain the principal")
	}
	if !contains(result, `HCP::Project::"my-project"`) {
		t.Error("resolved policy should contain the resource")
	}
	if contains(result, "?principal") || contains(result, "?resource") {
		t.Error("resolved policy should not contain placeholders")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
