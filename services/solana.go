package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

const rpcTimeout = 10 * time.Second
const defaultWSOLMint = "So11111111111111111111111111111111111111112"

func GetSolBalance(pubKey string) (float64, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	client := rpc.New(rpcURL)

	account, err := solana.PublicKeyFromBase58(pubKey)
	if err != nil {
		return 0, fmt.Errorf("invalid pubkey %s: %w", pubKey, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()

	res, err := client.GetBalance(ctx, account, rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("GetBalance RPC failed for %s: %w", pubKey, err)
	}

	return float64(res.Value) / 1e9, nil
}

// GetSPLBalance fetches the balance of an SPL token for a given wallet.
// Returns (0, nil) when the ATA doesn't exist yet — that's normal for new wallets.
// Returns (0, error) on real RPC failures.
func GetSPLBalance(walletPubKey, mintPubKey, rpcURL, tokenName string) (float64, error) {
	log.Printf("[SPL] Fetching %s balance for wallet %s", tokenName, walletPubKey)

	client := rpc.New(rpcURL)

	wallet, err := solana.PublicKeyFromBase58(walletPubKey)
	if err != nil {
		return 0, fmt.Errorf("[SPL] invalid wallet pubkey %s: %w", walletPubKey, err)
	}

	mint, err := solana.PublicKeyFromBase58(mintPubKey)
	if err != nil {
		return 0, fmt.Errorf("[SPL] invalid mint pubkey %s: %w", mintPubKey, err)
	}

	ata, _, err := solana.FindAssociatedTokenAddress(wallet, mint)
	if err != nil {
		return 0, fmt.Errorf("[SPL] failed to derive ATA for wallet %s mint %s: %w", walletPubKey, mintPubKey, err)
	}

	log.Printf("[SPL] %s ATA: %s", tokenName, ata.String())

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()

	res, err := client.GetTokenAccountBalance(ctx, ata, rpc.CommitmentFinalized)
	if err != nil {
		// ATA not existing yet is normal — the RPC returns an error like
		// "could not find account". Treat it as zero balance, not a failure.
		errStr := err.Error()
		if strings.Contains(errStr, "could not find account") ||
			strings.Contains(errStr, "AccountNotFound") ||
			strings.Contains(errStr, "Invalid param") {
			log.Printf("[SPL] %s ATA not found (wallet has no %s yet): %s", tokenName, tokenName, ata.String())
			return 0, nil
		}
		return 0, fmt.Errorf("[SPL] GetTokenAccountBalance RPC failed for %s (%s): %w", tokenName, ata.String(), err)
	}

	if res == nil || res.Value == nil {
		log.Printf("[SPL] %s: nil response from RPC", tokenName)
		return 0, nil
	}

	amount, err := strconv.ParseFloat(res.Value.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("[SPL] failed to parse %s amount %q: %w", tokenName, res.Value.Amount, err)
	}

	decimals := res.Value.Decimals
	if decimals == 0 {
		decimals = 6
	}

	divisor := 1.0
	for i := 0; i < int(decimals); i++ {
		divisor *= 10
	}

	balance := amount / divisor
	log.Printf("[SPL] Fetched %s balance: %f", tokenName, balance)
	return balance, nil
}

func GetAllSPLBalances(walletPubKey string) (usdc, usdt, usdg, pusd float64, err error) {
	log.Printf("[SPL] GetAllSPLBalances for wallet %s", walletPubKey)

	devnetRPC := os.Getenv("DEVNET_SOLANA_RPC_URL")
	mainnetRPC := os.Getenv("MAINNET_SOLANA_RPC_URL")

	usdcMint := os.Getenv("DEVNET_SOL_USDC_MINT")
	usdtMint := os.Getenv("MAINNET_SOL_USDT_MINT")
	usdgMint := os.Getenv("MAINNET_SOL_USDG_MINT")
	pusdMint := "CZzgUBvxaMLwMhVSLgqJn3npmxoTo6nzMNQPAnwtHF3s"

	var fetchErr error

	if usdcMint != "" && devnetRPC != "" {
		usdc, fetchErr = GetSPLBalance(walletPubKey, usdcMint, devnetRPC, "USDC")
		if fetchErr != nil {
			log.Printf("[SPL] USDC fetch error: %v", fetchErr)
			err = fetchErr
			usdc = 0
		}
	} else {
		log.Printf("[SPL] Skipping USDC: mint=%q devnetRPC=%q", usdcMint, devnetRPC)
	}

	if usdtMint != "" && mainnetRPC != "" {
		usdt, fetchErr = GetSPLBalance(walletPubKey, usdtMint, mainnetRPC, "USDT")
		if fetchErr != nil {
			log.Printf("[SPL] USDT fetch error: %v", fetchErr)
			if err == nil {
				err = fetchErr
			}
			usdt = 0
		}
	} else {
		log.Printf("[SPL] Skipping USDT: mint=%q mainnetRPC=%q", usdtMint, mainnetRPC)
	}

	if usdgMint != "" && mainnetRPC != "" {
		usdg, fetchErr = GetSPLBalance(walletPubKey, usdgMint, mainnetRPC, "USDG")
		if fetchErr != nil {
			log.Printf("[SPL] USDG fetch error: %v", fetchErr)
			if err == nil {
				err = fetchErr
			}
			usdg = 0
		}
	} else {
		log.Printf("[SPL] Skipping USDG: mint=%q mainnetRPC=%q", usdgMint, mainnetRPC)
	}

	if mainnetRPC != "" {
		pusd, fetchErr = GetSPLBalance(walletPubKey, pusdMint, mainnetRPC, "PUSD")
		if fetchErr != nil {
			log.Printf("[SPL] PUSD fetch error: %v", fetchErr)
			if err == nil {
				err = fetchErr
			}
			pusd = 0
		}
	} else {
		log.Printf("[SPL] Skipping PUSD: mainnetRPC not set")
	}

	log.Printf("[SPL] Final balances — USDC: %f, USDT: %f, USDG: %f, PUSD: %f", usdc, usdt, usdg, pusd)
	return
}

func GetWSOLBalances(walletPubKey string) (devnetWSOL, mainnetWSOL float64, err error) {
	devnetRPC := os.Getenv("DEVNET_SOLANA_RPC_URL")
	mainnetRPC := os.Getenv("MAINNET_SOLANA_RPC_URL")

	devnetMint := os.Getenv("DEVNET_SOL_WSOL_MINT")
	if devnetMint == "" {
		devnetMint = defaultWSOLMint
	}
	mainnetMint := os.Getenv("MAINNET_SOL_WSOL_MINT")
	if mainnetMint == "" {
		mainnetMint = defaultWSOLMint
	}

	var firstErr error
	if devnetRPC != "" {
		bal, fetchErr := GetSPLBalance(walletPubKey, devnetMint, devnetRPC, "WSOL_DEVNET")
		if fetchErr != nil {
			firstErr = fetchErr
		} else {
			devnetWSOL = bal
		}
	}

	if mainnetRPC != "" {
		bal, fetchErr := GetSPLBalance(walletPubKey, mainnetMint, mainnetRPC, "WSOL_MAINNET")
		if fetchErr != nil && firstErr == nil {
			firstErr = fetchErr
		} else if fetchErr == nil {
			mainnetWSOL = bal
		}
	}

	return devnetWSOL, mainnetWSOL, firstErr
}

func TransferSol(senderPrivateKey, recipientPublicKey string, amountSol float64) (string, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)

	client := rpc.New(rpcURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsClient, err := ws.Connect(ctx, wsURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer wsClient.Close()

	senderWallet, err := solana.WalletFromPrivateKeyBase58(senderPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create sender wallet from private key: %w", err)
	}

	feePayerSecret := os.Getenv("VANT_FEE_PAYER_SOLANA")
	feePayerWallet, err := solana.WalletFromPrivateKeyBase58(feePayerSecret)
	if err != nil {
		return "", fmt.Errorf("failed to create fee payer wallet: %w", err)
	}

	dest, err := solana.PublicKeyFromBase58(recipientPublicKey)
	if err != nil {
		return "", fmt.Errorf("invalid recipient pubkey %s: %w", recipientPublicKey, err)
	}

	lamports := uint64(amountSol * 1e9)

	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("GetLatestBlockhash failed: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			computebudget.NewSetComputeUnitPriceInstruction(100000).Build(),
			system.NewTransferInstruction(
				lamports,
				senderWallet.PublicKey(),
				dest,
			).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(feePayerWallet.PublicKey()),
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(senderWallet.PublicKey()) {
			return &senderWallet.PrivateKey
		}
		if key.Equals(feePayerWallet.PublicKey()) {
			return &feePayerWallet.PrivateKey
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	sig, err := confirm.SendAndConfirmTransaction(ctx, client, wsClient, tx)
	if err != nil {
		return "", fmt.Errorf("SendAndConfirmTransaction failed: %w", err)
	}

	return sig.String(), nil
}

func TransferSPLToken(senderPrivateKey, recipientPublicKey, mintPublicKey string, decimals uint8, amount float64, rpcURL string) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("amount must be positive")
	}
	if rpcURL == "" {
		return "", fmt.Errorf("solana rpc url is required")
	}
	wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)

	client := rpc.New(rpcURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsClient, err := ws.Connect(ctx, wsURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer wsClient.Close()

	senderWallet, err := solana.WalletFromPrivateKeyBase58(senderPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create sender wallet from private key: %w", err)
	}
	feePayerSecret := os.Getenv("VANT_FEE_PAYER_SOLANA")
	feePayerWallet, err := solana.WalletFromPrivateKeyBase58(feePayerSecret)
	if err != nil {
		return "", fmt.Errorf("failed to create fee payer wallet: %w", err)
	}
	dest, err := solana.PublicKeyFromBase58(recipientPublicKey)
	if err != nil {
		return "", fmt.Errorf("invalid recipient pubkey %s: %w", recipientPublicKey, err)
	}
	mint, err := solana.PublicKeyFromBase58(mintPublicKey)
	if err != nil {
		return "", fmt.Errorf("invalid mint pubkey %s: %w", mintPublicKey, err)
	}

	senderATA, _, err := solana.FindAssociatedTokenAddress(senderWallet.PublicKey(), mint)
	if err != nil {
		return "", fmt.Errorf("failed to derive sender ATA: %w", err)
	}
	destATA, _, err := solana.FindAssociatedTokenAddress(dest, mint)
	if err != nil {
		return "", fmt.Errorf("failed to derive destination ATA: %w", err)
	}

	instructions := []solana.Instruction{
		computebudget.NewSetComputeUnitPriceInstruction(100000).Build(),
	}

	// Ensure destination ATA exists.
	{
		checkCtx, checkCancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer checkCancel()
		_, err = client.GetAccountInfo(checkCtx, destATA)
		if err != nil {
			instructions = append(instructions, associatedtokenaccount.NewCreateInstruction(
				feePayerWallet.PublicKey(),
				dest,
				mint,
			).Build())
		}
	}

	baseUnits := uint64(math.Round(amount * math.Pow10(int(decimals))))
	instructions = append(instructions, token.NewTransferCheckedInstruction(
		baseUnits,
		decimals,
		senderATA,
		mint,
		destATA,
		senderWallet.PublicKey(),
		nil,
	).Build())

	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("GetLatestBlockhash failed: %w", err)
	}
	tx, err := solana.NewTransaction(
		instructions,
		recent.Value.Blockhash,
		solana.TransactionPayer(feePayerWallet.PublicKey()),
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(senderWallet.PublicKey()) {
			return &senderWallet.PrivateKey
		}
		if key.Equals(feePayerWallet.PublicKey()) {
			return &feePayerWallet.PrivateKey
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	sig, err := confirm.SendAndConfirmTransaction(ctx, client, wsClient, tx)
	if err != nil {
		return "", fmt.Errorf("SendAndConfirmTransaction failed: %w", err)
	}
	return sig.String(), nil
}

func AssetMintConfig(asset string) (mint string, decimals uint8, rpcURL string) {
	cluster := os.Getenv("SOLANA_CLUSTER")
	if cluster == "" {
		cluster = "devnet"
	}
	isDevnet := cluster == "devnet" || cluster == "testnet"

	switch asset {
	case "usdc_sol":
		if isDevnet {
			return os.Getenv("DEVNET_SOL_USDC_MINT"), 6, os.Getenv("DEVNET_SOLANA_RPC_URL")
		}
		return os.Getenv("MAINNET_SOL_USDC_MINT"), 6, os.Getenv("MAINNET_SOLANA_RPC_URL")
	case "usdt_sol":
		return os.Getenv("MAINNET_SOL_USDT_MINT"), 6, os.Getenv("MAINNET_SOLANA_RPC_URL")
	case "usdg_sol":
		return os.Getenv("MAINNET_SOL_USDG_MINT"), 6, os.Getenv("MAINNET_SOLANA_RPC_URL")
	case "wsol":
		if isDevnet {
			m := os.Getenv("DEVNET_SOL_WSOL_MINT")
			if m == "" {
				m = defaultWSOLMint
			}
			return m, 9, os.Getenv("DEVNET_SOLANA_RPC_URL")
		}
		m := os.Getenv("MAINNET_SOL_WSOL_MINT")
		if m == "" {
			m = defaultWSOLMint
		}
		return m, 9, os.Getenv("MAINNET_SOLANA_RPC_URL")
	default:
		return "", 0, ""
	}
}

func FundDemoAccount(recipientPubKey string, amountSol float64) (string, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)

	client := rpc.New(rpcURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsClient, err := ws.Connect(ctx, wsURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer wsClient.Close()

	faucetSecretRaw := os.Getenv("VANT_FAUCET_KEYPAIR")
	var faucetBytes []byte
	if err = json.Unmarshal([]byte(faucetSecretRaw), &faucetBytes); err != nil {
		return "", fmt.Errorf("failed to parse faucet keypair: %w", err)
	}

	faucetWallet := &solana.Wallet{PrivateKey: solana.PrivateKey(faucetBytes)}

	dest, err := solana.PublicKeyFromBase58(recipientPubKey)
	if err != nil {
		return "", fmt.Errorf("invalid recipient pubkey %s: %w", recipientPubKey, err)
	}

	lamports := uint64(amountSol * 1e9)

	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("GetLatestBlockhash failed: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			computebudget.NewSetComputeUnitPriceInstruction(100000).Build(),
			system.NewTransferInstruction(
				lamports,
				faucetWallet.PublicKey(),
				dest,
			).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(faucetWallet.PublicKey()),
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(faucetWallet.PublicKey()) {
			return &faucetWallet.PrivateKey
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	sig, err := confirm.SendAndConfirmTransaction(ctx, client, wsClient, tx)
	if err != nil {
		return "", fmt.Errorf("SendAndConfirmTransaction failed: %w", err)
	}

	return sig.String(), nil
}
