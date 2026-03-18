package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
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
		senderWallet = &solana.Wallet{PrivateKey: solana.PrivateKey(solana.PrivateKey(senderPrivateKey))}
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
		solana.TransactionPayer(senderWallet.PublicKey()),
	)
	if err != nil {
		return "", err
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(senderWallet.PublicKey()) {
			return &senderWallet.PrivateKey
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
