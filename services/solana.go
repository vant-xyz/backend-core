package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
)

func FundDemoAccount(recipientPubKey string, amountSol float64) (string, error) {
	rpcURL := os.Getenv("DEVNET_SOLANA_RPC_URL")
	client := rpc.New(rpcURL)

	faucetSecretRaw := os.Getenv("VANT_FAUCET_KEYPAIR")
	var faucetBytes []byte
	err := json.Unmarshal([]byte(faucetSecretRaw), &faucetBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse faucet keypair: %v", err)
	}

	faucetWallet, err := solana.WalletFromPrivateKeyBase58(solana.PrivateKey(faucetBytes).String())
	if err != nil {
		faucetWallet = &solana.Wallet{PrivateKey: solana.PrivateKey(faucetBytes)}
	}

	dest, err := solana.PublicKeyFromBase58(recipientPubKey)
	if err != nil {
		return "", err
	}

	lamports := uint64(amountSol * 1e9)

	recent, err := client.GetRecentBlockhash(context.TODO(), rpc.CommitmentFinalized)
	if err != nil {
		return "", err
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
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
		nil,
		tx,
	)
	if err != nil {
		return "", err
	}

	return sig.String(), nil
}
