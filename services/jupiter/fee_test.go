package jupiter

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gagliardetto/solana-go"
)

// realV0TxWithALT is a genuine on-chain Solana v0 (versioned) transaction that
// uses an Address Lookup Table, taken from the gagliardetto/solana-go test
// suite. Jupiter's order transactions are the same shape — v0 with ALTs — so
// this exercises the exact code path that is risky for fee injection: inserting
// static account keys and shifting instruction indices across the
// static/lookup-table boundary without corrupting any existing references.
const realV0TxWithALT = "Alkhq/BfGdBeok4oBP21xAwT4oO/R5PvkKqbCTq4sHHRsto+uDQCFcdp8hXh1g5D3mTh8GAJW8xE+EDD27f9IweTkH2Afiu4h5aM+Xbo0mklc0/Vi1xawd7SZVbstXDLtWdoJaf4Zt+20F/SasURzw/P4dkD+Q6BjgUNHT+vg5gOgAIBAQgaJV0Ch/DG6XwNcizWbI7STLgSbIOrg0Dl67Oo30WU1uA/NIbYLPRmuLarIJ4J0CcN3IWEm4Gf8675KhnXef2LaDXzjFgWVSbAO2yyTF6dK1oO3gTExie957LXDwu6oJMAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAVKU1qZKSEGTSTocWDaOHx8NbXdvJK7geQfqEBBBUSN1LfoiB9oYLDSHJL9rjAlchZhn+fd/23ACfq0oIGla54pt5JT0MdBTJhQI+z7dnVsisw2xWwW+vFSTs97l0tJPxmv9kxpXbHYZFenDpT2s6CT75/9QNFVTkHFLMK+UG6VlyFnQmYh1aMkGtq3c6TIOsk32S6XMUnN9DQgFGQq4lwEAwIAAgwCAAAAgJaYAAAAAAADAgAFDAIAAACAlpgAAAAAAAMCAAYMAgAAAICWmAAAAAAABAAMSGVsbG8gRmFiaW8hAX5s37FH6IeB4QeMYxD4LtpXf1DaupH/ro7W+kEQnofaAgECAQA="

const testFeeWallet = "DjVE6JNiYqPL2QXyCUUh8rNjHrbz9hXHNYt99MQ59qw1"

// accountID returns a stable identity for an instruction account index that does
// NOT depend on resolving lookup tables. Static keys are identified by their
// pubkey; lookup-table accounts by their slot position within the table region
// (index minus the number of static keys). This lets us prove that fee injection
// preserves every existing account reference: a static reference must still point
// to the same pubkey, and a lookup-table reference must still point to the same
// table slot.
func accountID(m *solana.Message, idx uint16) string {
	n := uint16(len(m.AccountKeys))
	if idx < n {
		return "static:" + m.AccountKeys[idx].String()
	}
	return fmt.Sprintf("alt:%d", idx-n)
}

func snapshotInstruction(m *solana.Message, ix solana.CompiledInstruction) (string, []string) {
	prog := accountID(m, ix.ProgramIDIndex)
	accs := make([]string, len(ix.Accounts))
	for i, a := range ix.Accounts {
		accs[i] = accountID(m, a)
	}
	return prog, accs
}

func TestCalcFee(t *testing.T) {
	cases := []struct {
		deposit uint64
		want    uint64
	}{
		{5_000_000, 25_000},      // $5 → $0.025
		{100_000_000, 500_000},   // $100 → $0.50
		{1_000_000, 5_000},       // $1 → $0.005
		{0, 0},                   // no deposit → no fee
		{199, 0},                 // dust rounds down to 0
	}
	for _, c := range cases {
		if got := CalcFee(c.deposit); got != c.want {
			t.Errorf("CalcFee(%d) = %d, want %d", c.deposit, got, c.want)
		}
	}
}

func TestInjectFee_RequiresFeeWallet(t *testing.T) {
	t.Setenv("V2_FEE_WALLET", "")
	_, _, err := InjectFee(realV0TxWithALT, "11111111111111111111111111111111", DefaultDepositMint, 5_000_000)
	if err == nil {
		t.Fatal("expected error when V2_FEE_WALLET is unset, got nil")
	}
}

