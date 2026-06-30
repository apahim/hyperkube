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

package apiserver

import (
	"encoding/json"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
)

var mhcTypeMeta = metav1.TypeMeta{
	APIVersion: "hcp.gcp.hypershift.openshift.com/v1alpha1",
	Kind:       "ManagedHostedCluster",
}

var mhcListTypeMeta = metav1.TypeMeta{
	APIVersion: "hcp.gcp.hypershift.openshift.com/v1alpha1",
	Kind:       "ManagedHostedClusterList",
}

// Handler implements REST endpoints for ManagedHostedCluster resources.
type Handler struct {
	Client client.Client
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")

	var list hcpv1alpha1.ManagedHostedClusterList
	if err := h.Client.List(r.Context(), &list, client.InNamespace(namespace)); err != nil {
		writeError(w, err)
		return
	}

	list.TypeMeta = mhcListTypeMeta
	for i := range list.Items {
		list.Items[i].TypeMeta = mhcTypeMeta
	}
	writeJSON(w, http.StatusOK, &list)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")

	var obj hcpv1alpha1.ManagedHostedCluster
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	obj.Namespace = namespace
	obj.TypeMeta = mhcTypeMeta

	if err := h.Client.Create(r.Context(), &obj); err != nil {
		writeError(w, err)
		return
	}

	obj.TypeMeta = mhcTypeMeta
	writeJSON(w, http.StatusCreated, &obj)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	var obj hcpv1alpha1.ManagedHostedCluster
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, &obj); err != nil {
		writeError(w, err)
		return
	}

	obj.TypeMeta = mhcTypeMeta
	writeJSON(w, http.StatusOK, &obj)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	var obj hcpv1alpha1.ManagedHostedCluster
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	obj.Namespace = namespace
	obj.Name = name
	obj.TypeMeta = mhcTypeMeta

	if err := h.Client.Update(r.Context(), &obj); err != nil {
		writeError(w, err)
		return
	}

	obj.TypeMeta = mhcTypeMeta
	writeJSON(w, http.StatusOK, &obj)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	obj := &hcpv1alpha1.ManagedHostedCluster{
		TypeMeta: mhcTypeMeta,
	}
	obj.Namespace = namespace
	obj.Name = name

	if err := h.Client.Delete(r.Context(), obj); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) GetKubeConfig(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	var obj hcpv1alpha1.ManagedHostedCluster
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, &obj); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters":   []any{},
		"contexts":   []any{},
		"users":      []any{},
		"metadata": map[string]string{
			"cluster": name,
			"status":  "placeholder",
		},
	})
}
