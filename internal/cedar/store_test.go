package cedar

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := hcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	store, err := NewStore(fakeClient, "cedar-policies", "")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreListTemplates(t *testing.T) {
	store := newTestStore(t)
	templates := store.ListTemplates()
	if len(templates) != 4 {
		t.Fatalf("expected 4 templates, got %d", len(templates))
	}

	names := make(map[string]bool)
	for _, tmpl := range templates {
		names[tmpl.Name] = true
	}
	for _, want := range []string{"service-admin", "cluster-admin", "cluster-viewer", "developer"} {
		if !names[want] {
			t.Errorf("missing template %q", want)
		}
	}
}

func TestStoreGetTemplate(t *testing.T) {
	store := newTestStore(t)

	tmpl, ok := store.GetTemplate("cluster-viewer")
	if !ok {
		t.Fatal("expected to find cluster-viewer template")
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

	att, err := store.CreateAttachment(ctx, "my-project", "cluster-viewer", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if att.ID == "" {
		t.Error("expected non-empty attachment ID")
	}
	if att.TemplateName != "cluster-viewer" {
		t.Errorf("template: got %q, want %q", att.TemplateName, "cluster-viewer")
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

	att, err := store.CreateAttachment(ctx, "my-project", "cluster-viewer", "alice")
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

	_, err := store.CreateAttachment(ctx, "my-project", "cluster-viewer", "alice")
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
	if !strings.Contains(result, `HCP::User::"alice"`) {
		t.Error("resolved policy should contain the principal")
	}
	if !strings.Contains(result, `HCP::Project::"my-project"`) {
		t.Error("resolved policy should contain the resource")
	}
	if strings.Contains(result, "?principal") || strings.Contains(result, "?resource") {
		t.Error("resolved policy should not contain placeholders")
	}
}

func TestResolvePolicies_CustomRole(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	role := &hcpv1alpha1.CustomRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-reader",
			Namespace: "my-project",
		},
		Spec: hcpv1alpha1.CustomRoleSpec{
			Permissions: []string{"cluster.get", "cluster.list"},
			Description: "Custom read-only role",
		},
	}
	if err := store.client.Create(ctx, role); err != nil {
		t.Fatal(err)
	}

	_, err := store.CreateAttachment(ctx, "my-project", "custom-reader", "alice")
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ResolvePolicies(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `HCP::User::"alice"`) {
		t.Error("resolved policy should contain the principal")
	}
	if !strings.Contains(result, `GetManagedHostedCluster`) {
		t.Error("resolved policy should contain GetManagedHostedCluster action")
	}
	if !strings.Contains(result, `ListManagedHostedClusters`) {
		t.Error("resolved policy should contain ListManagedHostedClusters action")
	}
}

func TestResolvePolicies_CustomRoleWithConditions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	role := &hcpv1alpha1.CustomRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regional-viewer",
			Namespace: "my-project",
		},
		Spec: hcpv1alpha1.CustomRoleSpec{
			Permissions: []string{"cluster.list", "cluster.get"},
			Conditions:  []string{`resource.labels.region == "us-east1"`},
		},
	}
	if err := store.client.Create(ctx, role); err != nil {
		t.Fatal(err)
	}

	_, err := store.CreateAttachment(ctx, "my-project", "regional-viewer", "alice")
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ResolvePolicies(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `when { resource.labels.region == "us-east1" }`) {
		t.Errorf("resolved policy should contain condition, got: %s", result)
	}
	if !strings.Contains(result, "ListManagedHostedClusters") {
		t.Error("resolved policy should contain ListManagedHostedClusters action")
	}
	for p := range strings.SplitSeq(result, ";\n") {
		if strings.Contains(p, "ListManagedHostedClusters") && strings.Contains(p, "when") {
			t.Error("collection permit block should not have conditions")
		}
	}
}

func TestResolvePolicies_WithGlobalPolicies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	globalCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultGlobalPolicyCM,
			Namespace: "cedar-policies",
		},
		Data: map[string]string{
			globalPoliciesKey: `permit (
    principal == HCP::User::"bootstrap-sa",
    action == HCP::Action::"ManagePolicies",
    resource
);`,
		},
	}
	if err := store.client.Create(ctx, globalCM); err != nil {
		t.Fatal(err)
	}

	result, err := store.ResolvePolicies(ctx, "fresh-project")
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("expected non-empty result with global policies")
	}
	if !strings.Contains(result, "bootstrap-sa") {
		t.Error("resolved policies should contain global policy for bootstrap-sa")
	}
	if !strings.Contains(result, "ManagePolicies") {
		t.Error("resolved policies should contain ManagePolicies action")
	}
}

