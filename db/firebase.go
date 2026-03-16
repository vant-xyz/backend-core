package db

import (
	"context"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var Client *firestore.Client

func Init(projectID string, credentialsPath string) {
	ctx := context.Background()
	sa := option.WithServiceAccountFile(credentialsPath)
	
	client, err := firestore.NewClient(ctx, projectID, sa)
	if err != nil {
		log.Fatalf("Failed to create firestore client: %v", err)
	}

	Client = client
}

func SaveWaitlistEntry(ctx context.Context, email, referralCode, referredBy string) (bool, error) {
	docRef := Client.Collection("waitlist").Doc(email)
	
	_, err := docRef.Create(ctx, map[string]interface{}{
		"email":         email,
		"referral_code": referralCode,
		"referred_by":   referredBy,
		"created_at":    time.Now(),
	})

	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return true, nil // Already exists
		}
		return false, err
	}

	return false, nil // New entry created
}

func HealthCheck(ctx context.Context) error {
	return Client.RunTransaction(ctx, func(ctx context.Background(), tx *firestore.Transaction) error {
		return nil
	})
}
