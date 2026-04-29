package services

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/vant-xyz/backend-code/db"
)

func SweepDepositFeeOptimistic(email, asset, network string, feeAmount float64) {
	if feeAmount <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := sweepDepositFee(ctx, email, asset, network, feeAmount); err != nil {
			log.Printf("[FeeSweep] failed email=%s asset=%s network=%s fee=%.8f err=%v", email, asset, network, feeAmount, err)
		}
	}()
}

func sweepDepositFee(ctx context.Context, email, asset, network string, feeAmount float64) error {
	wallet, err := db.GetWalletByEmail(ctx, email)
	if err != nil {
		return err
	}
	if strings.Contains(network, "base") {
		return sweepBaseFee(ctx, wallet.BasePrivateKey, asset, feeAmount)
	}
	return sweepSolanaFee(wallet.SolPrivateKey, asset, network, feeAmount)
}

func sweepSolanaFee(encPriv, asset, network string, feeAmount float64) error {
	if feeAmount <= 0 {
		return nil
	}
	dec, err := Decrypt(encPriv)
	if err != nil {
		return err
	}
	if asset == "sol" || asset == "demo_sol" {
		_, err = TransferSol(dec, solFeeWalletPubKey(), feeAmount)
		return err
	}

	mint, decimals, rpcURL := solanaFeeMintConfig(asset, network)
	if mint == "" || rpcURL == "" {
		log.Printf("[FeeSweep] solana sweep skipped unsupported/misconfigured asset=%s network=%s", asset, network)
		return nil
	}
	_, err = TransferSPLToken(dec, solFeeWalletPubKey(), mint, decimals, feeAmount, rpcURL)
	return err
}

func solanaFeeMintConfig(asset, network string) (mint string, decimals uint8, rpcURL string) {
	decimals = 6
	isDevnet := strings.Contains(network, "devnet") || strings.Contains(network, "testnet")

	switch asset {
	case "usdc_sol", "demo_usdc_sol":
		if isDevnet {
			return os.Getenv("DEVNET_SOL_USDC_MINT"), 6, os.Getenv("DEVNET_SOLANA_RPC_URL")
		}
		return os.Getenv("MAINNET_SOL_USDC_MINT"), 6, os.Getenv("MAINNET_SOLANA_RPC_URL")
	case "usdt_sol":
		return os.Getenv("MAINNET_SOL_USDT_MINT"), 6, os.Getenv("MAINNET_SOLANA_RPC_URL")
	case "usdg_sol":
		return os.Getenv("MAINNET_SOL_USDG_MINT"), 6, os.Getenv("MAINNET_SOLANA_RPC_URL")
	case "wsol_sol", "wsol":
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

func sweepBaseFee(ctx context.Context, encPriv, asset string, feeAmount float64) error {
	if feeAmount <= 0 {
		return nil
	}
	feeWallet := os.Getenv("VANTIC_FEE_WALLET_BASE")
	if feeWallet == "" {
		return fmt.Errorf("VANTIC_FEE_WALLET_BASE not set")
	}
	privHex, err := Decrypt(encPriv)
	if err != nil {
		return err
	}
	privHex = strings.TrimPrefix(privHex, "0x")
	key, err := crypto.HexToECDSA(privHex)
	if err != nil {
		return err
	}
	// Sponsor gas for user-signed Base tx when needed.
	if err := ensureBaseGasSponsored(ctx, key); err != nil {
		return err
	}
	if asset == "eth_base" {
		return sendBaseNativeFee(ctx, key, common.HexToAddress(feeWallet), feeAmount)
	}
	if asset == "usdc_base" {
		return sendBaseUSDCFee(ctx, key, common.HexToAddress(feeWallet), feeAmount)
	}
	log.Printf("[FeeSweep] base sweep skipped unsupported asset=%s", asset)
	return nil
}

func ensureBaseGasSponsored(ctx context.Context, userKey *ecdsa.PrivateKey) error {
	sponsorHex := os.Getenv("VANT_FEE_PAYER_BASE")
	if sponsorHex == "" {
		return fmt.Errorf("VANT_FEE_PAYER_BASE not set")
	}
	sponsorHex = strings.TrimPrefix(sponsorHex, "0x")
	sponsorKey, err := crypto.HexToECDSA(sponsorHex)
	if err != nil {
		return fmt.Errorf("invalid VANT_FEE_PAYER_BASE: %w", err)
	}

	client, chainID, sponsorAddr, err := baseClientAndFrom(sponsorKey)
	if err != nil {
		return err
	}
	defer client.Close()

	userAddr := crypto.PubkeyToAddress(userKey.PublicKey)
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return err
	}
	// Cover a couple of typical txs with margin.
	required := new(big.Int).Mul(gasPrice, big.NewInt(180000))
	required = required.Mul(required, big.NewInt(2))

	userBal, err := client.BalanceAt(ctx, userAddr, nil)
	if err != nil {
		return err
	}
	if userBal.Cmp(required) >= 0 {
		return nil
	}

	topUp := new(big.Int).Sub(required, userBal)
	nonce, err := client.PendingNonceAt(ctx, sponsorAddr)
	if err != nil {
		return err
	}
	tx := types.NewTransaction(nonce, userAddr, topUp, 21000, gasPrice, nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), sponsorKey)
	if err != nil {
		return err
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return err
	}
	log.Printf("[FeeSweep] base gas sponsored user=%s amount_wei=%s", userAddr.Hex(), topUp.String())
	return nil
}