func TestResolvePolicies_GlobalPoliciesMissing(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result, err := store.ResolvePolicies(ctx, "no-policies-project")
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Errorf("expected empty result with no global or project policies, got %q", result)
	}
}

func TestResolvePolicies_GlobalAndProjectPolicies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	globalCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultGlobalPolicyCM,
			Namespace: "cedar-policies",
		},
		Data: map[string]string{
			globalPoliciesKey: `permit (
    principal == HCP::User::"bootstrap-sa",
    action == HCP::Action::"ManagePolicies",
    resource
);`,
		},
	}
	if err := store.client.Create(ctx, globalCM); err != nil {
		t.Fatal(err)
	}

	_, err := store.CreateAttachment(ctx, "my-project", "cluster-viewer", "alice")
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ResolvePolicies(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "bootstrap-sa") {
		t.Error("result should contain global policy")
	}
	if !strings.Contains(result, `HCP::User::"alice"`) {
		t.Error("result should contain project-level policy")
	}
}

func TestListRoles(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	role := &hcpv1alpha1.CustomRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-custom-role",
			Namespace: "my-project",
		},
		Spec: hcpv1alpha1.CustomRoleSpec{
			Permissions: []string{"cluster.get"},
			Description: "test custom role",
		},
	}
	if err := store.client.Create(ctx, role); err != nil {
		t.Fatal(err)
	}

	roles, err := store.ListRoles(ctx, "my-project")
	if err != nil {
		t.Fatal(err)
	}

	if len(roles) != 5 {
		t.Fatalf("expected 5 roles (4 predefined + 1 custom), got %d", len(roles))
	}

	foundCustom := false
	for _, r := range roles {
		if r.Name == "my-custom-role" {
			foundCustom = true
			if r.Predefined {
				t.Error("custom role should not be marked predefined")
			}
		}
	}
	if !foundCustom {
		t.Error("custom role not found in list")
	}
}

func TestGeneratePolicyFromPermissions(t *testing.T) {
	policy := GeneratePolicyFromPermissions(
		[]string{"cluster.get", "cluster.list"},
		nil,
		"alice",
		"my-project",
	)
	if !strings.Contains(policy, `HCP::User::"alice"`) {
		t.Error("policy should contain principal")
	}
	if !strings.Contains(policy, `GetManagedHostedCluster`) {
		t.Error("policy should contain get action")
	}
	if !strings.Contains(policy, `ListManagedHostedClusters`) {
		t.Error("policy should contain list action")
	}
	if strings.Count(policy, "permit (") != 2 {
		t.Errorf("expected 2 permit blocks (collection + resource), got %d", strings.Count(policy, "permit ("))
	}
}

func TestGeneratePolicyFromPermissions_WithConditions(t *testing.T) {
	policy := GeneratePolicyFromPermissions(
		[]string{"cluster.list", "cluster.get"},
		[]string{`resource.labels.region == "us-east1"`},
		"alice",
		"my-project",
	)
	permits := strings.Split(policy, ";\n")
	var collectionBlock, resourceBlock string
	for _, p := range permits {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "ListManagedHostedClusters") {
			collectionBlock = p
		}
		if strings.Contains(p, "GetManagedHostedCluster") {
			resourceBlock = p
		}
	}
	if collectionBlock == "" {
		t.Fatal("expected collection permit block with ListManagedHostedClusters")
	}
	if resourceBlock == "" {
		t.Fatal("expected resource permit block with GetManagedHostedCluster")
	}
	if strings.Contains(collectionBlock, "when") {
		t.Error("collection permit block should NOT have conditions")
	}
	if !strings.Contains(resourceBlock, `when { resource.labels.region == "us-east1" }`) {
		t.Error("resource permit block should have conditions")
	}
}

func TestGeneratePolicyFromPermissions_CollectionOnly(t *testing.T) {
	policy := GeneratePolicyFromPermissions(
		[]string{"cluster.list"},
		[]string{`resource.labels.env == "prod"`},
		"alice",
		"my-project",
	)
	if strings.Count(policy, "permit (") != 1 {
		t.Errorf("expected 1 permit block for collection-only, got %d", strings.Count(policy, "permit ("))
	}
	if strings.Contains(policy, "when") {
		t.Error("collection-only policy should not have conditions")
	}
}

func TestGeneratePolicyFromPermissions_ResourceOnly(t *testing.T) {
	policy := GeneratePolicyFromPermissions(
		[]string{"cluster.get"},
		[]string{`resource.labels.env == "prod"`},
		"alice",
		"my-project",
	)
	if strings.Count(policy, "permit (") != 1 {
		t.Errorf("expected 1 permit block for resource-only, got %d", strings.Count(policy, "permit ("))
	}
	if !strings.Contains(policy, "when") {
		t.Error("resource-only policy should have conditions")
	}
}
