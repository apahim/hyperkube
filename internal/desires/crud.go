package desires

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	CollectionApplyDesires  = "applydesires"
	CollectionDeleteDesires = "deletedesires"
	CollectionReadDesires   = "readdesires"
)

// FirestoreMetadataAccessor provides generic access to the server-managed
// metadata fields on every desire type.
type FirestoreMetadataAccessor interface {
	GetDocumentID() string
	GetUpdateTime() time.Time
	GetCreateTime() time.Time
	SetDocumentID(string)
	SetUpdateTime(time.Time)
	SetCreateTime(time.Time)
}

// SpecStatusAccessor provides generic access to the spec and status fields
// used by Replace to build firestore.Update entries.
type SpecStatusAccessor interface {
	GetSpec() any
	GetStatus() any
}

// desire is the type constraint for the generic CRUD operations.
type desire[T any] interface {
	*T
	FirestoreMetadataAccessor
	SpecStatusAccessor
	KubeContentAccessor
	DeepCopy() *T
}

// ResourceCRUD is the generic CRUD interface for a single Firestore collection.
type ResourceCRUD[T any] interface {
	Get(ctx context.Context, documentID string) (*T, error)
	List(ctx context.Context) ([]*T, error)
	Create(ctx context.Context, obj *T) (*T, error)
	Replace(ctx context.Context, obj *T) (*T, error)
	Delete(ctx context.Context, documentID string) error
}

// firestoreCRUD implements ResourceCRUD[T] against a single Firestore collection.
type firestoreCRUD[T any, PT desire[T]] struct {
	client     *firestore.Client
	collection string
}

func (c *firestoreCRUD[T, PT]) col() *firestore.CollectionRef {
	return c.client.Collection(c.collection)
}

func (c *firestoreCRUD[T, PT]) Get(ctx context.Context, documentID string) (*T, error) {
	snap, err := c.col().Doc(documentID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, NewNotFoundError()
		}
		return nil, fmt.Errorf("firestore get %s/%s: %w", c.collection, documentID, err)
	}
	return snapshotToDesire[T, PT](snap)
}

func (c *firestoreCRUD[T, PT]) List(ctx context.Context) ([]*T, error) {
	snaps, err := c.col().Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore list %s: %w", c.collection, err)
	}
	result := make([]*T, 0, len(snaps))
	for _, snap := range snaps {
		obj, err := snapshotToDesire[T, PT](snap)
		if err != nil {
			return nil, fmt.Errorf("firestore list %s: convert %s: %w", c.collection, snap.Ref.ID, err)
		}
		result = append(result, obj)
	}
	return result, nil
}

func (c *firestoreCRUD[T, PT]) Create(ctx context.Context, obj *T) (*T, error) {
	pt := PT(obj)
	docID := pt.GetDocumentID()
	if docID == "" {
		return nil, fmt.Errorf("firestore create %s: DocumentID is empty", c.collection)
	}

	data, err := toFirestoreMap[T, PT](pt)
	if err != nil {
		return nil, fmt.Errorf("firestore create %s/%s: %w", c.collection, docID, err)
	}
	wr, err := c.col().Doc(docID).Create(ctx, data)
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return nil, status.Errorf(codes.AlreadyExists, "document %s/%s already exists", c.collection, docID)
		}
		return nil, fmt.Errorf("firestore create %s/%s: %w", c.collection, docID, err)
	}
	out := pt.DeepCopy()
	op := PT(out)
	op.SetDocumentID(docID)
	op.SetUpdateTime(wr.UpdateTime)
	op.SetCreateTime(wr.UpdateTime)
	return out, nil
}

func toFirestoreMap[T any, PT desire[T]](pt PT) (map[string]any, error) {
	data := map[string]any{
		"spec":   pt.GetSpec(),
		"status": pt.GetStatus(),
	}
	kubeFields, err := kubeContentWriteMap(pt)
	if err != nil {
		return nil, err
	}
	for k, v := range kubeFields {
		data[k] = v
	}
	return data, nil
}

func (c *firestoreCRUD[T, PT]) Replace(ctx context.Context, obj *T) (*T, error) {
	pt := PT(obj)
	docID := pt.GetDocumentID()
	updates := []firestore.Update{
		{Path: "spec", Value: pt.GetSpec()},
		{Path: "status", Value: pt.GetStatus()},
	}
	kubeUpdates, err := kubeContentWriteUpdates(pt)
	if err != nil {
		return nil, fmt.Errorf("firestore replace %s/%s: %w", c.collection, docID, err)
	}
	updates = append(updates, kubeUpdates...)
	wr, err := c.col().Doc(docID).Update(ctx, updates, firestore.LastUpdateTime(pt.GetUpdateTime()))
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			return nil, NewPreconditionFailedError()
		}
		if status.Code(err) == codes.NotFound {
			return nil, NewNotFoundError()
		}
		return nil, fmt.Errorf("firestore replace %s/%s: %w", c.collection, docID, err)
	}
	out := pt.DeepCopy()
	op := PT(out)
	op.SetUpdateTime(wr.UpdateTime)
	return out, nil
}

func (c *firestoreCRUD[T, PT]) Delete(ctx context.Context, documentID string) error {
	_, err := c.col().Doc(documentID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore delete %s/%s: %w", c.collection, documentID, err)
	}
	return nil
}

func snapshotToDesire[T any, PT desire[T]](snap *firestore.DocumentSnapshot) (*T, error) {
	var obj T
	if err := snap.DataTo(&obj); err != nil {
		return nil, fmt.Errorf("deserialize %s: %w", snap.Ref.ID, err)
	}
	pt := PT(&obj)
	pt.SetDocumentID(snap.Ref.ID)
	pt.SetUpdateTime(snap.UpdateTime)
	pt.SetCreateTime(snap.CreateTime)
	if err := kubeContentReadFromSnapshot(pt, snap.Data()); err != nil {
		return nil, fmt.Errorf("deserialize kubeContent %s: %w", snap.Ref.ID, err)
	}
	return &obj, nil
}
