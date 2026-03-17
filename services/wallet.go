package services

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gagliardetto/solana-go"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/utils"
)

func GenerateWallet(email string) (*models.Wallet, error) {
	solAccount := solana.NewWallet()
	solPub := solAccount.PublicKey().String()
	solPriv := solAccount.PrivateKey.String()

	ethPrivKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	ethPriv := hexutil.Encode(crypto.FromECDSA(ethPrivKey))
	
	publicKey := ethPrivKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}
	ethPub := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()

	encSolPriv, err := Encrypt(solPriv)
	if err != nil {
		return nil, err
	}
	encEthPriv, err := Encrypt(ethPriv)
	if err != nil {
		return nil, err
	}

	nairaAcc := fmt.Sprintf("99%s", utils.RandomNumbers(8))

	return &models.Wallet{
		AccountID:          fmt.Sprintf("ACC_%s", utils.RandomAlphanumeric(10)),
		Email:              email,
		SolPublicKey:       solPub,
		SolPrivateKey:      encSolPriv,
		BasePublicKey:      ethPub,
		BasePrivateKey:     encEthPriv,
		NairaAccountNumber: nairaAcc,
	}, nil
}
