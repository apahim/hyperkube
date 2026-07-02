package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	userID := flag.String("user-id", "", "Google identity subject (sub) to grant full Cedar access")
	flag.Parse()

	emulator := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulator == "" {
		log.Fatal("FIRESTORE_EMULATOR_HOST must be set")
	}

	ctx := context.Background()
	opts := []option.ClientOption{
		option.WithEndpoint(emulator),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	}

	placementClient, err := firestore.NewClientWithDatabase(ctx, "test-project", "placement", opts...)
	if err != nil {
		log.Fatalf("creating placement client: %v", err)
	}
	defer placementClient.Close()

	// Clear stale cluster mappings from any previous run.
	iter := placementClient.Collection("clustermap").Documents(ctx)
	for {
		snap, err := iter.Next()
		if err != nil {
			break
		}
		if _, err := snap.Ref.Delete(ctx); err != nil {
			log.Fatalf("deleting clustermap %s: %v", snap.Ref.ID, err)
		}
		fmt.Printf("placement/clustermap/%s: deleted\n", snap.Ref.ID)
	}

	// Clear stale desires from per-MC specs and status databases.
	for _, mc := range []string{"mc-01", "mc-02"} {
		for _, suffix := range []string{"specs", "status"} {
			dbID := fmt.Sprintf("mc-%s-%s", mc, suffix)
			c, err := firestore.NewClientWithDatabase(ctx, "test-project", dbID, opts...)
			if err != nil {
				log.Fatalf("creating %s client: %v", dbID, err)
			}
			for _, col := range []string{"applydesires", "deletedesires", "readdesires"} {
				dIter := c.Collection(col).Documents(ctx)
				for {
					snap, err := dIter.Next()
					if err != nil {
						break
					}
					if _, err := snap.Ref.Delete(ctx); err != nil {
						log.Fatalf("deleting %s/%s/%s: %v", dbID, col, snap.Ref.ID, err)
					}
					fmt.Printf("%s/%s/%s: deleted\n", dbID, col, snap.Ref.ID)
				}
			}
			c.Close()
		}
	}

	for _, mc := range []string{"mc-01", "mc-02"} {
		_, err := placementClient.Collection("managementclusters").Doc(mc).Set(ctx, map[string]any{
			"capacity":  int64(10),
			"allocated": int64(0),
		})
		if err != nil {
			log.Fatalf("seeding placement %s: %v", mc, err)
		}
		fmt.Printf("placement/managementclusters/%s: capacity=10, allocated=0\n", mc)
	}

	if *userID == "" {
		fmt.Println("\nPlacement seeded. Skipping Cedar (no --user-id provided).")
		return
	}

	cedarClient, err := firestore.NewClientWithDatabase(ctx, "test-project", "cedar", opts...)
	if err != nil {
		log.Fatalf("creating cedar client: %v", err)
	}
	defer cedarClient.Close()

	policy := fmt.Sprintf("permit (\n    principal == HCP::User::\"%s\",\n    action,\n    resource\n);", *userID)
	_, err = cedarClient.Collection("global-policies").Doc("default").Set(ctx, map[string]any{
		"policies.cedar": policy,
	})
	if err != nil {
		log.Fatalf("seeding cedar policy: %v", err)
	}
	fmt.Printf("\ncedar/global-policies/default: policy for user %s\n", *userID)
	fmt.Println("\nDone.")
}
