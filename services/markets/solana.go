package markets

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

const (
	discriminatorCreateCAPPM = 0
	discriminatorCreateGEM   = 1
	discriminatorSettleCAPPM = 2
	discriminatorSettleGEM   = 3
	discriminatorGetMarket   = 4

	marketSeed     = "market"
	settlementSeed = "settlement"

	rpcTimeout = 30 * time.Second
)

func getProgramID() (solana.PublicKey, error) {
	raw := os.Getenv("VANT_PROGRAM_ID")
	if raw == "" {
		return solana.PublicKey{}, fmt.Errorf("VANT_PROGRAM_ID not set")
	}
	return solana.PublicKeyFromBase58(raw)
}

func getSettlerKeypair() (solana.PrivateKey, error) {
	raw := os.Getenv("VANT_MARKET_APPROVED_SETLLER_KEYPAIR")
	if raw == "" {
		return nil, fmt.Errorf("VANT_MARKET_APPROVED_SETLLER_KEYPAIR not set")
	}
	var keyBytes []byte
	if err := json.Unmarshal([]byte(raw), &keyBytes); err != nil {
		return nil, fmt.Errorf("failed to parse settler keypair: %w", err)
	}
	return solana.PrivateKey(keyBytes), nil
}

func deriveMarketPDA(marketID string) (solana.PublicKey, uint8, error) {
	programID, err := getProgramID()
	if err != nil {
		return solana.PublicKey{}, 0, err
	}
	addr, bump, err := solana.FindProgramAddress(
		[][]byte{[]byte(marketSeed), []byte(marketID)},
		programID,
	)
	if err != nil {
		return solana.PublicKey{}, 0, fmt.Errorf("failed to derive market PDA for %s: %w", marketID, err)
	}
	return addr, bump, nil
}

func deriveSettlementPDA(marketID string) (solana.PublicKey, uint8, error) {
	programID, err := getProgramID()
	if err != nil {
		return solana.PublicKey{}, 0, err
	}
	addr, bump, err := solana.FindProgramAddress(
		[][]byte{[]byte(settlementSeed), []byte(marketID)},
		programID,
	)
	if err != nil {
		return solana.PublicKey{}, 0, fmt.Errorf("failed to derive settlement PDA for %s: %w", marketID, err)
	}
	return addr, bump, nil
}

func writeString(buf []byte, offset *int, s string) {
	binary.LittleEndian.PutUint16(buf[*offset:], uint16(len(s)))
	*offset += 2
	copy(buf[*offset:], s)
	*offset += len(s)
}

func writeU64(buf []byte, offset *int, v uint64) {
	binary.LittleEndian.PutUint64(buf[*offset:], v)
	*offset += 8
}

func writeU8(buf []byte, offset *int, v uint8) {
	buf[*offset] = v
	*offset++
}

func stringLen(s string) int {
	return 2 + len(s)
}

// getFallbackRPCURLs returns the ordered list of RPC URLs to try.
// DEVNET_SOLANA_RPC_URL is always tried first. DEVNET_SOLANA_RPC_URL_1 and
// DEVNET_SOLANA_RPC_URL_2 are only tried if the previous URL fails.
// Falls back to the public devnet endpoint if no env vars are set.
func getFallbackRPCURLs() []string {
	urls := []string{
		os.Getenv("DEVNET_SOLANA_RPC_URL"),
		os.Getenv("DEVNET_SOLANA_RPC_URL_1"),
		os.Getenv("DEVNET_SOLANA_RPC_URL_2"),
	}

	var valid []string
	for _, url := range urls {
		if url != "" {
			valid = append(valid, url)
		}
	}

	if len(valid) == 0 {
		return []string{"https://api.devnet.solana.com"}
	}

	return valid
}

// sendAndConfirm builds, signs, and submits a transaction. It iterates through
// all fallback RPC URLs — each URL gets a fresh WS connection and blockhash.
// Transaction build and signing errors are terminal (not retried across URLs)
// since they indicate a code-level problem, not an RPC availability problem.
func sendAndConfirm(
	instructions []solana.Instruction,
	signers []solana.PrivateKey,
	feePayer solana.PublicKey,
) (string, error) {
	rpcURLs := getFallbackRPCURLs()
	var lastErr error

	for _, rpcURL := range rpcURLs {
		wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)

		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)

		wsClient, err := ws.Connect(ctx, wsURL)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("RPC %s ws connect failed: %w", rpcURL, err)
			continue
		}

		client := rpc.New(rpcURL)
		recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
		if err != nil {
			wsClient.Close()
			cancel()
			lastErr = fmt.Errorf("RPC %s GetLatestBlockhash failed: %w", rpcURL, err)
			continue
		}

		allInstructions := append(
			[]solana.Instruction{computebudget.NewSetComputeUnitPriceInstruction(100000).Build()},
			instructions...,
		)

		tx, err := solana.NewTransaction(allInstructions, recent.Value.Blockhash, solana.TransactionPayer(feePayer))
		if err != nil {
			wsClient.Close()
			cancel()
			return "", fmt.Errorf("failed to build transaction: %w", err)
		}

		keyMap := make(map[solana.PublicKey]*solana.PrivateKey, len(signers))
		for i := range signers {
			keyMap[signers[i].PublicKey()] = &signers[i]
		}

		if _, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey { return keyMap[key] }); err != nil {
			wsClient.Close()
			cancel()
			return "", fmt.Errorf("failed to sign transaction: %w", err)
		}

		sig, err := confirm.SendAndConfirmTransaction(ctx, client, wsClient, tx)
		wsClient.Close()
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("RPC %s SendAndConfirmTransaction failed: %w", rpcURL, err)
			continue
		}

		return sig.String(), nil
	}

	return "", fmt.Errorf("all RPC endpoints failed: %w", lastErr)
}