func sendBaseNativeFee(ctx context.Context, key *ecdsa.PrivateKey, to common.Address, amount float64) error {
	client, chainID, from, err := baseClientAndFrom(key)
	if err != nil {
		return err
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return err
	}
	value := weiFromEth(amount)
	tx := types.NewTransaction(nonce, to, value, 21000, gasPrice, nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return err
	}
	return client.SendTransaction(ctx, signed)
}

func sendBaseUSDCFee(ctx context.Context, key *ecdsa.PrivateKey, to common.Address, amount float64) error {
	contractHex := os.Getenv("MAINNET_BASE_USDC_CONTRACT")
	if contractHex == "" {
		return fmt.Errorf("MAINNET_BASE_USDC_CONTRACT not set")
	}
	contract := common.HexToAddress(contractHex)
	client, chainID, from, err := baseClientAndFrom(key)
	if err != nil {
		return err
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return err
	}
	// transfer(address,uint256)
	methodID := crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	paddedTo := common.LeftPadBytes(to.Bytes(), 32)
	units := uint64(math.Round(amount * 1_000_000))
	paddedAmount := common.LeftPadBytes(new(big.Int).SetUint64(units).Bytes(), 32)
	data := append(methodID, append(paddedTo, paddedAmount...)...)

	tx := types.NewTransaction(nonce, contract, big.NewInt(0), 120000, gasPrice, data)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return err
	}
	return client.SendTransaction(ctx, signed)
}

func baseClientAndFrom(key *ecdsa.PrivateKey) (*ethclient.Client, *big.Int, common.Address, error) {
	rpcURL := os.Getenv("MAINNET_BASE_HTTP_URL")
	if rpcURL == "" {
		rpcURL = os.Getenv("MAINNET_BASE_RPC_URL")
	}
	if rpcURL == "" {
		return nil, nil, common.Address{}, fmt.Errorf("base RPC env missing")
	}
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, nil, common.Address{}, err
	}
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		client.Close()
		return nil, nil, common.Address{}, err
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	return client, chainID, from, nil
}

func weiFromEth(v float64) *big.Int {
	if v <= 0 {
		return big.NewInt(0)
	}
	f := new(big.Float).Mul(big.NewFloat(v), big.NewFloat(1e18))
	out := new(big.Int)
	f.Int(out)
	return out
}

func solFeeWalletPubKey() string {
	if v := os.Getenv("VANTIC_FEE_WALLET_SOL"); v != "" {
		return v
	}
	return "EVy3WPB2iZpvh1Dw4DogkzZcVfdVr1tVhtqBpnKNje7G"
}
