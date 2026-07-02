package desires

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// FirestoreMetadata holds server-managed fields extracted from a Firestore
// DocumentSnapshot. All fields carry firestore:"-" because they are not stored
// in the document body — the CRUD layer populates them from the snapshot.
type FirestoreMetadata struct {
	DocumentID string    `json:"documentID" firestore:"-"`
	UpdateTime time.Time `json:"updateTime" firestore:"-"`
	CreateTime time.Time `json:"createTime,omitempty" firestore:"-"`
}

func (m *FirestoreMetadata) GetDocumentID() string     { return m.DocumentID }
func (m *FirestoreMetadata) GetUpdateTime() time.Time  { return m.UpdateTime }
func (m *FirestoreMetadata) GetCreateTime() time.Time  { return m.CreateTime }
func (m *FirestoreMetadata) SetDocumentID(id string)   { m.DocumentID = id }
func (m *FirestoreMetadata) SetUpdateTime(t time.Time) { m.UpdateTime = t }
func (m *FirestoreMetadata) SetCreateTime(t time.Time) { m.CreateTime = t }

// ResourceReference identifies a single Kubernetes object on the management
// cluster. The Resource field is the plural lowercase form (e.g.
// "managedhostedclusters"), not the Kind.
type ResourceReference struct {
	Group     string `json:"group" firestore:"group"`
	Version   string `json:"version" firestore:"version"`
	Resource  string `json:"resource" firestore:"resource"`
	Namespace string `json:"namespace,omitempty" firestore:"namespace,omitempty"`
	Name      string `json:"name" firestore:"name"`
}

// ApplyDesire holds a single Kubernetes object to be server-side-applied to
// the management cluster.
type ApplyDesire struct {
	FirestoreMetadata `json:"firestoreMetadata"`
	Spec              ApplyDesireSpec   `json:"spec" firestore:"spec"`
	Status            ApplyDesireStatus `json:"status" firestore:"status"`
}

type ApplyDesireSpec struct {
	ManagementCluster string                `json:"managementCluster" firestore:"managementCluster"`
	ClusterID         string                `json:"clusterID" firestore:"clusterID"`
	NodePoolName      string                `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
	TargetItem        ResourceReference     `json:"targetItem" firestore:"targetItem"`
	KubeContent       *runtime.RawExtension `json:"kubeContent,omitempty" firestore:"-"`
}

type ApplyDesireStatus struct {
	Conditions                []metav1.Condition `json:"conditions,omitempty" firestore:"conditions,omitempty"`
	ObservedDesireUpdateTime  time.Time          `json:"observedDesireUpdateTime,omitempty" firestore:"observedDesireUpdateTime,omitempty"`
	AppliedResourceGeneration int64              `json:"appliedResourceGeneration,omitempty" firestore:"appliedResourceGeneration,omitempty"`
}

func (d *ApplyDesire) GetSpec() any                                 { return d.Spec }
func (d *ApplyDesire) GetStatus() any                               { return d.Status }
func (d *ApplyDesire) GetSpecKubeContent() *runtime.RawExtension    { return d.Spec.KubeContent }
func (d *ApplyDesire) SetSpecKubeContent(ext *runtime.RawExtension) { d.Spec.KubeContent = ext }
func (d *ApplyDesire) GetStatusKubeContent() *runtime.RawExtension  { return nil }
func (d *ApplyDesire) SetStatusKubeContent(_ *runtime.RawExtension) {}
func (d *ApplyDesire) DeepCopy() *ApplyDesire {
	out := *d
	if d.Spec.KubeContent != nil {
		raw := make([]byte, len(d.Spec.KubeContent.Raw))
		copy(raw, d.Spec.KubeContent.Raw)
		out.Spec.KubeContent = &runtime.RawExtension{Raw: raw}
	}
	if len(d.Status.Conditions) > 0 {
		out.Status.Conditions = make([]metav1.Condition, len(d.Status.Conditions))
		copy(out.Status.Conditions, d.Status.Conditions)
	}
	return &out
}

// DeleteDesire targets a single Kubernetes object on the management cluster
// for deletion.
type DeleteDesire struct {
	FirestoreMetadata `json:"firestoreMetadata"`
	Spec              DeleteDesireSpec   `json:"spec" firestore:"spec"`
	Status            DeleteDesireStatus `json:"status" firestore:"status"`
}

type DeleteDesireSpec struct {
	ManagementCluster string            `json:"managementCluster" firestore:"managementCluster"`
	ClusterID         string            `json:"clusterID" firestore:"clusterID"`
	NodePoolName      string            `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
	TargetItem        ResourceReference `json:"targetItem,omitempty" firestore:"targetItem"`
}

