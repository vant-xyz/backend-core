package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gagliardetto/solana-go"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

func GetSolBalance(pubKey string) (float64, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	client := rpc.New(rpcURL)

	account, err := solana.PublicKeyFromBase58(pubKey)
	if err != nil {
		return 0, err
	}

	res, err := client.GetBalance(context.TODO(), account, rpc.CommitmentFinalized)
	if err != nil {
		return 0, err
	}

	return float64(res.Value) / 1e9, nil
}

// GetSPLBalance fetches the balance of an SPL token for a given wallet
func GetSPLBalance(walletPubKey, mintPubKey, rpcURL, tokenName string) (float64, error) {
	client := rpc.New(rpcURL)

	wallet, err := solana.PublicKeyFromBase58(walletPubKey)
	if err != nil {
		return 0, err
	}

	mint, err := solana.PublicKeyFromBase58(mintPubKey)
	if err != nil {
		return 0, err
	}

	ata, _, err := solana.FindAssociatedTokenAddress(wallet, mint)
	if err != nil {
		return 0, err
	}

	res, err := client.GetTokenAccountBalance(context.TODO(), ata, rpc.CommitmentFinalized)
	if err != nil {
		return 0, nil
	}

	amount, err := strconv.ParseFloat(res.Value.Amount, 64)
	if err != nil {
		return 0, err
	}

	decimals := res.Value.Decimals
	if decimals == 0 {
		decimals = 6
	}

	divisor := 1.0
	for i := 0; i < int(decimals); i++ {
		divisor *= 10
	}

	return amount / divisor, nil
}

func GetAllSPLBalances(walletPubKey string) (usdc, usdt, usdg float64, err error) {
	devnetRPC := os.Getenv("DEVNET_SOLANA_RPC_URL")
	mainnetRPC := os.Getenv("MAINNET_SOLANA_RPC_URL")

	usdcMint := os.Getenv("DEVNET_SOL_USDC_MINT")
	usdtMint := os.Getenv("MAINNET_SOL_USDT_MINT")
	usdgMint := os.Getenv("MAINNET_SOL_USDG_MINT")

	if usdcMint != "" && devnetRPC != "" {
		usdc, err = GetSPLBalance(walletPubKey, usdcMint, devnetRPC, "USDC")
		if err != nil {
			usdc = 0
		}
	}

	if usdtMint != "" && mainnetRPC != "" {
		usdt, err = GetSPLBalance(walletPubKey, usdtMint, mainnetRPC, "USDT")
		if err != nil {
			usdt = 0
		}
	}

	if usdgMint != "" && mainnetRPC != "" {
		usdg, err = GetSPLBalance(walletPubKey, usdgMint, mainnetRPC, "USDG")
		if err != nil {
			usdg = 0
		}
	}

	return usdc, usdt, usdg, nil
}

func TransferSol(senderPrivateKey, recipientPublicKey string, amountSol float64) (string, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)
	
	client := rpc.New(rpcURL)
	wsClient, err := ws.Connect(context.TODO(), wsURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to websocket: %v", err)
	}
	defer wsClient.Close()

	senderWallet, err := solana.WalletFromPrivateKeyBase58(senderPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create sender wallet from private key: %v", err)
	}

	feePayerSecret := os.Getenv("VANT_FEE_PAYER_SOLANA")
	feePayerWallet, err := solana.WalletFromPrivateKeyBase58(feePayerSecret)
	if err != nil {
		return "", fmt.Errorf("failed to create fee payer wallet: %v", err)
	}

	dest, err := solana.PublicKeyFromBase58(recipientPublicKey)
	if err != nil {
		return "", err
	}

	lamports := uint64(amountSol * 1e9)

	recent, err := client.GetLatestBlockhash(context.TODO(), rpc.CommitmentFinalized)
	if err != nil {
		return "", err
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
		return "", err
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
		return "", err
	}

	sig, err := confirm.SendAndConfirmTransaction(
		context.TODO(),
		client,
		wsClient,
		tx,
	)
	if err != nil {
		return "", err
	}

	return sig.String(), nil
}

func FundDemoAccount(recipientPubKey string, amountSol float64) (string, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)
	
	client := rpc.New(rpcURL)
	wsClient, err := ws.Connect(context.TODO(), wsURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to websocket: %v", err)
	}
	defer wsClient.Close()

	faucetSecretRaw := os.Getenv("VANT_FAUCET_KEYPAIR")
	var faucetBytes []byte
	err = json.Unmarshal([]byte(faucetSecretRaw), &faucetBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse faucet keypair: %v", err)
	}

	faucetWallet := &solana.Wallet{PrivateKey: solana.PrivateKey(faucetBytes)}

	dest, err := solana.PublicKeyFromBase58(recipientPubKey)
	if err != nil {
		return "", err
	}

	lamports := uint64(amountSol * 1e9)

	recent, err := client.GetLatestBlockhash(context.TODO(), rpc.CommitmentFinalized)
	if err != nil {
		return "", err
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
		return "", err
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(faucetWallet.PublicKey()) {
			return &faucetWallet.PrivateKey
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	sig, err := confirm.SendAndConfirmTransaction(
		context.TODO(),
		client,
		wsClient,
		tx,
	)
	if err != nil {
		return "", err
	}

	return sig.String(), nil
}
