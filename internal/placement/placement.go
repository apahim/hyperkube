package placement

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	collectionMgmtClusters = "managementclusters"
	collectionClusterMap   = "clustermap"

	fieldCapacity    = "capacity"
	fieldAllocated   = "allocated"
	fieldMgmtCluster = "managementCluster"
)

// ClusterMapping maps a namespace/name pair to the management cluster hosting it.
type ClusterMapping struct {
	Namespace         string `json:"namespace" firestore:"-"`
	Name              string `json:"name" firestore:"-"`
	ManagementCluster string `json:"managementCluster" firestore:"managementCluster"`
}

// Client provides placement decisions and cluster-to-MC mapping backed by
// Firestore.
type Client struct {
	fsClient *firestore.Client
}

// NewClient creates a placement client backed by the given Firestore client
// (which should point to the placement database).
func NewClient(fsClient *firestore.Client) *Client {
	return &Client{fsClient: fsClient}
}

// Allocate picks a management cluster with available capacity and atomically
// increments its allocated count. Returns the management cluster name.
func (c *Client) Allocate(ctx context.Context) (string, error) {
	var chosen string
	err := c.fsClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Firestore doesn't support cross-field comparisons (allocated < capacity),
		// so we read all docs and filter in-memory.
		col := c.fsClient.Collection(collectionMgmtClusters)
		allSnaps, err := tx.Documents(col).GetAll()
		if err != nil {
			return fmt.Errorf("list management clusters: %w", err)
		}
		if len(allSnaps) == 0 {
			return fmt.Errorf("no management clusters configured")
		}

		type mcDoc struct {
			ref       *firestore.DocumentRef
			capacity  int64
			allocated int64
		}

		var best *mcDoc
		for _, snap := range allSnaps {
			data := snap.Data()
			cap, _ := toInt64(data[fieldCapacity])
			alloc, _ := toInt64(data[fieldAllocated])
			if alloc >= cap {
				continue
			}
			if best == nil || alloc < best.allocated {
				best = &mcDoc{ref: snap.Ref, capacity: cap, allocated: alloc}
			}
		}
		if best == nil {
			return fmt.Errorf("no management cluster capacity available")
		}

		if err := tx.Update(best.ref, []firestore.Update{
			{Path: fieldAllocated, Value: best.allocated + 1},
		}); err != nil {
			return fmt.Errorf("increment allocated: %w", err)
		}
		chosen = best.ref.ID
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("placement allocate: %w", err)
	}
	return chosen, nil
}

// Release decrements the allocated count for the given management cluster.
func (c *Client) Release(ctx context.Context, managementCluster string) error {
	err := c.fsClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := c.fsClient.Collection(collectionMgmtClusters).Doc(managementCluster)
		snap, err := tx.Get(ref)
		if err != nil {
			return fmt.Errorf("get management cluster %s: %w", managementCluster, err)
		}
		data := snap.Data()
		alloc, _ := toInt64(data[fieldAllocated])
		if alloc <= 0 {
			return nil
		}
		return tx.Update(ref, []firestore.Update{
			{Path: fieldAllocated, Value: alloc - 1},
		})
	})
	if err != nil {
		return fmt.Errorf("placement release: %w", err)
	}
	return nil
}

// clusterMapDocID builds a Firestore-safe document ID for the clustermap.
// Firestore document IDs cannot contain '/', so we use a ':' separator.
func clusterMapDocID(namespace, name string) string {
	return namespace + ":" + name
}

// SetClusterMapping records which management cluster hosts a given
// namespace/name cluster.
func (c *Client) SetClusterMapping(ctx context.Context, namespace, name, managementCluster string) error {
	docID := clusterMapDocID(namespace, name)
	_, err := c.fsClient.Collection(collectionClusterMap).Doc(docID).Set(ctx, map[string]any{
		fieldMgmtCluster: managementCluster,
		"namespace":      namespace,
		"name":           name,
	})
	if err != nil {
		return fmt.Errorf("set cluster mapping %s: %w", docID, err)
	}
	return nil
}

// GetClusterMapping returns the management cluster for a given namespace/name.
func (c *Client) GetClusterMapping(ctx context.Context, namespace, name string) (string, error) {
	docID := clusterMapDocID(namespace, name)
	snap, err := c.fsClient.Collection(collectionClusterMap).Doc(docID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return "", status.Errorf(codes.NotFound, "cluster mapping %s not found", docID)
		}
		return "", fmt.Errorf("get cluster mapping %s: %w", docID, err)
	}
	mc, ok := snap.Data()[fieldMgmtCluster].(string)
	if !ok {
		return "", fmt.Errorf("cluster mapping %s: missing managementCluster field", docID)
	}
	return mc, nil
}

// DeleteClusterMapping removes the cluster mapping for the given namespace/name.
func (c *Client) DeleteClusterMapping(ctx context.Context, namespace, name string) error {
	docID := clusterMapDocID(namespace, name)
	_, err := c.fsClient.Collection(collectionClusterMap).Doc(docID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("delete cluster mapping %s: %w", docID, err)
	}
	return nil
}

// ListClusterMappings returns all cluster mappings for the given namespace.
func (c *Client) ListClusterMappings(ctx context.Context, namespace string) ([]ClusterMapping, error) {
	// Query documents where the "namespace" field matches. This is more
	// reliable than document ID prefix queries.
	col := c.fsClient.Collection(collectionClusterMap)
	snaps, err := col.Where("namespace", "==", namespace).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("list cluster mappings for %s: %w", namespace, err)
	}
	result := make([]ClusterMapping, 0, len(snaps))
	for _, snap := range snaps {
		data := snap.Data()
		mc, _ := data[fieldMgmtCluster].(string)
		name, _ := data["name"].(string)
		result = append(result, ClusterMapping{
			Namespace:         namespace,
			Name:              name,
			ManagementCluster: mc,
		})
	}
	return result, nil
}

// toInt64 converts a Firestore numeric value to int64.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}

// Close closes the underlying Firestore client.
func (c *Client) Close() error {
	return c.fsClient.Close()
}