// TestInjectFee_PreservesV0WithALT is the core correctness test. It runs a real
// v0+ALT transaction through InjectFee and asserts that:
//  1. every original instruction's program + accounts still resolve to the same
//     identities (no index corruption across the static/lookup boundary),
//  2. exactly two instructions are appended (create-ATA + fee transfer),
//  3. the fee transfer is a well-formed SPL token Transfer of the right amount
//     from the owner's ATA to the fee wallet's ATA,
//  4. the lookup tables are left untouched,
//  5. the result still fits in one packet.
func TestInjectFee_PreservesV0WithALT(t *testing.T) {
	t.Setenv("V2_FEE_WALLET", testFeeWallet)

	rawBefore, err := base64.StdEncoding.DecodeString(realV0TxWithALT)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	txBefore, err := solana.TransactionFromBytes(rawBefore)
	if err != nil {
		t.Fatalf("deserialize fixture: %v", err)
	}
	if !txBefore.Message.IsVersioned() || txBefore.Message.GetAddressTableLookups().NumLookups() == 0 {
		t.Fatal("fixture is not a v0 transaction with address lookup tables")
	}

	// Use an existing signer as the order owner — mirrors a real Jupiter order
	// where the owner is already the fee payer / signer[0].
	owner := txBefore.Message.AccountKeys[0]
	const depositMint = DefaultDepositMint
	const depositAmount = uint64(50_000_000) // $50
	wantFee := CalcFee(depositAmount)

	// Snapshot every original instruction's resolved identities.
	origCount := len(txBefore.Message.Instructions)
	type snap struct {
		prog string
		accs []string
	}
	before := make([]snap, origCount)
	for i, ix := range txBefore.Message.Instructions {
		p, a := snapshotInstruction(&txBefore.Message, ix)
		before[i] = snap{p, a}
	}
	origLookups := txBefore.Message.GetAddressTableLookups()

	// Inject.
	modifiedB64, feeAmount, err := InjectFee(realV0TxWithALT, owner.String(), depositMint, depositAmount)
	if err != nil {
		t.Fatalf("InjectFee: %v", err)
	}
	if feeAmount != wantFee {
		t.Errorf("fee amount = %d, want %d", feeAmount, wantFee)
	}

	rawAfter, err := base64.StdEncoding.DecodeString(modifiedB64)
	if err != nil {
		t.Fatalf("decode modified: %v", err)
	}
	txAfter, err := solana.TransactionFromBytes(rawAfter)
	if err != nil {
		t.Fatalf("deserialize modified: %v", err)
	}

	// (2) exactly two instructions appended.
	if got := len(txAfter.Message.Instructions); got != origCount+2 {
		t.Fatalf("instruction count = %d, want %d", got, origCount+2)
	}

	// (1) original instructions preserved, identity for identity.
	for i := 0; i < origCount; i++ {
		p, a := snapshotInstruction(&txAfter.Message, txAfter.Message.Instructions[i])
		if p != before[i].prog {
			t.Errorf("instruction %d program changed: %s -> %s", i, before[i].prog, p)
		}
		if len(a) != len(before[i].accs) {
			t.Fatalf("instruction %d account count changed: %d -> %d", i, len(before[i].accs), len(a))
		}
		for j := range a {
			if a[j] != before[i].accs[j] {
				t.Errorf("instruction %d account %d changed: %s -> %s", i, j, before[i].accs[j], a[j])
			}
		}
	}

	// (4) lookups untouched.
	if got := txAfter.Message.GetAddressTableLookups().NumLookups(); got != origLookups.NumLookups() {
		t.Errorf("lookup count changed: %d -> %d", origLookups.NumLookups(), got)
	}

	// (3) verify the appended fee transfer.
	mint := solana.MustPublicKeyFromBase58(depositMint)
	feeWallet := solana.MustPublicKeyFromBase58(testFeeWallet)
	wantSource, _, _ := solana.FindAssociatedTokenAddress(owner, mint)
	wantDest, _, _ := solana.FindAssociatedTokenAddress(feeWallet, mint)

	transferIx := txAfter.Message.Instructions[origCount+1]
	if prog := txAfter.Message.AccountKeys[transferIx.ProgramIDIndex]; !prog.Equals(solana.TokenProgramID) {
		t.Errorf("transfer program = %s, want token program", prog)
	}
	// SPL Token Transfer: data = [3, amount(u64 LE)].
	data := []byte(transferIx.Data)
	if len(data) != 9 || data[0] != 3 {
		t.Fatalf("transfer data malformed: %v", data)
	}
	if amt := binary.LittleEndian.Uint64(data[1:]); amt != wantFee {
		t.Errorf("transfer amount = %d, want %d", amt, wantFee)
	}
	// Accounts: [source, dest, authority].
	if len(transferIx.Accounts) != 3 {
		t.Fatalf("transfer accounts = %d, want 3", len(transferIx.Accounts))
	}
	gotSource := txAfter.Message.AccountKeys[transferIx.Accounts[0]]
	gotDest := txAfter.Message.AccountKeys[transferIx.Accounts[1]]
	gotAuth := txAfter.Message.AccountKeys[transferIx.Accounts[2]]
	if !gotSource.Equals(wantSource) {
		t.Errorf("transfer source = %s, want owner ATA %s", gotSource, wantSource)
	}
	if !gotDest.Equals(wantDest) {
		t.Errorf("transfer dest = %s, want fee-wallet ATA %s", gotDest, wantDest)
	}
	if !gotAuth.Equals(owner) {
		t.Errorf("transfer authority = %s, want owner %s", gotAuth, owner)
	}

	// create-idempotent ATA instruction sits just before the transfer.
	createIx := txAfter.Message.Instructions[origCount]
	if prog := txAfter.Message.AccountKeys[createIx.ProgramIDIndex]; !prog.Equals(ataProgramID) {
		t.Errorf("create-ATA program = %s, want ATA program", prog)
	}
	if d := []byte(createIx.Data); len(d) != 1 || d[0] != 1 {
		t.Errorf("create-ATA discriminator = %v, want [1] (CreateIdempotent)", d)
	}

	// (5) still one packet.
	if len(rawAfter) > maxTxSize {
		t.Errorf("modified tx size %d exceeds %d", len(rawAfter), maxTxSize)
	}
}
