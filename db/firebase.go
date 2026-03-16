package db

import (
	"context"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
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

func SaveWaitlistEntry(ctx context.Context, email, referralCode, referredByCode string) (bool, string, error) {
	docRef := Client.Collection("waitlist").Doc(email)
	
	doc, err := docRef.Get(ctx)
	if err == nil {
		var data map[string]interface{}
		doc.DataTo(&data)
		existingCode := data["referral_code"].(string)
		return true, existingCode, nil
	}

	if status.Code(err) != codes.NotFound {
		return false, "", err
	}

	newEntry := map[string]interface{}{
		"email":          email,
		"referral_code":  referralCode,
		"referred_by":    referredByCode,
		"referral_count": 0,
		"created_at":     time.Now(),
	}

	_, err = docRef.Set(ctx, newEntry)
	if err != nil {
		return false, "", err
	}

	if referredByCode != "" && referredByCode != referralCode {
		iter := Client.Collection("waitlist").Where("referral_code", "==", referredByCode).Limit(1).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("Error finding referrer: %v", err)
				break
			}
			
			if doc.Data()["email"] != email {
				doc.Ref.Update(ctx, []firestore.Update{
					{Path: "referral_count", Value: firestore.Increment(1)},
				})
			}
			break
		}
	}

	return false, referralCode, nil
}

func HealthCheck(ctx context.Context) error {
	return Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		return nil
	})
}
