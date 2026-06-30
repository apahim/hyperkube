package cedar

import (
	"encoding/json"
	"net/http"
)

type AuthzHandler struct {
	Store *Store
}

func (h *AuthzHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	writeAuthzJSON(w, http.StatusOK, h.Store.ListTemplates())
}

func (h *AuthzHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tmpl, ok := h.Store.GetTemplate(name)
	if !ok {
		writeAuthzError(w, http.StatusNotFound, "template not found")
		return
	}
	writeAuthzJSON(w, http.StatusOK, tmpl)
}

func (h *AuthzHandler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	attachments, err := h.Store.ListAttachments(r.Context(), namespace)
	if err != nil {
		writeAuthzError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if attachments == nil {
		attachments = []Attachment{}
	}
	writeAuthzJSON(w, http.StatusOK, attachments)
}

type createAttachmentRequest struct {
	TemplateName string `json:"template_name"`
	UserID       string `json:"user_id"`
}

func (h *AuthzHandler) CreateAttachment(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")

	var req createAttachmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAuthzError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.TemplateName == "" || req.UserID == "" {
		writeAuthzError(w, http.StatusBadRequest, "template_name and user_id are required")
		return
	}

	att, err := h.Store.CreateAttachment(r.Context(), namespace, req.TemplateName, req.UserID)
	if err != nil {
		writeAuthzError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeAuthzJSON(w, http.StatusCreated, att)
}

func (h *AuthzHandler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	id := r.PathValue("id")

	if err := h.Store.DeleteAttachment(r.Context(), namespace, id); err != nil {
		writeAuthzError(w, http.StatusNotFound, err.Error())
		return
	}

	writeAuthzJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AuthzHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	roles, err := h.Store.ListRoles(r.Context(), namespace)
	if err != nil {
		writeAuthzError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAuthzJSON(w, http.StatusOK, roles)
}

func (h *AuthzHandler) GetRole(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	role, ok := h.Store.GetRole(r.Context(), name, namespace)
	if !ok {
		writeAuthzError(w, http.StatusNotFound, "role not found")
		return
	}
	writeAuthzJSON(w, http.StatusOK, role)
}
