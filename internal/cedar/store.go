package cedar

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
)

const (
	attachmentsKey        = "attachments.json"
	globalPoliciesKey     = "policies.cedar"
	DefaultGlobalPolicyCM = "cedar-global-policies"
)

type Template struct {
	Name       string `json:"name"`
	PolicyText string `json:"policy_text"`
}

type Attachment struct {
	ID           string `json:"id"`
	TemplateName string `json:"template_name"`
	Principal    string `json:"principal"`
	CreatedAt    string `json:"created_at"`
}

type Store struct {
	client                client.Client
	namespace             string
	templates             map[string]Template
	globalPolicyConfigMap string
}

func NewStore(k8sClient client.Client, namespace, globalPolicyConfigMap string) (*Store, error) {
	templates, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}
	if globalPolicyConfigMap == "" {
		globalPolicyConfigMap = DefaultGlobalPolicyCM
	}
	return &Store{
		client:                k8sClient,
		namespace:             namespace,
		templates:             templates,
		globalPolicyConfigMap: globalPolicyConfigMap,
	}, nil
}

func loadTemplates() (map[string]Template, error) {
	templates := make(map[string]Template)
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := templatesFS.ReadFile(path.Join("templates", entry.Name()))
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Name(), ".cedar")
		templates[name] = Template{
			Name:       name,
			PolicyText: string(data),
		}
	}
	return templates, nil
}

func (s *Store) ListTemplates() []Template {
	result := make([]Template, 0, len(s.templates))
	for _, t := range s.templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *Store) GetTemplate(name string) (Template, bool) {
	t, ok := s.templates[name]
	return t, ok
}

func configMapName(projectID string) string {
	return "cedar-attachments-" + projectID
}

func (s *Store) getAttachments(ctx context.Context, projectID string) ([]Attachment, error) {
	var cm corev1.ConfigMap
	err := s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: configMapName(projectID)}, &cm)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	data, ok := cm.Data[attachmentsKey]
	if !ok || data == "" {
		return nil, nil
	}

	var attachments []Attachment
	if err := json.Unmarshal([]byte(data), &attachments); err != nil {
		return nil, fmt.Errorf("decoding attachments: %w", err)
	}
	return attachments, nil
}

func (s *Store) ListAttachments(ctx context.Context, projectID string) ([]Attachment, error) {
	return s.getAttachments(ctx, projectID)
}

func (s *Store) CreateAttachment(ctx context.Context, projectID, templateName, userID string) (Attachment, error) {
	if _, ok := s.templates[templateName]; !ok {
		if err := s.validateCustomRole(ctx, projectID, templateName); err != nil {
			return Attachment{}, err
		}
	}

	attachments, err := s.getAttachments(ctx, projectID)
	if err != nil {
		return Attachment{}, err
	}

	att := Attachment{
		ID:           uuid.New().String()[:8],
		TemplateName: templateName,
		Principal:    userID,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	attachments = append(attachments, att)

	if err := s.saveAttachments(ctx, projectID, attachments); err != nil {
		return Attachment{}, err
	}
	return att, nil
}

func (s *Store) validateCustomRole(ctx context.Context, projectID, roleName string) error {
	var role hcpv1alpha1.CustomRole
	err := s.client.Get(ctx, client.ObjectKey{Namespace: projectID, Name: roleName}, &role)
	if errors.IsNotFound(err) {
		return fmt.Errorf("template %q not found", roleName)
	}
	if err != nil {
		return fmt.Errorf("looking up custom role %q: %w", roleName, err)
	}
	return ValidatePermissions(role.Spec.Permissions)
}

func (s *Store) DeleteAttachment(ctx context.Context, projectID, attachmentID string) error {
	attachments, err := s.getAttachments(ctx, projectID)
	if err != nil {
		return err
	}

	filtered := make([]Attachment, 0, len(attachments))
	found := false
	for _, a := range attachments {
		if a.ID == attachmentID {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	if !found {
		return fmt.Errorf("attachment %q not found", attachmentID)
	}

	return s.saveAttachments(ctx, projectID, filtered)
}

func (s *Store) saveAttachments(ctx context.Context, projectID string, attachments []Attachment) error {
	data, err := json.Marshal(attachments)
	if err != nil {
		return err
	}

	cmName := configMapName(projectID)
	var cm corev1.ConfigMap
	err = s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: cmName}, &cm)

	if errors.IsNotFound(err) {
		cm = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: s.namespace,
			},
			Data: map[string]string{
				attachmentsKey: string(data),
			},
		}
		return s.client.Create(ctx, &cm)
	}
	if err != nil {
		return err
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[attachmentsKey] = string(data)
	return s.client.Update(ctx, &cm)
}

