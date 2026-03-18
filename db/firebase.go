package db

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
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

func GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	doc, err := Client.Collection("users").Doc(email).Get(ctx)
	if err != nil {
		return nil, err
	}
	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

func CheckUsernameExists(ctx context.Context, username string) (bool, error) {
	iter := Client.Collection("users").Where("username", "==", username).Limit(1).Documents(ctx)
	_, err := iter.Next()
	if err == iterator.Done {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func CreateUser(ctx context.Context, email, hashedPassword string) (*models.User, error) {
	wallet, err := services.GenerateWallet(email)
	if err != nil {
		return nil, err
	}

	balanceID := fmt.Sprintf("BAL_%s", utils.RandomAlphanumeric(10))
	vantID := fmt.Sprintf("VANTID_%s", utils.RandomNumbers(8))
	username := fmt.Sprintf("@user%s", utils.RandomAlphanumeric(6))

	user := models.User{
		Email:           email,
		Username:        username,
		Password:        hashedPassword,
		VantID:          vantID,
		BalanceID:       balanceID,
		Socials:         []string{},
		ProfileImageURL: "",
		CreatedAt:       time.Now(),
	}

	balance := models.Balance{
		ID:    balanceID,
		Email: email,
	}

	_, err = Client.Collection("users").Doc(email).Set(ctx, user)
	if err != nil {
		return nil, err
	}

	_, err = Client.Collection("balances").Doc(balanceID).Set(ctx, balance)
	if err != nil {
		return nil, err
	}

	_, err = Client.Collection("wallets").Doc(email).Set(ctx, wallet)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func UpdateUser(ctx context.Context, email string, updates map[string]interface{}) error {
	if username, ok := updates["username"].(string); ok {
		exists, err := CheckUsernameExists(ctx, username)
		if err != nil {
			return err
		}
		if exists {
			doc, _ := Client.Collection("users").Doc(email).Get(ctx)
			var user models.User
			doc.DataTo(&user)
			if user.Username != username {
				return fmt.Errorf("username already taken")
			}
		}
	}

	var firestoreUpdates []firestore.Update
	for k, v := range updates {
		firestoreUpdates = append(firestoreUpdates, firestore.Update{Path: k, Value: v})
	}

	_, err := Client.Collection("users").Doc(email).Update(ctx, firestoreUpdates)
	return err
}

func UpdateUsername(ctx context.Context, email, username string) error {
	exists, err := CheckUsernameExists(ctx, username)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("username already taken")
	}

	_, err = Client.Collection("users").Doc(email).Update(ctx, []firestore.Update{
		{Path: "username", Value: username},
	})
	return err
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

	return false, referralCode, nil
}

func TrackReferral(referredByCode, newUserEmail string) {
	if referredByCode == "" {
		return
	}

	searchCode := strings.ToUpper(referredByCode)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	iter := Client.Collection("waitlist").Where("referral_code", "==", searchCode).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err == iterator.Done {
		return
	}
	if err != nil {
		log.Printf("Error finding referrer: %v", err)
		return
	}

	if doc.Data()["email"] != newUserEmail {
		_, err := doc.Ref.Update(ctx, []firestore.Update{
			{Path: "referral_count", Value: firestore.Increment(1)},
		})
		if err != nil {
			log.Printf("Error updating referral count: %v", err)
		}
	}
}

func HealthCheck(ctx context.Context) error {
	return Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		return nil
	})
}

func GetBalanceByEmail(ctx context.Context, email string) (*models.Balance, error) {
	iter := Client.Collection("balances").Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err != nil {
		return nil, err
	}
	var balance models.Balance
	if err := doc.DataTo(&balance); err != nil {
		return nil, err
	}
	return &balance, nil
}

func GetWalletByEmail(ctx context.Context, email string) (*models.Wallet, error) {
	doc, err := Client.Collection("wallets").Doc(email).Get(ctx)
	if err != nil {
		return nil, err
	}
	var wallet models.Wallet
	if err := doc.DataTo(&wallet); err != nil {
		return nil, err
	}
	return &wallet, nil
}

func SaveTransaction(ctx context.Context, tx models.Transaction) error {
	_, err := Client.Collection("transactions").Doc(tx.ID).Set(ctx, tx)
	return err
}

func UpdateBalance(ctx context.Context, email string, field string, amount float64) error {
	iter := Client.Collection("balances").Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err != nil {
		return err
	}
	
	// Direct update using the discovered document reference
	_, err = doc.Ref.Update(ctx, []firestore.Update{
		{Path: field, Value: firestore.Increment(amount)},
	})
	return err
}

func SetBalance(ctx context.Context, email string, field string, amount float64) error {
	iter := Client.Collection("balances").Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err != nil {
		return err
	}
	_, err = doc.Ref.Update(ctx, []firestore.Update{
		{Path: field, Value: amount},
	})
	return err
}
