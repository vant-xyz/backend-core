package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/vant-xyz/backend-code/models"
)

type VASClient struct {
	baseURL string
	timeout time.Duration
}

func NewVASClient() *VASClient {
	baseURL := os.Getenv("VANT_AUXILIARY_SERVICE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:3000" // fallback for local dev
	}
	return &VASClient{
		baseURL: baseURL,
		timeout: 30 * time.Second,
	}
}

type SendWaitlistEmailRequest struct {
	To           string `json:"to"`
	ReferralCode string `json:"referralCode"`
}

type SendTransactionEmailRequest struct {
	To        string  `json:"to"`
	TxID      string  `json:"txId"`
	TxType    string  `json:"txType"`
	Amount    float64 `json:"amount"`
	FeeAmount float64 `json:"feeAmount,omitempty"`
	FeeRate   float64 `json:"feeRate,omitempty"`
	FeeChain  string  `json:"feeChain,omitempty"`
	FeeWallet string  `json:"feeWallet,omitempty"`
	NetAmount float64 `json:"netAmount,omitempty"`
	Currency  string  `json:"currency"`
	Nature    string  `json:"nature"`
	Status    string  `json:"status"`
	TxHash    string  `json:"txHash,omitempty"`
	CreatedAt string  `json:"createdAt,omitempty"`
}

type EmailResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type SendAdminReminderRequest struct {
	MarketID    string `json:"marketId"`
	MarketTitle string `json:"marketTitle"`
}

type SendMarketResolvedBatchRequest struct {
	Market struct {
		Title       string `json:"title"`
		BannerImage string `json:"bannerImage"`
		AvatarImage string `json:"avatarImage"`
		Outcome     string `json:"outcome"`
	} `json:"market"`
	Participants []MarketResolvedParticipant `json:"participants"`
}

type MarketResolvedParticipant struct {
	Email      string  `json:"email"`
	Won        bool    `json:"won"`
	Stake      float64 `json:"stake"`
	Payout     float64 `json:"payout"`
	PnL        float64 `json:"pnl"`
	Multiplier float64 `json:"multiplier"`
}

func SendAdminReminderEmail(marketID, marketTitle string) error {
	reqBody := SendAdminReminderRequest{
		MarketID:    marketID,
		MarketTitle: marketTitle,
	}
	return callProtectedVASEndpoint("/email/admin-reminder", reqBody)
}

func SendMarketResolvedBatchEmail(req SendMarketResolvedBatchRequest) error {
	return callProtectedVASEndpoint("/email/market-resolved-batch", req)
}

type SendRebalanceNotificationRequest struct {
	Asset  string  `json:"asset"`
	Amount float64 `json:"amount"`
	TxHash string  `json:"txHash"`
}

func SendRebalanceEmail(details SendRebalanceNotificationRequest) error {
	reqBody := struct {
		To string `json:"to"`
		SendRebalanceNotificationRequest
	}{
		To: "team@vantic.xyz",
		SendRebalanceNotificationRequest: details,
	}
	return callProtectedVASEndpoint("/email/rebalance-alert", reqBody)
}

func callProtectedVASEndpoint(path string, body interface{}) error {
	baseURL := os.Getenv("VANT_AUXILIARY_SERVICE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	adminKey := os.Getenv("ADMIN_API_KEY")

	url := fmt.Sprintf("%s%s", baseURL, path)
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", adminKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result EmailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("VAS error: %s", result.Message)
	}
	return nil
}

func SendWaitlistEmail(toEmail, referralCode string) error {
	log.Printf("[VAS] Sending waitlist email to %s", toEmail)

	reqBody := SendWaitlistEmailRequest{
		To:           toEmail,
		ReferralCode: referralCode,
	}

	url := fmt.Sprintf("%s/email/waitlist", os.Getenv("VANT_AUXILIARY_SERVICE_URL"))
	if url == "/email/waitlist" {
		url = "http://localhost:3000/email/waitlist"
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("[VAS] failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBodyJSON))
	if err != nil {
		return fmt.Errorf("[VAS] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("[VAS] failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("[VAS] unexpected status code: %d", resp.StatusCode)
	}

	var result EmailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("[VAS] failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("[VAS] email service returned failure: %s", result.Message)
	}

	log.Printf("[VAS] Successfully sent waitlist email to %s", toEmail)
	return nil
}

func SendTransactionEmail(toEmail string, tx models.Transaction) error {
	log.Printf("[VAS] Sending %s transaction email to %s (txID: %s)", tx.Type, toEmail, tx.ID)

	reqBody := SendTransactionEmailRequest{
		To:        toEmail,
		TxID:      tx.ID,
		TxType:    tx.Type,
		Amount:    tx.Amount,
		FeeAmount: tx.FeeAmount,
		FeeRate:   tx.FeeRate,
		FeeChain:  tx.FeeChain,
		FeeWallet: tx.FeeWallet,
		NetAmount: tx.Amount,
		Currency:  tx.Currency,
		Nature:    tx.Nature,
		Status:    tx.Status,
		TxHash:    tx.TxHash,
		CreatedAt: tx.CreatedAt.Format(time.RFC3339),
	}

	url := fmt.Sprintf("%s/email/transaction", os.Getenv("VANT_AUXILIARY_SERVICE_URL"))
	if url == "/email/transaction" {
		url = "http://localhost:3000/email/transaction"
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("[VAS] failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBodyJSON))
	if err != nil {
		return fmt.Errorf("[VAS] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("[VAS] failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("[VAS] unexpected status code: %d", resp.StatusCode)
	}

	var result EmailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("[VAS] failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("[VAS] email service returned failure: %s", result.Message)
	}

	log.Printf("[VAS] Successfully sent transaction email to %s", toEmail)
	return nil
}