func (s *Store) ResolvePolicies(ctx context.Context, projectID string) (string, error) {
	var resolved strings.Builder

	globalPolicies := s.loadGlobalPolicies(ctx)
	if globalPolicies != "" {
		resolved.WriteString(globalPolicies)
		resolved.WriteString("\n")
	}

	attachments, err := s.getAttachments(ctx, projectID)
	if err != nil {
		return resolved.String(), err
	}

	if globalPolicies == "" && len(attachments) == 0 {
		return "", nil
	}

	for _, att := range attachments {
		policy, err := s.resolveAttachmentPolicy(ctx, att, projectID)
		if err != nil {
			continue
		}
		resolved.WriteString(policy)
		resolved.WriteString("\n")
	}
	return resolved.String(), nil
}

func (s *Store) loadGlobalPolicies(ctx context.Context) string {
	var cm corev1.ConfigMap
	err := s.client.Get(ctx, client.ObjectKey{
		Namespace: s.namespace,
		Name:      s.globalPolicyConfigMap,
	}, &cm)
	if err != nil {
		return ""
	}
	return cm.Data[globalPoliciesKey]
}

func (s *Store) resolveAttachmentPolicy(ctx context.Context, att Attachment, projectID string) (string, error) {
	if tmpl, ok := s.templates[att.TemplateName]; ok {
		policy := tmpl.PolicyText
		policy = strings.ReplaceAll(policy, "?principal", fmt.Sprintf(`HCP::User::"%s"`, att.Principal))
		policy = strings.ReplaceAll(policy, "?resource", fmt.Sprintf(`HCP::Project::"%s"`, projectID))
		return policy, nil
	}

	var role hcpv1alpha1.CustomRole
	err := s.client.Get(ctx, client.ObjectKey{Namespace: projectID, Name: att.TemplateName}, &role)
	if err != nil {
		return "", fmt.Errorf("custom role %q not found: %w", att.TemplateName, err)
	}

	return GeneratePolicyFromPermissions(role.Spec.Permissions, role.Spec.Conditions, att.Principal, projectID), nil
}

func GeneratePolicyFromPermissions(permissions, conditions []string, principal, projectID string) string {
	var collectionPerms, resourcePerms []string
	for _, p := range permissions {
		if IsCollectionPermission(p) {
			collectionPerms = append(collectionPerms, p)
		} else {
			resourcePerms = append(resourcePerms, p)
		}
	}

	var b strings.Builder
	if len(collectionPerms) > 0 {
		writePermitBlock(&b, MapPermissionsToActions(collectionPerms), nil, principal, projectID)
	}
	if len(resourcePerms) > 0 {
		writePermitBlock(&b, MapPermissionsToActions(resourcePerms), conditions, principal, projectID)
	}
	return b.String()
}

func writePermitBlock(b *strings.Builder, actions, conditions []string, principal, projectID string) {
	actionStrs := make([]string, len(actions))
	for i, a := range actions {
		actionStrs[i] = fmt.Sprintf(`HCP::Action::"%s"`, a)
	}
	fmt.Fprintf(b, "permit (\n    principal == HCP::User::\"%s\",\n    action in [%s],\n    resource in HCP::Project::\"%s\"\n)",
		principal, strings.Join(actionStrs, ", "), projectID)
	for _, cond := range conditions {
		fmt.Fprintf(b, "\nwhen { %s }", cond)
	}
	b.WriteString(";\n")
}

type Role struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty"`
	Description string   `json:"description,omitempty"`
	Predefined  bool     `json:"predefined"`
}

func (s *Store) ListRoles(ctx context.Context, projectID string) ([]Role, error) {
	roles := s.predefinedRoles()

	var customRoles hcpv1alpha1.CustomRoleList
	if err := s.client.List(ctx, &customRoles, client.InNamespace(projectID)); err != nil {
		return roles, nil
	}
	for _, cr := range customRoles.Items {
		roles = append(roles, Role{
			Name:        cr.Name,
			Permissions: cr.Spec.Permissions,
			Description: cr.Spec.Description,
			Predefined:  false,
		})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles, nil
}

func (s *Store) GetRole(ctx context.Context, name, projectID string) (Role, bool) {
	for _, r := range s.predefinedRoles() {
		if r.Name == name {
			return r, true
		}
	}

	var cr hcpv1alpha1.CustomRole
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: projectID, Name: name}, &cr); err == nil {
		return Role{
			Name:        cr.Name,
			Permissions: cr.Spec.Permissions,
			Description: cr.Spec.Description,
			Predefined:  false,
		}, true
	}
	return Role{}, false
}

func (s *Store) predefinedRoles() []Role {
	predefined := map[string]string{
		"service-admin":  "Full access to all operations including policy management",
		"cluster-admin":  "Full cluster CRUD operations and kubeconfig access",
		"cluster-viewer": "Read-only cluster access (list and get)",
		"developer":      "Read clusters and retrieve kubeconfig",
	}
	roles := make([]Role, 0, len(predefined))
	for name, desc := range predefined {
		roles = append(roles, Role{
			Name:        name,
			Description: desc,
			Predefined:  true,
		})
	}
	return roles
}
