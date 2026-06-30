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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
)

var hostedClusterGVK = schema.GroupVersionKind{
	Group:   "hypershift.openshift.io",
	Version: "v1beta1",
	Kind:    "HostedCluster",
}

// HostedClusterReconciler reconciles ManagedHostedCluster resources by
// building a HyperShift HostedCluster CR from the embedded spec.
type HostedClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hcp.gcp.hypershift.openshift.com,resources=managedhostedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=hcp.gcp.hypershift.openshift.com,resources=managedhostedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hcp.gcp.hypershift.openshift.com,resources=versionstreams,verbs=get;list;watch
// +kubebuilder:rbac:groups=hypershift.openshift.io,resources=hostedclusters,verbs=get;list;watch;create;update;patch;delete

func (r *HostedClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var mhc hcpv1alpha1.ManagedHostedCluster
	if err := r.Get(ctx, req.NamespacedName, &mhc); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var vs hcpv1alpha1.VersionStream
	if err := r.Get(ctx, types.NamespacedName{Name: mhc.Spec.VersionStreamRef.Name}, &vs); err != nil {
		log.Error(err, "Failed to get VersionStream", "versionStream", mhc.Spec.VersionStreamRef.Name)
		return ctrl.Result{}, err
	}

	if vs.Status.ReleaseImage == "" {
		log.Info("VersionStream image not yet resolved, requeueing", "versionStream", vs.Name)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	hc, err := buildHostedCluster(&mhc, vs.Status.ReleaseImage)
	if err != nil {
		log.Error(err, "Failed to build HostedCluster from spec")
		return ctrl.Result{}, err
	}

	hc.SetName(mhc.Name)
	hc.SetNamespace(mhc.Namespace)

	if err := ctrl.SetControllerReference(&mhc, hc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(hc.GroupVersionKind())
	err = r.Get(ctx, types.NamespacedName{Name: hc.GetName(), Namespace: hc.GetNamespace()}, existing)
	if errors.IsNotFound(err) {
		log.Info("Creating HostedCluster", "name", hc.GetName())
		if err := r.Create(ctx, hc); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.setCondition(ctx, &mhc, "HostedClusterCreated", metav1.ConditionTrue, "Created", "HostedCluster CR created successfully")
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	hc.SetResourceVersion(existing.GetResourceVersion())
	log.Info("Updating HostedCluster", "name", hc.GetName())
	if err := r.Update(ctx, hc); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.setCondition(ctx, &mhc, "HostedClusterCreated", metav1.ConditionTrue, "Updated", "HostedCluster CR updated successfully")
}

func buildHostedCluster(mhc *hcpv1alpha1.ManagedHostedCluster, releaseImage string) (*unstructured.Unstructured, error) {
	specJSON, err := json.Marshal(mhc.Spec.HostedCluster)
	if err != nil {
		return nil, fmt.Errorf("marshaling hosted cluster spec: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(specJSON, &specMap); err != nil {
		return nil, fmt.Errorf("unmarshaling hosted cluster spec: %w", err)
	}

	// Override release image with the VersionStream-resolved value.
	if release, ok := specMap["release"].(map[string]any); ok {
		release["image"] = releaseImage
	} else {
		specMap["release"] = map[string]any{"image": releaseImage}
	}

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": hostedClusterGVK.Group + "/" + hostedClusterGVK.Version,
			"kind":       hostedClusterGVK.Kind,
			"spec":       specMap,
		},
	}

	return obj, nil
}

func (r *HostedClusterReconciler) setCondition(ctx context.Context, mhc *hcpv1alpha1.ManagedHostedCluster, condType string, status metav1.ConditionStatus, reason, message string) error {
	condition := metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	existing := false
	for i, c := range mhc.Status.Conditions {
		if c.Type == condType {
			mhc.Status.Conditions[i] = condition
			existing = true
			break
		}
	}
	if !existing {
		mhc.Status.Conditions = append(mhc.Status.Conditions, condition)
	}

	return r.Status().Update(ctx, mhc)
}

// SetupWithManager sets up the controller with the Manager.
func (r *HostedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hcpv1alpha1.ManagedHostedCluster{}).
		Named("hostedcluster").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10,
		}).
		Watches(
			&hcpv1alpha1.VersionStream{},
			handler.EnqueueRequestsFromMapFunc(r.mapVersionStreamToManagedHostedClusters),
		).
		Complete(r)
}

func (r *HostedClusterReconciler) mapVersionStreamToManagedHostedClusters(ctx context.Context, obj client.Object) []reconcile.Request {
	vs, ok := obj.(*hcpv1alpha1.VersionStream)
	if !ok {
		return nil
	}

	var mhcList hcpv1alpha1.ManagedHostedClusterList
	if err := r.List(ctx, &mhcList); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, mhc := range mhcList.Items {
		if mhc.Spec.VersionStreamRef.Name == vs.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      mhc.Name,
					Namespace: mhc.Namespace,
				},
			})
		}
	}
	return requests
}
