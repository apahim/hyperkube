package cedar

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var testDBCounter atomic.Int64

func newTestFirestoreClient(t *testing.T) *firestore.Client {
	t.Helper()
	emulator := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulator == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set, skipping Firestore test")
	}
	n := testDBCounter.Add(1)
	dbID := fmt.Sprintf("cedar-test-%d", n)
	ctx := context.Background()
	client, err := firestore.NewClientWithDatabase(ctx, "test-project", dbID,
		option.WithEndpoint(emulator),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		t.Fatalf("creating test Firestore client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	client := newTestFirestoreClient(t)
	store, err := NewStore(client)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func seedCustomRole(t *testing.T, store *Store, projectID, roleName string, permissions []string, conditions []string) {
	t.Helper()
	docID := customRoleDocID(projectID, roleName)
	doc := customRoleDoc{
		Permissions: permissions,
		Conditions:  conditions,
	}
	_, err := store.fsClient.Collection(collectionCustomRoles).Doc(docID).Set(context.Background(), doc)
	if err != nil {
		t.Fatalf("seeding custom role %s: %v", docID, err)
	}
}

func seedGlobalPolicies(t *testing.T, store *Store, policyText string) {
	t.Helper()
	_, err := store.fsClient.Collection(collectionGlobalPolicies).Doc(globalPoliciesDocID).Set(
		context.Background(),
		map[string]any{globalPoliciesKey: policyText},
	)
	if err != nil {
		t.Fatalf("seeding global policies: %v", err)
	}
}
