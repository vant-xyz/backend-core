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

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/joho/godotenv"
)

// Mirrors the backend structs exactly.

type payReq struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Mint        string `json:"mint"`
	Amount      uint64 `json:"amount"`
	FromBalance string `json:"fromBalance"`
	ToBalance   string `json:"toBalance"`
	Visibility  string `json:"visibility"`
	Cluster     string `json:"cluster,omitempty"`
}

type payResp struct {
	Transaction          string   `json:"transactionBase64"`
	RequiredSigners      []string `json:"requiredSigners"`
	SendTo               string   `json:"sendTo"`
	RecentBlockhash      string   `json:"recentBlockhash"`
	LastValidBlockHeight uint64   `json:"lastValidBlockHeight"`
}

func step(n int, msg string, args ...any) {
	fmt.Printf("\n[STEP %d] "+msg+"\n", append([]any{n}, args...)...)
}

func main() {
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")

	to := flag.String("to", "", "Recipient Solana address (required)")
	amount := flag.Float64("amount", 0.01, "Amount in USDC (0.01 = 10000 micro-units)")
	dry := flag.Bool("dry", false, "Stop after API response — don't sign or send")
	cluster := flag.String("cluster", "", "Override cluster (default: reads SOLANA_CLUSTER env or 'devnet')")
	mint := flag.String("mint", "", "Override USDC mint (default: reads DEVNET_SOL_USDC_MINT env)")
	fromBalance := flag.String("from-balance", "base", "fromBalance field value (e.g. base, ephemeral)")
	toBalance := flag.String("to-balance", "base", "toBalance field value (e.g. base, ephemeral)")
	flag.Parse()

	if *to == "" {
		log.Fatal("--to is required. Example: --to 7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	}

	// ── Step 1: Load settler keypair ─────────────────────────────────────────
	step(1, "Loading settler keypair from VANT_MARKET_APPROVED_SETLLER_KEYPAIR")
	raw := os.Getenv("VANT_MARKET_APPROVED_SETLLER_KEYPAIR")
	if raw == "" {
		log.Fatal("VANT_MARKET_APPROVED_SETLLER_KEYPAIR env var not set")
	}
	var keyBytes []byte
	if err := json.Unmarshal([]byte(raw), &keyBytes); err != nil {
		log.Fatalf("Failed to parse keypair JSON: %v", err)
	}
	keypair := solana.PrivateKey(keyBytes)
	fmt.Printf("  pubkey : %s\n", keypair.PublicKey())

	// ── Step 2: Resolve config ────────────────────────────────────────────────
	step(2, "Resolving config")

	resolvedCluster := *cluster
	if resolvedCluster == "" {
		resolvedCluster = os.Getenv("SOLANA_CLUSTER")
	}
	if resolvedCluster == "" {
		resolvedCluster = "devnet"
	}

	resolvedMint := *mint
	if resolvedMint == "" {
		resolvedMint = os.Getenv("DEVNET_SOL_USDC_MINT")
	}
	if resolvedMint == "" {
		resolvedMint = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	}

	units := uint64(*amount * 1_000_000)
	fmt.Printf("  cluster     : %s\n", resolvedCluster)
	fmt.Printf("  mint        : %s\n", resolvedMint)
	fmt.Printf("  amount      : %.6f USDC (%d micro-units)\n", *amount, units)
	fmt.Printf("  fromBalance : %s\n", *fromBalance)
	fmt.Printf("  toBalance   : %s\n", *toBalance)
	fmt.Printf("  visibility  : private\n")

	// ── Step 3: Build and send API request ────────────────────────────────────
	step(3, "Calling MagicBlock payments API")

	reqBody := payReq{
		From:        keypair.PublicKey().String(),
		To:          *to,
		Mint:        resolvedMint,
		Amount:      units,
		FromBalance: *fromBalance,
		ToBalance:   *toBalance,
		Visibility:  "private",
		Cluster:     resolvedCluster,
	}

	bodyJSON, _ := json.MarshalIndent(reqBody, "  ", "  ")
	endpoint := "https://payments.magicblock.app/v1/spl/transfer"
	fmt.Printf("  POST %s\n", endpoint)
	fmt.Printf("  body:\n  %s\n", string(bodyJSON))

	httpClient := &http.Client{Timeout: 15 * time.Second}
	httpResp, err := httpClient.Post(endpoint, "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		log.Fatalf("  HTTP request failed: %v", err)
	}
	defer httpResp.Body.Close()

	rawBody, _ := io.ReadAll(httpResp.Body)
	fmt.Printf("  status : %d\n", httpResp.StatusCode)
	fmt.Printf("  body   : %s\n", string(rawBody))

	if httpResp.StatusCode != http.StatusOK {
		log.Fatalf("  API returned non-200. Stopping.")
	}

	var apiResp payResp
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		log.Fatalf("  Failed to decode response: %v", err)
	}

	fmt.Printf("  sendTo               : %q\n", apiResp.SendTo)
	fmt.Printf("  requiredSigners      : %v\n", apiResp.RequiredSigners)
	fmt.Printf("  recentBlockhash      : %s\n", apiResp.RecentBlockhash)
	fmt.Printf("  lastValidBlockHeight : %d\n", apiResp.LastValidBlockHeight)
	fmt.Printf("  transactionBase64 len: %d chars\n", len(apiResp.Transaction))

	if *dry {
		fmt.Println("\n[DRY RUN] API call succeeded. Stopping before sign/send.")
		return
	}

	// ── Step 4: Decode transaction ────────────────────────────────────────────
	step(4, "Decoding base64 transaction")
	txBytes, err := base64.StdEncoding.DecodeString(apiResp.Transaction)
	if err != nil {
		log.Fatalf("  base64 decode failed: %v", err)
	}
	fmt.Printf("  decoded %d bytes\n", len(txBytes))

	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		log.Fatalf("  TransactionFromBytes failed: %v", err)
	}
	fmt.Printf("  instructions : %d\n", len(tx.Message.Instructions))
	fmt.Printf("  accounts     : %d\n", len(tx.Message.AccountKeys))
	for i, acc := range tx.Message.AccountKeys {
		fmt.Printf("    [%d] %s\n", i, acc)
	}

	// ── Step 5: Sign ──────────────────────────────────────────────────────────
	step(5, "Signing transaction with settler keypair")
	keyMap := map[solana.PublicKey]*solana.PrivateKey{
		keypair.PublicKey(): &keypair,
	}
	sigs, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey { return keyMap[key] })
	if err != nil {
		log.Fatalf("  Sign failed: %v", err)
	}
	fmt.Printf("  signed %d signature slot(s)\n", len(sigs))

	// ── Step 6: Determine RPC ─────────────────────────────────────────────────
	step(6, "Determining RPC endpoint (sendTo=%q)", apiResp.SendTo)
	var rpcURLs []string
	if apiResp.SendTo == "ephemeral" {
		ephemeralURL := os.Getenv("MAGICBLOCK_EPHEMERAL_RPC_URL")
		if ephemeralURL == "" {
			ephemeralURL = "https://devnet-eu.magicblock.app"
		}
		rpcURLs = []string{ephemeralURL}
		fmt.Printf("  Using ephemeral RPC: %s\n", ephemeralURL)
	} else {
		// sendTo == "base" → submit to standard Solana RPC
		rpcURLs = []string{"https://api.devnet.solana.com"}
		if u := os.Getenv("DEVNET_SOLANA_RPC_URL"); u != "" {
			rpcURLs = append([]string{u}, rpcURLs...)
		}
		fmt.Printf("  Using base RPCs: %v\n", rpcURLs)
	}

	// ── Step 7: Send (skip preflight to avoid blockhash drift) ───────────────
	step(7, "Sending transaction (SkipPreflight=true)")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, rpcURL := range rpcURLs {
		fmt.Printf("  Trying: %s\n", rpcURL)
		rpcClient := rpc.New(rpcURL)
		sig, sendErr := rpcClient.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
			SkipPreflight: true,
		})
		if sendErr != nil {
			fmt.Printf("  Send failed: %v\n", sendErr)
			continue
		}
		fmt.Printf("  Submitted sig=%s\n  Polling for confirmation...\n", sig)

		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			statuses, pollErr := rpcClient.GetSignatureStatuses(ctx, true, sig)
			if pollErr != nil {
				fmt.Printf("  poll error: %v\n", pollErr)
				continue
			}
			if len(statuses.Value) == 0 || statuses.Value[0] == nil {
				continue
			}
			s := statuses.Value[0]
			if s.Err != nil {
				log.Fatalf("\n[FAIL] Transaction failed on-chain: %v", s.Err)
			}
			if s.ConfirmationStatus == rpc.ConfirmationStatusConfirmed || s.ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				fmt.Printf("\n[SUCCESS] sig=%s  status=%s\n", sig, s.ConfirmationStatus)
				return
			}
			fmt.Printf("  status=%s slot=%d (attempt %d/30)\n", s.ConfirmationStatus, s.Slot, i+1)
		}
		fmt.Printf("  Confirmation timed out for %s\n", rpcURL)
	}

	fmt.Println("\n[FAIL] All RPC endpoints failed.")
	os.Exit(1)
}
