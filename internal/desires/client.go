package desires

import (
	"cloud.google.com/go/firestore"
)

// DBClient wraps a single Firestore named database and provides typed CRUD
// access to the three desire collections. The API server creates one DBClient
// per database (e.g. mc-01-specs, mc-01-status).
type DBClient struct {
	fsClient *firestore.Client
}

// NewDBClient creates a DBClient wrapping the given Firestore client.
func NewDBClient(fsClient *firestore.Client) *DBClient {
	return &DBClient{fsClient: fsClient}
}

func (c *DBClient) ApplyDesires() ResourceCRUD[ApplyDesire] {
	return &firestoreCRUD[ApplyDesire, *ApplyDesire]{
		client:     c.fsClient,
		collection: CollectionApplyDesires,
	}
}

func (c *DBClient) DeleteDesires() ResourceCRUD[DeleteDesire] {
	return &firestoreCRUD[DeleteDesire, *DeleteDesire]{
		client:     c.fsClient,
		collection: CollectionDeleteDesires,
	}
}

func (c *DBClient) ReadDesires() ResourceCRUD[ReadDesire] {
	return &firestoreCRUD[ReadDesire, *ReadDesire]{
		client:     c.fsClient,
		collection: CollectionReadDesires,
	}
}

func (c *DBClient) Close() error {
	return c.fsClient.Close()
}
