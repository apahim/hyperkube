package apiserver

import (
	"time"

	hcpv1alpha1 "github.com/apahim/hyperkube/api/v1alpha1"
	"github.com/apahim/hyperkube/internal/desires"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DesireConditionStatus struct {
	Conditions               []metav1.Condition `json:"conditions,omitempty"`
	ObservedDesireUpdateTime time.Time          `json:"observedDesireUpdateTime,omitempty"`
}

type ApplyDesireConditionStatus struct {
	DesireConditionStatus     `json:",inline"`
	AppliedResourceGeneration int64 `json:"appliedResourceGeneration,omitempty"`
}

type DesireStatus struct {
	Apply *ApplyDesireConditionStatus `json:"apply,omitempty"`
	Read  *DesireConditionStatus      `json:"read,omitempty"`
}

type ManagedHostedClusterResponse struct {
	hcpv1alpha1.ManagedHostedCluster `json:",inline"`
	DesireStatus                     *DesireStatus `json:"desireStatus,omitempty"`
}

type ManagedHostedClusterListResponse struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ManagedHostedClusterResponse `json:"items"`
}

func buildDesireStatus(apply *desires.ApplyDesire, read *desires.ReadDesire) *DesireStatus {
	ds := &DesireStatus{}
	if apply != nil && (len(apply.Status.Conditions) > 0 || !apply.Status.ObservedDesireUpdateTime.IsZero()) {
		ds.Apply = &ApplyDesireConditionStatus{
			DesireConditionStatus: DesireConditionStatus{
				Conditions:               apply.Status.Conditions,
				ObservedDesireUpdateTime: apply.Status.ObservedDesireUpdateTime,
			},
			AppliedResourceGeneration: apply.Status.AppliedResourceGeneration,
		}
	}
	if read != nil && (len(read.Status.Conditions) > 0 || !read.Status.ObservedDesireUpdateTime.IsZero()) {
		ds.Read = &DesireConditionStatus{
			Conditions:               read.Status.Conditions,
			ObservedDesireUpdateTime: read.Status.ObservedDesireUpdateTime,
		}
	}
	if ds.Apply == nil && ds.Read == nil {
		return nil
	}
	return ds
}
