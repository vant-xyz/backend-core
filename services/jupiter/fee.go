package jupiter

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
)

// ataProgramID is the Associated Token Account program.
var ataProgramID = solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJe1bN8")

const (
	FeeBps = 50 // 0.5%

	// DefaultDepositMint is mainnet USDC.
	DefaultDepositMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	// maxTxSize is the hard limit for a serialized Solana transaction (one packet).
	maxTxSize = 1232
)

// CalcFee returns the Vantic fee in the same token units as depositAmount.
func CalcFee(depositAmount uint64) uint64 {
	return depositAmount * FeeBps / 10_000
}

// InjectFee takes Jupiter's unsigned base64 transaction, appends a SPL Token
// Transfer of the Vantic fee from owner → V2_FEE_WALLET, and returns the
// modified base64 transaction plus the fee amount deducted.
//
// Only call this for buy orders (isBuy=true). Close/claim txs pass through unchanged.
func InjectFee(txBase64, ownerPubkey, depositMint string, depositAmount uint64) (string, uint64, error) {
	feeWalletAddr := os.Getenv("V2_FEE_WALLET")
	if feeWalletAddr == "" {
		return "", 0, fmt.Errorf("V2_FEE_WALLET env var not set")
	}
	if depositMint == "" {
		depositMint = DefaultDepositMint
	}

	feeAmount := CalcFee(depositAmount)
	if feeAmount == 0 {
		return txBase64, 0, nil
	}

	txBytes, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", 0, fmt.Errorf("decode tx base64: %w", err)
	}
	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		return "", 0, fmt.Errorf("deserialize transaction: %w", err)
	}

	owner, err := solana.PublicKeyFromBase58(ownerPubkey)
	if err != nil {
		return "", 0, fmt.Errorf("invalid owner pubkey: %w", err)
	}
	feeWallet, err := solana.PublicKeyFromBase58(feeWalletAddr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid V2_FEE_WALLET: %w", err)
	}
	mint, err := solana.PublicKeyFromBase58(depositMint)
	if err != nil {
		return "", 0, fmt.Errorf("invalid deposit mint: %w", err)
	}

	sourceATA, _, err := solana.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return "", 0, fmt.Errorf("derive source ATA: %w", err)
	}
	destATA, _, err := solana.FindAssociatedTokenAddress(feeWallet, mint)
	if err != nil {
		return "", 0, fmt.Errorf("derive dest ATA: %w", err)
	}

	// 1. CreateIdempotent ATA for the fee wallet if it doesn't exist yet.
	//    The user (tx fee payer) covers the ~0.002 SOL rent on the very first
	//    trade ever. All subsequent trades see this as a compute-only no-op.
	createATAIx := createIdempotentATA(owner, destATA, feeWallet, mint)
	createATACompiled, err := compileInstruction(tx, createATAIx)
	if err != nil {
		return "", 0, fmt.Errorf("compile create-ATA instruction: %w", err)
	}
	tx.Message.Instructions = append(tx.Message.Instructions, createATACompiled)

	// 2. Transfer the Vantic fee into the fee wallet's ATA.
	transferIx := token.NewTransferInstruction(feeAmount, sourceATA, destATA, owner, nil).Build()
	transferCompiled, err := compileInstruction(tx, transferIx)
	if err != nil {
		return "", 0, fmt.Errorf("compile fee transfer instruction: %w", err)
	}
	tx.Message.Instructions = append(tx.Message.Instructions, transferCompiled)

	out, err := tx.MarshalBinary()
	if err != nil {
		return "", 0, fmt.Errorf("serialize modified transaction: %w", err)
	}

	// A Solana transaction must fit in a single 1232-byte packet. Jupiter's
	// order txs are frequently close to this limit, and our two extra
	// instructions plus any new account keys can push it over. If so, refuse
	// loudly rather than handing the user a tx that will be rejected on submit.
	if len(out) > maxTxSize {
		return "", 0, fmt.Errorf("fee injection overflowed tx size: %d > %d bytes", len(out), maxTxSize)
	}
	return base64.StdEncoding.EncodeToString(out), feeAmount, nil
}

