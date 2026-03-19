package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type WhitelistRequest struct {
	Email         string `json:"email"`
	SolPublicKey  string `json:"sol_public_key"`
	BasePublicKey string `json:"base_public_key"`
}

func NotifyIndexerWhitelist(email, solKey, baseKey string) error {
	indexerURL := os.Getenv("INDEXER_URL")
	if indexerURL == "" {
		indexerURL = "http://localhost:3001"
	}

	reqBody := WhitelistRequest{
		Email:         email,
		SolPublicKey:  solKey,
		BasePublicKey: baseKey,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := http.Post(indexerURL+"/whitelist", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indexer returned non-OK status: %d", resp.StatusCode)
	}

	return nil
}