// buildEd25519VerifyInstruction constructs the Ed25519Program verify instruction
// manually by packing the instruction data according to the Solana Ed25519
// native program spec:
//
//	u16 num_signatures
//	per signature:
//	  u16 signature_offset
//	  u16 signature_instruction_index (0xFFFF = current)
//	  u16 public_key_offset
//	  u16 public_key_instruction_index (0xFFFF = current)
//	  u16 message_data_offset
//	  u16 message_data_size
//	  u16 message_instruction_index (0xFFFF = current)
//	followed by: signature (64 bytes) | pubkey (32 bytes) | message
func buildEd25519VerifyInstruction(pubKey solana.PublicKey, message []byte, sig []byte) (solana.Instruction, error) {
	if len(sig) != 64 {
		return nil, fmt.Errorf("signature must be 64 bytes, got %d", len(sig))
	}

	const headerSize = 2 + 14
	sigOffset := uint16(headerSize)
	pubKeyOffset := sigOffset + 64
	msgOffset := pubKeyOffset + 32
	msgSize := uint16(len(message))
	currentIx := uint16(0xFFFF)

	data := make([]byte, int(msgOffset)+len(message))

	binary.LittleEndian.PutUint16(data[0:], 1)
	binary.LittleEndian.PutUint16(data[2:], sigOffset)
	binary.LittleEndian.PutUint16(data[4:], currentIx)
	binary.LittleEndian.PutUint16(data[6:], pubKeyOffset)
	binary.LittleEndian.PutUint16(data[8:], currentIx)
	binary.LittleEndian.PutUint16(data[10:], msgOffset)
	binary.LittleEndian.PutUint16(data[12:], msgSize)
	binary.LittleEndian.PutUint16(data[14:], currentIx)

	copy(data[sigOffset:], sig)
	copy(data[pubKeyOffset:], pubKey[:])
	copy(data[msgOffset:], message)

	ed25519ProgramID := solana.MustPublicKeyFromBase58("Ed25519SigVerify111111111111111111111111111")

	return solana.NewInstruction(
		ed25519ProgramID,
		solana.AccountMetaSlice{},
		data,
	), nil
}

func SignSettlementMessage(message string) ([]byte, error) {
	privKey, err := getSettlerKeypair()
	if err != nil {
		return nil, err
	}
	sig, err := privKey.Sign([]byte(message))
	if err != nil {
		return nil, fmt.Errorf("failed to sign settlement message: %w", err)
	}
	return sig[:], nil
}

type CreateMarketCAPPMParams struct {
	MarketID        string
	Title           string
	Description     string
	StartTimeUTC    uint64
	DurationSeconds uint64
	Direction       uint8
	TargetPrice     uint64
	DataProvider    string
	CurrentPrice    uint64
	Asset           string
}

func CreateMarketCAPPM(params CreateMarketCAPPMParams) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(params.MarketID)
	if err != nil {
		return "", err
	}

	size := 1 +
		stringLen(params.MarketID) +
		stringLen(params.Title) +
		stringLen(params.Description) +
		8 + 8 + 1 + 8 +
		stringLen(params.DataProvider) +
		8 +
		stringLen(params.Asset)

	data := make([]byte, size)
	off := 0
	writeU8(data, &off, discriminatorCreateCAPPM)
	writeString(data, &off, params.MarketID)
	writeString(data, &off, params.Title)
	writeString(data, &off, params.Description)
	writeU64(data, &off, params.StartTimeUTC)
	writeU64(data, &off, params.DurationSeconds)
	writeU8(data, &off, params.Direction)
	writeU64(data, &off, params.TargetPrice)
	writeString(data, &off, params.DataProvider)
	writeU64(data, &off, params.CurrentPrice)
	writeString(data, &off, params.Asset)

	creatorPub := settlerKey.PublicKey()
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: creatorPub, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}, data)

	return sendAndConfirm([]solana.Instruction{ix}, []solana.PrivateKey{settlerKey}, creatorPub)
}

