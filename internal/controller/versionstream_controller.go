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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
	"github.com/gcp-hcp/gcp-hcp-backend/internal/cincinnati"
)

// VersionStreamReconciler reconciles a VersionStream object
type VersionStreamReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver cincinnati.VersionResolver
}

// +kubebuilder:rbac:groups=hcp.gcp.hypershift.openshift.com,resources=versionstreams,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hcp.gcp.hypershift.openshift.com,resources=versionstreams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hcp.gcp.hypershift.openshift.com,resources=versionstreams/finalizers,verbs=update

func (r *VersionStreamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var vs hcpv1alpha1.VersionStream
	if err := r.Get(ctx, req.NamespacedName, &vs); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	image, resolvedVersion, channel, err := r.Resolver.ResolveVersion(
		ctx,
		vs.Spec.TargetVersion,
		vs.Spec.ChannelGroup,
		vs.Spec.Arch,
	)
	if err != nil {
		log.Error(err, "Failed to resolve version", "targetVersion", vs.Spec.TargetVersion)
		meta.SetStatusCondition(&vs.Status.Conditions, metav1.Condition{
			Type:               "ImageResolved",
			Status:             metav1.ConditionFalse,
			Reason:             "ResolutionFailed",
			Message:            err.Error(),
			ObservedGeneration: vs.Generation,
		})
		if statusErr := r.Status().Update(ctx, &vs); statusErr != nil {
			log.Error(statusErr, "Failed to update VersionStream status")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	log.Info("Resolved version", "targetVersion", vs.Spec.TargetVersion, "resolvedVersion", resolvedVersion, "image", image)

	vs.Status.ReleaseImage = image
	vs.Status.ResolvedVersion = resolvedVersion
	vs.Status.Channel = channel
	meta.SetStatusCondition(&vs.Status.Conditions, metav1.Condition{
		Type:               "ImageResolved",
		Status:             metav1.ConditionTrue,
		Reason:             "Resolved",
		Message:            "Resolved version " + resolvedVersion + " to " + image,
		ObservedGeneration: vs.Generation,
	})

	if err := r.Status().Update(ctx, &vs); err != nil {
		log.Error(err, "Failed to update VersionStream status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VersionStreamReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hcpv1alpha1.VersionStream{}).
		Named("versionstream").
		Complete(r)
}
