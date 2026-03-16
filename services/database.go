package services

import (
	"context"
	"log"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
)

var FirestoreClient *firestore.Client

func InitDB() {
	ctx := context.Background()
	sa := option.WithServiceAccountFile("serviceAccount.json")
	
	client, err := firestore.NewClient(ctx, "vant-a2479", sa)
	if err != nil {
		log.Fatalf("Failed to create firestore client: %v", err)
	}

	FirestoreClient = client
}
