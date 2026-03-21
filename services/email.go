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

	if resp.StatusCode != http.StatusOK {
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

	if resp.StatusCode != http.StatusOK {
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