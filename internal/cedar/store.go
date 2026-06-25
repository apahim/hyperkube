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
)

const attachmentsKey = "attachments.json"

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
	client    client.Client
	namespace string
	templates map[string]Template
}

func NewStore(k8sClient client.Client, namespace string) (*Store, error) {
	templates, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}
	return &Store{
		client:    k8sClient,
		namespace: namespace,
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
		return Attachment{}, fmt.Errorf("template %q not found", templateName)
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
	attachments, err := s.getAttachments(ctx, projectID)
	if err != nil {
		return "", err
	}
	if len(attachments) == 0 {
		return "", nil
	}

	var resolved strings.Builder
	for _, att := range attachments {
		tmpl, ok := s.templates[att.TemplateName]
		if !ok {
			continue
		}
		policy := tmpl.PolicyText
		policy = strings.ReplaceAll(policy, "?principal", fmt.Sprintf(`HCP::User::"%s"`, att.Principal))
		policy = strings.ReplaceAll(policy, "?resource", fmt.Sprintf(`HCP::Project::"%s"`, projectID))
		resolved.WriteString(policy)
		resolved.WriteString("\n")
	}
	return resolved.String(), nil
}