// compileInstruction resolves each account meta against the transaction's
// existing account key list, inserting missing accounts in the correct slot
// (writable before readonly unsigned), and returns a CompiledInstruction.
//
// Insertion happens in two phases. Phase 1 ensures every key exists, which may
// insert keys and shift the indices of instructions already on the message.
// Phase 2 reads each key's FINAL index. Doing it in two phases is essential:
// resolving the program index up front (as the previous version did) left it
// stale once a later writable-account insert shifted the account list — which
// silently corrupted the program reference on v0 transactions with lookup tables.
func compileInstruction(tx *solana.Transaction, ix solana.Instruction) (solana.CompiledInstruction, error) {
	data, err := ix.Data()
	if err != nil {
		return solana.CompiledInstruction{}, fmt.Errorf("instruction data: %w", err)
	}

	// Phase 1 — ensure all keys are present.
	for _, meta := range ix.Accounts() {
		if meta.IsWritable {
			findOrAddWritable(tx, meta.PublicKey)
		} else {
			findOrAddReadonly(tx, meta.PublicKey)
		}
	}
	findOrAddReadonly(tx, ix.ProgramID())

	// Phase 2 — read final indices now that no further insertions will occur
	// for this instruction.
	programIdx, ok := findAccount(tx, ix.ProgramID())
	if !ok {
		return solana.CompiledInstruction{}, fmt.Errorf("program key missing after insert")
	}
	accountIdxs := make([]uint16, len(ix.Accounts()))
	for i, meta := range ix.Accounts() {
		idx, ok := findAccount(tx, meta.PublicKey)
		if !ok {
			return solana.CompiledInstruction{}, fmt.Errorf("account key missing after insert")
		}
		accountIdxs[i] = idx
	}

	return solana.CompiledInstruction{
		ProgramIDIndex: programIdx,
		Accounts:       accountIdxs,
		Data:           solana.Base58(data),
	}, nil
}

// findOrAddWritable returns the index of key in AccountKeys, inserting it as
// a writable non-signer (before the readonly unsigned accounts) if absent.
func findOrAddWritable(tx *solana.Transaction, key solana.PublicKey) uint16 {
	if idx, ok := findAccount(tx, key); ok {
		return idx
	}
	// Insert before readonly unsigned accounts.
	insertAt := uint16(len(tx.Message.AccountKeys) - int(tx.Message.Header.NumReadonlyUnsignedAccounts))
	insertAt16 := insertAt

	tx.Message.AccountKeys = append(tx.Message.AccountKeys, solana.PublicKey{})
	copy(tx.Message.AccountKeys[insertAt16+1:], tx.Message.AccountKeys[insertAt16:])
	tx.Message.AccountKeys[insertAt16] = key

	shiftIndices(tx, insertAt16)
	return insertAt16
}

// findOrAddReadonly returns the index of key in AccountKeys, appending it as
// a readonly unsigned static account if absent.
//
// The new key lands at the end of the STATIC key list, which on a versioned
// (v0) transaction is exactly where the lookup-table accounts begin in the
// virtual account ordering. Every existing instruction index at or beyond that
// boundary therefore refers to a lookup-table account that has just shifted up
// by one, so we must shift those references too. On a legacy transaction there
// are no such indices and shiftIndices is a no-op.
func findOrAddReadonly(tx *solana.Transaction, key solana.PublicKey) uint16 {
	if idx, ok := findAccount(tx, key); ok {
		return idx
	}
	idx := uint16(len(tx.Message.AccountKeys))
	shiftIndices(tx, idx)
	tx.Message.AccountKeys = append(tx.Message.AccountKeys, key)
	tx.Message.Header.NumReadonlyUnsignedAccounts++
	return idx
}

func findAccount(tx *solana.Transaction, key solana.PublicKey) (uint16, bool) {
	for i, ak := range tx.Message.AccountKeys {
		if ak.Equals(key) {
			return uint16(i), true
		}
	}
	return 0, false
}

// createIdempotentATA builds a raw CreateIdempotent ATA instruction (discriminator 1).
// gagliardetto v1.14.0 only ships Create (discriminator 0), so we build this manually.
// Accounts: [funder(w,s), ata(w), wallet(r), mint(r), systemProgram(r), tokenProgram(r)]
func createIdempotentATA(funder, ata, wallet, mint solana.PublicKey) solana.Instruction {
	return solana.NewInstruction(
		ataProgramID,
		solana.AccountMetaSlice{
			{PublicKey: funder, IsWritable: true, IsSigner: true},
			{PublicKey: ata, IsWritable: true, IsSigner: false},
			{PublicKey: wallet, IsWritable: false, IsSigner: false},
			{PublicKey: mint, IsWritable: false, IsSigner: false},
			{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
			{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		},
		[]byte{1}, // CreateIdempotent discriminator
	)
}

// shiftIndices increments every account index >= threshold in existing
// compiled instructions to account for a newly inserted account key.
func shiftIndices(tx *solana.Transaction, threshold uint16) {
	for i := range tx.Message.Instructions {
		if tx.Message.Instructions[i].ProgramIDIndex >= threshold {
			tx.Message.Instructions[i].ProgramIDIndex++
		}
		for j := range tx.Message.Instructions[i].Accounts {
			if tx.Message.Instructions[i].Accounts[j] >= threshold {
				tx.Message.Instructions[i].Accounts[j]++
			}
		}
	}
}