type CreateMarketGEMParams struct {
	MarketID        string
	Title           string
	Description     string
	StartTimeUTC    uint64
	DurationSeconds uint64
	DataProvider    string
}

func CreateMarketGEM(params CreateMarketGEMParams) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(params.MarketID)
	if err != nil {
		return "", err
	}

	size := 1 +
		stringLen(params.MarketID) +
		stringLen(params.Title) +
		stringLen(params.Description) +
		8 + 8 +
		stringLen(params.DataProvider)

	data := make([]byte, size)
	off := 0
	writeU8(data, &off, discriminatorCreateGEM)
	writeString(data, &off, params.MarketID)
	writeString(data, &off, params.Title)
	writeString(data, &off, params.Description)
	writeU64(data, &off, params.StartTimeUTC)
	writeU64(data, &off, params.DurationSeconds)
	writeString(data, &off, params.DataProvider)

	creatorPub := settlerKey.PublicKey()
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: creatorPub, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}, data)

	return sendAndConfirm([]solana.Instruction{ix}, []solana.PrivateKey{settlerKey}, creatorPub)
}

func SettleMarketCAPPM(marketID string, endPriceCents uint64) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(marketID)
	if err != nil {
		return "", err
	}
	settlementPDA, _, err := deriveSettlementPDA(marketID)
	if err != nil {
		return "", err
	}

	message := fmt.Sprintf("VANT_CAPPM_SETTLEMENT:%s:%d", marketID, endPriceCents)
	msgBytes := []byte(message)
	sig, err := SignSettlementMessage(message)
	if err != nil {
		return "", err
	}

	settlerPub := settlerKey.PublicKey()

	ed25519Ix, err := buildEd25519VerifyInstruction(settlerPub, msgBytes, sig)
	if err != nil {
		return "", err
	}

	size := 1 + stringLen(marketID) + 8 + 64
	data := make([]byte, size)
	off := 0
	writeU8(data, &off, discriminatorSettleCAPPM)
	writeString(data, &off, marketID)
	writeU64(data, &off, endPriceCents)
	copy(data[off:], sig)

	settleIx := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: settlementPDA, IsSigner: false, IsWritable: true},
		{PublicKey: settlerPub, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SysVarInstructionsPubkey, IsSigner: false, IsWritable: false},
	}, data)

	return sendAndConfirm(
		[]solana.Instruction{ed25519Ix, settleIx},
		[]solana.PrivateKey{settlerKey},
		settlerPub,
	)
}

func SettleMarketGEM(marketID string, outcome uint8, outcomeDescription string) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(marketID)
	if err != nil {
		return "", err
	}
	settlementPDA, _, err := deriveSettlementPDA(marketID)
	if err != nil {
		return "", err
	}

	outcomeStr := "YES"
	if outcome == 1 {
		outcomeStr = "NO"
	}
	message := fmt.Sprintf("VANT_GEM_SETTLEMENT:%s:%s", marketID, outcomeStr)
	msgBytes := []byte(message)
	sig, err := SignSettlementMessage(message)
	if err != nil {
		return "", err
	}

	settlerPub := settlerKey.PublicKey()

	ed25519Ix, err := buildEd25519VerifyInstruction(settlerPub, msgBytes, sig)
	if err != nil {
		return "", err
	}

	size := 1 + stringLen(marketID) + 1 + stringLen(outcomeDescription) + 64
	data := make([]byte, size)
	off := 0
	writeU8(data, &off, discriminatorSettleGEM)
	writeString(data, &off, marketID)
	writeU8(data, &off, outcome)
	writeString(data, &off, outcomeDescription)
	copy(data[off:], sig)

	settleIx := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: settlementPDA, IsSigner: false, IsWritable: true},
		{PublicKey: settlerPub, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SysVarInstructionsPubkey, IsSigner: false, IsWritable: false},
	}, data)

	return sendAndConfirm(
		[]solana.Instruction{ed25519Ix, settleIx},
		[]solana.PrivateKey{settlerKey},
		settlerPub,
	)
}

type OnchainMarket struct {
	MarketID           string
	MarketType         uint8
	IsResolved         bool
	Creator            string
	ApprovedSettler    string
	Title              string
	Description        string
	StartTimeUTC       uint64
	EndTimeUTC         uint64
	DurationSeconds    uint64
	DataProvider       string
	CreatedAt          uint64
	Asset              string
	Direction          *uint8
	TargetPrice        *uint64
	CurrentPrice       *uint64
	EndPrice           *uint64
	Outcome            *uint8
	OutcomeDescription string
}