type DeleteDesireStatus struct {
	Conditions               []metav1.Condition `json:"conditions,omitempty" firestore:"conditions,omitempty"`
	ObservedDesireUpdateTime time.Time          `json:"observedDesireUpdateTime,omitempty" firestore:"observedDesireUpdateTime,omitempty"`
}

func (d *DeleteDesire) GetSpec() any                                 { return d.Spec }
func (d *DeleteDesire) GetStatus() any                               { return d.Status }
func (d *DeleteDesire) GetSpecKubeContent() *runtime.RawExtension    { return nil }
func (d *DeleteDesire) SetSpecKubeContent(_ *runtime.RawExtension)   {}
func (d *DeleteDesire) GetStatusKubeContent() *runtime.RawExtension  { return nil }
func (d *DeleteDesire) SetStatusKubeContent(_ *runtime.RawExtension) {}
func (d *DeleteDesire) DeepCopy() *DeleteDesire {
	out := *d
	if len(d.Status.Conditions) > 0 {
		out.Status.Conditions = make([]metav1.Condition, len(d.Status.Conditions))
		copy(out.Status.Conditions, d.Status.Conditions)
	}
	return &out
}

// ReadDesire indicates a Kubernetes object to watch, mirroring the live object
// into status.kubeContent.
type ReadDesire struct {
	FirestoreMetadata `json:"firestoreMetadata"`
	Spec              ReadDesireSpec   `json:"spec" firestore:"spec"`
	Status            ReadDesireStatus `json:"status" firestore:"status"`
}

type ReadDesireSpec struct {
	ManagementCluster string            `json:"managementCluster" firestore:"managementCluster"`
	ClusterID         string            `json:"clusterID" firestore:"clusterID"`
	NodePoolName      string            `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
	TargetItem        ResourceReference `json:"targetItem,omitempty" firestore:"targetItem"`
}

type ReadDesireStatus struct {
	Conditions               []metav1.Condition    `json:"conditions,omitempty" firestore:"conditions,omitempty"`
	ObservedDesireUpdateTime time.Time             `json:"observedDesireUpdateTime,omitempty" firestore:"observedDesireUpdateTime,omitempty"`
	KubeContent              *runtime.RawExtension `json:"kubeContent,omitempty" firestore:"-"`
}

func (d *ReadDesire) GetSpec() any                                   { return d.Spec }
func (d *ReadDesire) GetStatus() any                                 { return d.Status }
func (d *ReadDesire) GetSpecKubeContent() *runtime.RawExtension      { return nil }
func (d *ReadDesire) SetSpecKubeContent(_ *runtime.RawExtension)     {}
func (d *ReadDesire) GetStatusKubeContent() *runtime.RawExtension    { return d.Status.KubeContent }
func (d *ReadDesire) SetStatusKubeContent(ext *runtime.RawExtension) { d.Status.KubeContent = ext }
func (d *ReadDesire) DeepCopy() *ReadDesire {
	out := *d
	if d.Status.KubeContent != nil {
		raw := make([]byte, len(d.Status.KubeContent.Raw))
		copy(raw, d.Status.KubeContent.Raw)
		out.Status.KubeContent = &runtime.RawExtension{Raw: raw}
	}
	if len(d.Status.Conditions) > 0 {
		out.Status.Conditions = make([]metav1.Condition, len(d.Status.Conditions))
		copy(out.Status.Conditions, d.Status.Conditions)
	}
	return &out
}

// Condition type and reason constants matching the kube-applier agent.
const (
	ConditionTypeSuccessful = "Successful"
	ConditionTypeDegraded   = "Degraded"

	ConditionReasonKubeAPIError       = "KubeAPIError"
	ConditionReasonPreCheckFailed     = "PreCheckFailed"
	ConditionReasonWaitingForDeletion = "WaitingForDeletion"
	ConditionReasonNoErrors           = "NoErrors"
	ConditionReasonFailed             = "Failed"
)
