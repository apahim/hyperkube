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
	"k8s.io/apimachinery/pkg/runtime"

	hcpv1alpha1 "github.com/apahim/hyperkube/api/v1alpha1"
	"github.com/apahim/hyperkube/internal/desires"
	"github.com/apahim/hyperkube/internal/placement"
)

const (
	mhcGroup    = "hcp.gcp.hypershift.openshift.com"
	mhcVersion  = "v1alpha1"
	mhcResource = "managedhostedclusters"
)

var mhcTypeMeta = metav1.TypeMeta{
	APIVersion: mhcGroup + "/" + mhcVersion,
	Kind:       "ManagedHostedCluster",
}

var mhcListTypeMeta = metav1.TypeMeta{
	APIVersion: mhcGroup + "/" + mhcVersion,
	Kind:       "ManagedHostedClusterList",
}

// Handler implements REST endpoints for ManagedHostedCluster resources.
type Handler struct {
	Placement *placement.Client
	SpecsDB   func(managementCluster string) (*desires.DBClient, error)
	StatusDB  func(managementCluster string) (*desires.DBClient, error)
}

func mhcResourceRef(namespace, name string) desires.ResourceReference {
	return desires.ResourceReference{
		Group:     mhcGroup,
		Version:   mhcVersion,
		Resource:  mhcResource,
		Namespace: namespace,
		Name:      name,
	}
}

func mhcDocumentID(namespace, name string) string {
	return desires.NewDocumentID(desires.TaskKey, mhcGroup, mhcVersion, mhcResource, namespace, name)
}

func nsDocumentID(namespace string) string {
	return desires.NewDocumentID(desires.TaskKey, "", "v1", "namespaces", "", namespace)
}

func nsResourceRef(namespace string) desires.ResourceReference {
	return desires.ResourceReference{
		Version:  "v1",
		Resource: "namespaces",
		Name:     namespace,
	}
}

func serializeNamespace(name string) (*runtime.RawExtension, error) {
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": name},
	})
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{Raw: raw}, nil
}

