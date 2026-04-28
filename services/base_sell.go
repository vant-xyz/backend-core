package services

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TransferBaseAssetToVault(encPriv, asset string, amount float64) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("amount must be positive")
	}
	vault := os.Getenv("VANT_ETH_VAULT_PUBLIC_KEY")
	if vault == "" {
		return "", fmt.Errorf("VANT_ETH_VAULT_PUBLIC_KEY not set")
	}

	privHex, err := Decrypt(encPriv)
	if err != nil {
		return "", err
	}
	privHex = strings.TrimPrefix(privHex, "0x")
	key, err := crypto.HexToECDSA(privHex)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if err := ensureBaseGasSponsored(ctx, key); err != nil {
		return "", err
	}

	to := common.HexToAddress(vault)
	switch asset {
	case "eth_base":
		return sendBaseNativeWithHash(ctx, key, to, amount)
	case "usdc_base":
		return sendBaseUSDCWithHash(ctx, key, to, amount)
	default:
		return "", fmt.Errorf("unsupported base asset for vault transfer: %s", asset)
	}
}

func sendBaseNativeWithHash(ctx context.Context, key *ecdsa.PrivateKey, to common.Address, amount float64) (string, error) {
	client, chainID, from, err := baseClientAndFrom(key)
	if err != nil {
		return "", err
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return "", err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return "", err
	}
	value := weiFromEth(amount)
	tx := types.NewTransaction(nonce, to, value, 21000, gasPrice, nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return "", err
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return "", err
	}
	return signed.Hash().Hex(), nil
}

func sendBaseUSDCWithHash(ctx context.Context, key *ecdsa.PrivateKey, to common.Address, amount float64) (string, error) {
	contractHex := os.Getenv("MAINNET_BASE_USDC_CONTRACT")
	if contractHex == "" {
		return "", fmt.Errorf("MAINNET_BASE_USDC_CONTRACT not set")
	}
	contract := common.HexToAddress(contractHex)
	client, chainID, from, err := baseClientAndFrom(key)
	if err != nil {
		return "", err
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return "", err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return "", err
	}
	methodID := crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	paddedTo := common.LeftPadBytes(to.Bytes(), 32)
	units := uint64(math.Round(amount * 1_000_000))
	paddedAmount := common.LeftPadBytes(new(big.Int).SetUint64(units).Bytes(), 32)
	data := append(methodID, append(paddedTo, paddedAmount...)...)

	tx := types.NewTransaction(nonce, contract, big.NewInt(0), 120000, gasPrice, data)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return "", err
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return "", err
	}
	return signed.Hash().Hex(), nil
}
