package cedar

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	attachmentsKey        = "attachments.json"
	globalPoliciesKey     = "policies.cedar"
	DefaultGlobalPolicyCM = "cedar-global-policies"

	collectionAttachments    = "attachments"
	collectionGlobalPolicies = "global-policies"
	collectionCustomRoles    = "custom-roles"
	collectionUserProjects   = "user-projects"

	globalPoliciesDocID = "default"
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

// customRoleDoc is the Firestore document schema for custom roles.
type customRoleDoc struct {
	Permissions []string `firestore:"permissions" json:"permissions"`
	Description string   `firestore:"description,omitempty" json:"description,omitempty"`
	Conditions  []string `firestore:"conditions,omitempty" json:"conditions,omitempty"`
}

// userProjectsDoc is the Firestore document schema for the user-projects reverse index.
type userProjectsDoc struct {
	Projects []string `firestore:"projects" json:"projects"`
}

type Store struct {
	fsClient  *firestore.Client
	templates map[string]Template
}

func NewStore(fsClient *firestore.Client) (*Store, error) {
	templates, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}
	return &Store{
		fsClient:  fsClient,
		templates: templates,
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

func customRoleDocID(projectID, roleName string) string {
	return projectID + ":" + roleName
}

func (s *Store) getAttachments(ctx context.Context, projectID string) ([]Attachment, error) {
	snap, err := s.fsClient.Collection(collectionAttachments).Doc(projectID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	data := snap.Data()
	raw, ok := data[attachmentsKey]
	if !ok {
		return nil, nil
	}

	jsonStr, ok := raw.(string)
	if !ok || jsonStr == "" {
		return nil, nil
	}

	var attachments []Attachment
	if err := json.Unmarshal([]byte(jsonStr), &attachments); err != nil {
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

	if err := s.addUserProject(ctx, userID, projectID); err != nil {
		return Attachment{}, fmt.Errorf("updating user-projects index: %w", err)
	}

	return att, nil
}

func (s *Store) validateCustomRole(ctx context.Context, projectID, roleName string) error {
	docID := customRoleDocID(projectID, roleName)
	snap, err := s.fsClient.Collection(collectionCustomRoles).Doc(docID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return fmt.Errorf("template %q not found", roleName)
	}
	if err != nil {
		return fmt.Errorf("looking up custom role %q: %w", roleName, err)
	}

	var role customRoleDoc
	if err := snap.DataTo(&role); err != nil {
		return fmt.Errorf("deserializing custom role %q: %w", roleName, err)
	}
	return ValidatePermissions(role.Permissions)
}

func (s *Store) DeleteAttachment(ctx context.Context, projectID, attachmentID string) error {
	attachments, err := s.getAttachments(ctx, projectID)
	if err != nil {
		return err
	}

	var deletedUserID string
	filtered := make([]Attachment, 0, len(attachments))
	found := false
	for _, a := range attachments {
		if a.ID == attachmentID {
			found = true
			deletedUserID = a.Principal
			continue
		}
		filtered = append(filtered, a)
	}
	if !found {
		return fmt.Errorf("attachment %q not found", attachmentID)
	}

	if err := s.saveAttachments(ctx, projectID, filtered); err != nil {
		return err
	}

	// Check if user still has attachments for this project.
	hasRemaining := false
	for _, a := range filtered {
		if a.Principal == deletedUserID {
			hasRemaining = true
			break
		}
	}
	if !hasRemaining && deletedUserID != "" {
		if err := s.removeUserProject(ctx, deletedUserID, projectID); err != nil {
			return fmt.Errorf("updating user-projects index: %w", err)
		}
	}

	return nil
}

func (s *Store) saveAttachments(ctx context.Context, projectID string, attachments []Attachment) error {
	data, err := json.Marshal(attachments)
	if err != nil {
		return err
	}
	_, err = s.fsClient.Collection(collectionAttachments).Doc(projectID).Set(ctx, map[string]any{
		attachmentsKey: string(data),
	})
	return err
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
	snap, err := s.fsClient.Collection(collectionGlobalPolicies).Doc(globalPoliciesDocID).Get(ctx)
	if err != nil {
		return ""
	}
	data := snap.Data()
	policies, _ := data[globalPoliciesKey].(string)
	return policies
}

func (s *Store) resolveAttachmentPolicy(ctx context.Context, att Attachment, projectID string) (string, error) {
	if tmpl, ok := s.templates[att.TemplateName]; ok {
		policy := tmpl.PolicyText
		policy = strings.ReplaceAll(policy, "?principal", fmt.Sprintf(`HCP::User::"%s"`, att.Principal))
		policy = strings.ReplaceAll(policy, "?resource", fmt.Sprintf(`HCP::Project::"%s"`, projectID))
		return policy, nil
	}

	docID := customRoleDocID(projectID, att.TemplateName)
	snap, err := s.fsClient.Collection(collectionCustomRoles).Doc(docID).Get(ctx)
	if err != nil {
		return "", fmt.Errorf("custom role %q not found: %w", att.TemplateName, err)
	}

	var role customRoleDoc
	if err := snap.DataTo(&role); err != nil {
		return "", fmt.Errorf("deserializing custom role %q: %w", att.TemplateName, err)
	}

	return GeneratePolicyFromPermissions(role.Permissions, role.Conditions, att.Principal, projectID), nil
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

	prefix := projectID + ":"
	snaps, err := s.fsClient.Collection(collectionCustomRoles).Documents(ctx).GetAll()
	if err != nil {
		return roles, nil
	}
	for _, snap := range snaps {
		if !strings.HasPrefix(snap.Ref.ID, prefix) {
			continue
		}
		var role customRoleDoc
		if err := snap.DataTo(&role); err != nil {
			continue
		}
		roleName := strings.TrimPrefix(snap.Ref.ID, prefix)
		roles = append(roles, Role{
			Name:        roleName,
			Permissions: role.Permissions,
			Description: role.Description,
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

	docID := customRoleDocID(projectID, name)
	snap, err := s.fsClient.Collection(collectionCustomRoles).Doc(docID).Get(ctx)
	if err != nil {
		return Role{}, false
	}

	var role customRoleDoc
	if err := snap.DataTo(&role); err != nil {
		return Role{}, false
	}
	return Role{
		Name:        name,
		Permissions: role.Permissions,
		Description: role.Description,
		Predefined:  false,
	}, true
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

// ListUserProjects returns the list of project IDs the user has access to.
func (s *Store) ListUserProjects(ctx context.Context, userID string) ([]string, error) {
	snap, err := s.fsClient.Collection(collectionUserProjects).Doc(userID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user-projects for %s: %w", userID, err)
	}
	var doc userProjectsDoc
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("deserialize user-projects for %s: %w", userID, err)
	}
	return doc.Projects, nil
}

func (s *Store) addUserProject(ctx context.Context, userID, projectID string) error {
	ref := s.fsClient.Collection(collectionUserProjects).Doc(userID)
	return s.fsClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return tx.Set(ref, userProjectsDoc{Projects: []string{projectID}})
		}
		if err != nil {
			return err
		}
		var doc userProjectsDoc
		if err := snap.DataTo(&doc); err != nil {
			return err
		}
		for _, p := range doc.Projects {
			if p == projectID {
				return nil
			}
		}
		doc.Projects = append(doc.Projects, projectID)
		return tx.Set(ref, doc)
	})
}

func (s *Store) removeUserProject(ctx context.Context, userID, projectID string) error {
	ref := s.fsClient.Collection(collectionUserProjects).Doc(userID)
	return s.fsClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var doc userProjectsDoc
		if err := snap.DataTo(&doc); err != nil {
			return err
		}
		filtered := make([]string, 0, len(doc.Projects))
		for _, p := range doc.Projects {
			if p != projectID {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return tx.Delete(ref)
		}
		doc.Projects = filtered
		return tx.Set(ref, doc)
	})
}
