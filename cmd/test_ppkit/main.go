package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	ppkit "github.com/DavidNzube101/magicblock-pp-kit-go"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type apiReq struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Mint        string `json:"mint"`
	Amount      uint64 `json:"amount"`
	FromBalance string `json:"fromBalance"`
	ToBalance   string `json:"toBalance"`
	Visibility  string `json:"visibility"`
	Cluster     string `json:"cluster,omitempty"`
	MinDelayMs  string `json:"minDelayMs"`
	MaxDelayMs  string `json:"maxDelayMs"`
	Split       int    `json:"split"`
}

type apiResp struct {
	Transaction          string   `json:"transactionBase64"`
	RequiredSigners      []string `json:"requiredSigners"`
	SendTo               string   `json:"sendTo"`
	RecentBlockhash      string   `json:"recentBlockhash"`
	LastValidBlockHeight uint64   `json:"lastValidBlockHeight"`
}

func main() {
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")

	to := flag.String("to", "", "Recipient Solana address (required)")
	amount := flag.Float64("amount", 0.01, "Amount in USDC")
	cluster := flag.String("cluster", "devnet", "devnet or mainnet")
	flag.Parse()

	if *to == "" {
		log.Fatal("--to is required")
	}

	raw := os.Getenv("VANT_MARKET_APPROVED_SETLLER_KEYPAIR")
	if raw == "" {
		log.Fatal("VANT_MARKET_APPROVED_SETLLER_KEYPAIR not set")
	}
	var keyBytes []byte
	if err := json.Unmarshal([]byte(raw), &keyBytes); err != nil {
		log.Fatalf("keypair parse: %v", err)
	}
	keypair := solana.PrivateKey(keyBytes)

	var mint string
	if *cluster == "mainnet" {
		mint = os.Getenv("MAINNET_SOL_USDC_MINT")
		if mint == "" {
			mint = ppkit.MintUSDCMainnet
		}
	} else {
		mint = os.Getenv("DEVNET_SOL_USDC_MINT")
		if mint == "" {
			mint = ppkit.MintUSDCDevnet
		}
	}

	units := uint64(*amount * 1_000_000)
	fmt.Printf("payer   : %s\n", keypair.PublicKey())
	fmt.Printf("to      : %s\n", *to)
	fmt.Printf("amount  : %.6f USDC (%d micro-units)\n", *amount, units)
	fmt.Printf("cluster : %s\n", *cluster)
	fmt.Printf("mint    : %s\n\n", mint)

	// Step 1: call MagicBlock payments API
	fmt.Println("[1] Calling MagicBlock payments API...")
	body, _ := json.Marshal(apiReq{
		From:        keypair.PublicKey().String(),
		To:          *to,
		Mint:        mint,
		Amount:      units,
		FromBalance: "base",
		ToBalance:   "base",
		Visibility:  "private",
		Cluster:     *cluster,
		MinDelayMs:  "0",
		MaxDelayMs:  "0",
		Split:       1,
	})
	httpClient := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := httpClient.Post("https://payments.magicblock.app/v1/spl/transfer", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("    API request failed: %v", err)
	}
	defer httpResp.Body.Close()
	rawBody, _ := io.ReadAll(httpResp.Body)
	fmt.Printf("    status: %d\n", httpResp.StatusCode)
	if httpResp.StatusCode != 200 {
		log.Fatalf("    API error: %s", string(rawBody))
	}
	var resp apiResp
	if err := json.Unmarshal(rawBody, &resp); err != nil {
		log.Fatalf("    decode response: %v\nbody: %s", err, string(rawBody))
	}
	fmt.Printf("    sendTo              : %s\n", resp.SendTo)
	fmt.Printf("    recentBlockhash     : %s\n", resp.RecentBlockhash)
	fmt.Printf("    lastValidBlockHeight: %d\n", resp.LastValidBlockHeight)
	fmt.Printf("    requiredSigners     : %v\n", resp.RequiredSigners)

	// Step 2: decode + sign
	fmt.Println("\n[2] Decoding and signing...")
	txBytes, err := base64.StdEncoding.DecodeString(resp.Transaction)
	if err != nil {
		log.Fatalf("    base64 decode: %v", err)
	}
	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		log.Fatalf("    parse tx: %v", err)
	}
	fmt.Printf("    instructions: %d\n", len(tx.Message.Instructions))
	keyMap := map[solana.PublicKey]*solana.PrivateKey{keypair.PublicKey(): &keypair}
	if _, err = tx.Sign(func(k solana.PublicKey) *solana.PrivateKey { return keyMap[k] }); err != nil {
		log.Fatalf("    sign: %v", err)
	}
	fmt.Println("    signed OK")

	// Step 3: submit with verbose error logging
	fmt.Println("\n[3] Submitting to Solana RPC...")
	rpcURLs := rpcURLsForCluster(*cluster)
	if resp.SendTo == "ephemeral" {
		u := os.Getenv("MAGICBLOCK_EPHEMERAL_RPC_URL")
		if u == "" {
			u = "https://devnet-eu.magicblock.app"
		}
		rpcURLs = []string{u}
	}
	fmt.Printf("    endpoints: %v\n", rpcURLs)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, endpoint := range rpcURLs {
		fmt.Printf("    trying: %s\n", endpoint)
		sig, sendErr := rpc.New(endpoint).SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{SkipPreflight: true})
		if sendErr != nil {
			fmt.Printf("    FAILED: %v\n", sendErr)
			continue
		}
		fmt.Printf("\n[SUCCESS] sig=%s\n", sig)
		return
	}

	log.Fatal("all RPC endpoints failed")
}

func rpcURLsForCluster(cluster string) []string {
	if cluster == "mainnet" {
		urls := []string{}
		if u := os.Getenv("MAINNET_SOLANA_RPC_URL"); u != "" {
			urls = append(urls, u)
		}
		return append(urls, "https://api.mainnet-beta.solana.com")
	}
	var urls []string
	for _, env := range []string{"DEVNET_SOLANA_RPC_URL", "DEVNET_SOLANA_RPC_URL_1", "DEVNET_SOLANA_RPC_URL_2"} {
		if u := os.Getenv(env); u != "" {
			urls = append(urls, u)
		}
	}
	return append(urls, "https://api.devnet.solana.com")
}
