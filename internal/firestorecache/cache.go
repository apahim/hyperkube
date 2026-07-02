package firestorecache

import (
	"context"
	"sync"

	"cloud.google.com/go/firestore"
)

// Cache is a thread-safe cache of Firestore clients keyed by database ID.
// Per-MC clients are created lazily on first request and reused thereafter.
type Cache struct {
	project string
	mu      sync.Mutex
	clients map[string]*firestore.Client
}

// NewCache creates a cache that will create Firestore clients in the given
// GCP project.
func NewCache(project string) *Cache {
	return &Cache{
		project: project,
		clients: make(map[string]*firestore.Client),
	}
}

// GetOrCreate returns a cached Firestore client for the given database ID,
// creating one if it doesn't exist yet.
func (c *Cache) GetOrCreate(ctx context.Context, databaseID string) (*firestore.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if client, ok := c.clients[databaseID]; ok {
		return client, nil
	}

	client, err := firestore.NewClientWithDatabase(ctx, c.project, databaseID)
	if err != nil {
		return nil, err
	}
	c.clients[databaseID] = client
	return client, nil
}

// Close closes all cached Firestore clients.
func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for id, client := range c.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(c.clients, id)
	}
	return firstErr
}