func serializeCR(obj *hcpv1alpha1.ManagedHostedCluster) (*runtime.RawExtension, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{Raw: raw}, nil
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

	kubeContent, err := serializeCR(&obj)
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "Failed to serialize resource")
		return
	}

	ctx := r.Context()

	_, err = h.Placement.GetClusterMapping(ctx, namespace, obj.Name)
	if err == nil {
		writeErrorMsg(w, http.StatusConflict, "cluster already exists")
		return
	}
	if !desires.IsNotFoundError(err) {
		writeError(w, err)
		return
	}

	mc, err := h.Placement.Allocate(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	specsDB, err := h.SpecsDB(mc)
	if err != nil {
		writeError(w, err)
		return
	}

	nsKubeContent, err := serializeNamespace(namespace)
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "Failed to serialize namespace")
		return
	}

	nsDesire := desires.ApplyDesire{
		Spec: desires.ApplyDesireSpec{
			ManagementCluster: mc,
			TargetItem:        nsResourceRef(namespace),
			KubeContent:       nsKubeContent,
		},
	}
	nsDesire.SetDocumentID(nsDocumentID(namespace))

	if _, err := specsDB.ApplyDesires().Create(ctx, &nsDesire); err != nil && !desires.IsAlreadyExistsError(err) {
		writeError(w, err)
		return
	}

	ref := mhcResourceRef(namespace, obj.Name)
	docID := mhcDocumentID(namespace, obj.Name)

	applyDesire := desires.ApplyDesire{
		Spec: desires.ApplyDesireSpec{
			ManagementCluster: mc,
			ClusterID:         obj.Spec.ClusterID,
			TargetItem:        ref,
			KubeContent:       kubeContent,
		},
	}
	applyDesire.SetDocumentID(docID)

	if _, err := specsDB.ApplyDesires().Create(ctx, &applyDesire); err != nil {
		writeError(w, err)
		return
	}

	readDesire := desires.ReadDesire{
		Spec: desires.ReadDesireSpec{
			ManagementCluster: mc,
			ClusterID:         obj.Spec.ClusterID,
			TargetItem:        ref,
		},
	}
	readDesire.SetDocumentID(docID)

	if _, err := specsDB.ReadDesires().Create(ctx, &readDesire); err != nil {
		writeError(w, err)
		return
	}

	if err := h.Placement.SetClusterMapping(ctx, namespace, obj.Name, mc); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, &obj)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	ctx := r.Context()

	mc, err := h.Placement.GetClusterMapping(ctx, namespace, name)
	if err != nil {
		writeError(w, err)
		return
	}

	docID := mhcDocumentID(namespace, name)

	statusDB, err := h.StatusDB(mc)
	if err != nil {
		writeError(w, err)
		return
	}

	readDesire, err := statusDB.ReadDesires().Get(ctx, docID)
	if err != nil && !desires.IsNotFoundError(err) {
		writeError(w, err)
		return
	}

	applyDesire, err := statusDB.ApplyDesires().Get(ctx, docID)
	if err != nil && !desires.IsNotFoundError(err) {
		writeError(w, err)
		return
	}

	ds := buildDesireStatus(applyDesire, readDesire)

	if readDesire != nil && readDesire.Status.KubeContent != nil {
		var obj hcpv1alpha1.ManagedHostedCluster
		if err := json.Unmarshal(readDesire.Status.KubeContent.Raw, &obj); err != nil {
			writeErrorMsg(w, http.StatusInternalServerError, "Failed to deserialize resource from status")
			return
		}
		obj.TypeMeta = mhcTypeMeta
		writeJSON(w, http.StatusOK, &ManagedHostedClusterResponse{
			ManagedHostedCluster: obj,
			DesireStatus:         ds,
		})
		return
	}

	// Agent hasn't synced yet — fall back to the spec's kubeContent.
	specsDB, err := h.SpecsDB(mc)
	if err != nil {
		writeError(w, err)
		return
	}

	specApplyDesire, err := specsDB.ApplyDesires().Get(ctx, docID)
	if err != nil {
		writeError(w, err)
		return
	}

	if specApplyDesire.Spec.KubeContent != nil {
		var obj hcpv1alpha1.ManagedHostedCluster
		if err := json.Unmarshal(specApplyDesire.Spec.KubeContent.Raw, &obj); err != nil {
			writeErrorMsg(w, http.StatusInternalServerError, "Failed to deserialize resource from spec")
			return
		}
		obj.TypeMeta = mhcTypeMeta
		writeJSON(w, http.StatusOK, &ManagedHostedClusterResponse{
			ManagedHostedCluster: obj,
			DesireStatus:         ds,
		})
		return
	}

	writeErrorMsg(w, http.StatusNotFound, "Resource not found")
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	ctx := r.Context()

	mappings, err := h.Placement.ListClusterMappings(ctx, namespace)
	if err != nil {
		writeError(w, err)
		return
	}

	// Group mappings by management cluster.
	mcToNames := make(map[string]map[string]bool)
	for _, m := range mappings {
		if mcToNames[m.ManagementCluster] == nil {
			mcToNames[m.ManagementCluster] = make(map[string]bool)
		}
		mcToNames[m.ManagementCluster][m.Name] = true
	}

	var items []ManagedHostedClusterResponse

	for mc, nameSet := range mcToNames {
		statusDB, err := h.StatusDB(mc)
		if err != nil {
			writeError(w, err)
			return
		}

		for name := range nameSet {
			docID := mhcDocumentID(namespace, name)
			rd, err := statusDB.ReadDesires().Get(ctx, docID)
			if err != nil {
				continue
			}
			if rd.Status.KubeContent == nil {
				continue
			}
			var obj hcpv1alpha1.ManagedHostedCluster
			if err := json.Unmarshal(rd.Status.KubeContent.Raw, &obj); err != nil {
				continue
			}
			obj.TypeMeta = mhcTypeMeta

			ad, _ := statusDB.ApplyDesires().Get(ctx, docID)
			items = append(items, ManagedHostedClusterResponse{
				ManagedHostedCluster: obj,
				DesireStatus:         buildDesireStatus(ad, rd),
			})
		}
	}

	list := ManagedHostedClusterListResponse{
		TypeMeta: mhcListTypeMeta,
		Items:    items,
	}
	if list.Items == nil {
		list.Items = []ManagedHostedClusterResponse{}
	}
	writeJSON(w, http.StatusOK, &list)
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

	ctx := r.Context()

	mc, err := h.Placement.GetClusterMapping(ctx, namespace, name)
	if err != nil {
		writeError(w, err)
		return
	}

	specsDB, err := h.SpecsDB(mc)
	if err != nil {
		writeError(w, err)
		return
	}

	docID := mhcDocumentID(namespace, name)

	existing, err := specsDB.ApplyDesires().Get(ctx, docID)
	if err != nil {
		writeError(w, err)
		return
	}

	kubeContent, err := serializeCR(&obj)
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "Failed to serialize resource")
		return
	}

	existing.Spec.KubeContent = kubeContent

	if _, err := specsDB.ApplyDesires().Replace(ctx, existing); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, &obj)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	ctx := r.Context()

	mc, err := h.Placement.GetClusterMapping(ctx, namespace, name)
	if err != nil {
		writeError(w, err)
		return
	}

	specsDB, err := h.SpecsDB(mc)
	if err != nil {
		writeError(w, err)
		return
	}

	docID := mhcDocumentID(namespace, name)

	// Look up existing ApplyDesire to get ClusterID for the DeleteDesire.
	existing, err := specsDB.ApplyDesires().Get(ctx, docID)
	if err != nil {
		writeError(w, err)
		return
	}

	// Order matters: remove ApplyDesire before writing DeleteDesire.
	if err := specsDB.ApplyDesires().Delete(ctx, docID); err != nil {
		writeError(w, err)
		return
	}

	if err := specsDB.ReadDesires().Delete(ctx, docID); err != nil {
		writeError(w, err)
		return
	}

	ref := mhcResourceRef(namespace, name)
	deleteDesire := desires.DeleteDesire{
		Spec: desires.DeleteDesireSpec{
			ManagementCluster: mc,
			ClusterID:         existing.Spec.ClusterID,
			TargetItem:        ref,
		},
	}
	deleteDesire.SetDocumentID(docID)

	if _, err := specsDB.DeleteDesires().Create(ctx, &deleteDesire); err != nil {
		writeError(w, err)
		return
	}

	if err := h.Placement.Release(ctx, mc); err != nil {
		writeError(w, err)
		return
	}

	if err := h.Placement.DeleteClusterMapping(ctx, namespace, name); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) GetKubeConfig(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	if _, err := h.Placement.GetClusterMapping(r.Context(), namespace, name); err != nil {
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