// GetMarketOnchain fetches raw account data from Solana with fallback RPC support.
func GetMarketOnchain(marketID string) (*OnchainMarket, error) {
	marketPDA, _, err := deriveMarketPDA(marketID)
	if err != nil {
		return nil, err
	}

	rpcURLs := getFallbackRPCURLs()
	var lastErr error

	for _, rpcURL := range rpcURLs {
		client := rpc.New(rpcURL)
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)

		accountInfo, err := client.GetAccountInfo(ctx, marketPDA)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("RPC %s GetAccountInfo failed: %w", rpcURL, err)
			continue
		}
		if accountInfo == nil || accountInfo.Value == nil {
			return nil, fmt.Errorf("market account not found: %s", marketID)
		}

		return unpackOnchainMarket(marketID, accountInfo.Value.Data.GetBinary())
	}

	return nil, fmt.Errorf("all RPC endpoints failed: %w", lastErr)
}

func unpackOnchainMarket(marketID string, raw []byte) (*OnchainMarket, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty market account data for %s", marketID)
	}

	m := &OnchainMarket{MarketID: marketID}
	pos := 0

	readU8 := func() (uint8, error) {
		if pos >= len(raw) {
			return 0, fmt.Errorf("buffer underflow at offset %d", pos)
		}
		v := raw[pos]
		pos++
		return v, nil
	}
	readU64 := func() (uint64, error) {
		if pos+8 > len(raw) {
			return 0, fmt.Errorf("buffer underflow reading u64 at offset %d", pos)
		}
		v := binary.LittleEndian.Uint64(raw[pos:])
		pos += 8
		return v, nil
	}
	readStr := func() (string, error) {
		if pos+2 > len(raw) {
			return "", fmt.Errorf("buffer underflow reading string length at offset %d", pos)
		}
		l := int(binary.LittleEndian.Uint16(raw[pos:]))
		pos += 2
		if pos+l > len(raw) {
			return "", fmt.Errorf("buffer underflow reading string data at offset %d", pos)
		}
		s := string(raw[pos : pos+l])
		pos += l
		return s, nil
	}
	readPubkey := func() (string, error) {
		if pos+32 > len(raw) {
			return "", fmt.Errorf("buffer underflow reading pubkey at offset %d", pos)
		}
		pk := solana.PublicKeyFromBytes(raw[pos : pos+32])
		pos += 32
		return pk.String(), nil
	}

	var err error

	if m.MarketType, err = readU8(); err != nil {
		return nil, err
	}
	resolved, err := readU8()
	if err != nil {
		return nil, err
	}
	m.IsResolved = resolved == 1

	if m.Creator, err = readPubkey(); err != nil {
		return nil, err
	}
	if m.ApprovedSettler, err = readPubkey(); err != nil {
		return nil, err
	}
	if m.Title, err = readStr(); err != nil {
		return nil, err
	}
	if m.Description, err = readStr(); err != nil {
		return nil, err
	}
	if m.StartTimeUTC, err = readU64(); err != nil {
		return nil, err
	}
	if m.EndTimeUTC, err = readU64(); err != nil {
		return nil, err
	}
	if m.DurationSeconds, err = readU64(); err != nil {
		return nil, err
	}
	if m.DataProvider, err = readStr(); err != nil {
		return nil, err
	}
	if m.CreatedAt, err = readU64(); err != nil {
		return nil, err
	}

	if _, err = readU8(); err != nil {
		return nil, err
	}

	if m.Asset, err = readStr(); err != nil {
		return nil, err
	}

	dirPresent, err := readU8()
	if err != nil {
		return nil, err
	}
	dirVal, err := readU8()
	if err != nil {
		return nil, err
	}
	if dirPresent == 1 {
		v := dirVal
		m.Direction = &v
	}

	targetPresent, err := readU8()
	if err != nil {
		return nil, err
	}
	targetVal, err := readU64()
	if err != nil {
		return nil, err
	}
	if targetPresent == 1 {
		v := targetVal
		m.TargetPrice = &v
	}

	currentPresent, err := readU8()
	if err != nil {
		return nil, err
	}
	currentVal, err := readU64()
	if err != nil {
		return nil, err
	}
	if currentPresent == 1 {
		v := currentVal
		m.CurrentPrice = &v
	}

	endPresent, err := readU8()
	if err != nil {
		return nil, err
	}
	endVal, err := readU64()
	if err != nil {
		return nil, err
	}
	if endPresent == 1 {
		v := endVal
		m.EndPrice = &v
	}

	outcomePresent, err := readU8()
	if err != nil {
		return nil, err
	}
	outcomeVal, err := readU8()
	if err != nil {
		return nil, err
	}
	if outcomePresent == 1 {
		v := outcomeVal
		m.Outcome = &v
	}

	if m.OutcomeDescription, err = readStr(); err != nil {
		return nil, err
	}

	return m, nil
}