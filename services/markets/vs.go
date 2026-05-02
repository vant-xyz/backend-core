package markets

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

const (
	discriminatorCreateVS  = 6
	discriminatorJoinVS    = 7
	discriminatorConfirmVS = 8
	discriminatorResolveVS = 9
	discriminatorCancelVS  = 10
)

type CreateVSEventInput struct {
	Title              string
	Description        string
	CreatorEmail       string
	Mode               models.VSMode
	Threshold          int
	StakeAmount        float64
	ParticipantTarget  int
	JoinDeadlineUTC    time.Time
	ResolveDeadlineUTC time.Time
}

func writeI32(buf []byte, offset *int, v int32) {
	binary.LittleEndian.PutUint32(buf[*offset:], uint32(v))
	*offset += 4
}

func CreateVSEvent(ctx context.Context, input CreateVSEventInput) (*models.VSEvent, error) {
	if input.Mode != models.VSModeMutual && input.Mode != models.VSModeConsensus {
		return nil, fmt.Errorf("invalid mode")
	}
	if input.StakeAmount <= 0 {
		return nil, fmt.Errorf("stake must be > 0")
	}
	if input.ParticipantTarget < 2 {
		return nil, fmt.Errorf("participant_target must be >= 2")
	}
	if input.ResolveDeadlineUTC.Before(input.JoinDeadlineUTC) {
		return nil, fmt.Errorf("resolve deadline must be after join deadline")
	}

	if err := services.LockBalance(ctx, input.CreatorEmail, input.StakeAmount, "USD"); err != nil {
		return nil, err
	}

	id := fmt.Sprintf("VS_%s", utils.RandomAlphanumeric(10))
	now := time.Now().UTC()
	event := &models.VSEvent{
		ID:                 id,
		Title:              input.Title,
		Description:        input.Description,
		CreatorEmail:       input.CreatorEmail,
		Mode:               input.Mode,
		Threshold:          input.Threshold,
		StakeAmount:        input.StakeAmount,
		ParticipantTarget:  input.ParticipantTarget,
		Status:             models.VSStatusOpen,
		ChainState:         "PENDING_CHAIN_CREATE",
		JoinDeadlineUTC:    input.JoinDeadlineUTC.UTC(),
		ResolveDeadlineUTC: input.ResolveDeadlineUTC.UTC(),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := db.SaveVSEvent(ctx, event); err != nil {
		_ = services.UnlockBalance(ctx, input.CreatorEmail, input.StakeAmount, "USD")
		return nil, err
	}

	creatorPart := &models.VSEventParticipant{
		ID:           fmt.Sprintf("VSP_%s", utils.RandomAlphanumeric(12)),
		VSEventID:    event.ID,
		UserEmail:    input.CreatorEmail,
		JoinedAt:     now,
		LockedAmount: input.StakeAmount,
	}
	if err := db.SaveVSEventParticipant(ctx, creatorPart); err != nil {
		return nil, err
	}

	tx, err := createVSEventOnchain(event.ID, input)
	if err != nil {
		log.Printf("[VS] create onchain failed for %s: %v", event.ID, err)
		_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), event.ID, "CHAIN_CREATE_FAILED")
		fresh, _ := db.GetVSEventByID(ctx, event.ID)
		if fresh != nil {
			return fresh, nil
		}
		return event, nil
	}
	if _, err := DelegateMarket(event.ID); err != nil {
		log.Printf("[VS] delegate failed for %s: %v", event.ID, err)
		_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), event.ID, "CHAIN_DELEGATE_FAILED")
		fresh, _ := db.GetVSEventByID(ctx, event.ID)
		if fresh != nil {
			return fresh, nil
		}
		return event, nil
	}
	_ = db.UpdateVSEventFields(context.Background(), event.ID, map[string]interface{}{"creation_tx_hash": tx})
	_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), event.ID, "CHAIN_CREATED")

	fresh, _ := db.GetVSEventByID(ctx, event.ID)
	if fresh != nil {
		return fresh, nil
	}
	return event, nil
}

func JoinVSEvent(ctx context.Context, eventID, userEmail string) (*models.VSEvent, error) {
	event, err := db.GetVSEventByID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if event.Status != models.VSStatusOpen && event.Status != models.VSStatusActive {
		return nil, fmt.Errorf("event is not joinable")
	}
	for _, p := range event.Participants {
		if p.UserEmail == userEmail {
			return nil, fmt.Errorf("already joined")
		}
	}

	if err := services.LockBalance(ctx, userEmail, event.StakeAmount, "USD"); err != nil {
		return nil, err
	}

	part := &models.VSEventParticipant{
		ID:           fmt.Sprintf("VSP_%s", utils.RandomAlphanumeric(12)),
		VSEventID:    event.ID,
		UserEmail:    userEmail,
		JoinedAt:     time.Now().UTC(),
		LockedAmount: event.StakeAmount,
	}
	if err := db.SaveVSEventParticipant(ctx, part); err != nil {
		return nil, err
	}

	participants, _ := db.GetVSEventParticipants(ctx, event.ID)
	status := event.Status
	if len(participants) >= event.ParticipantTarget {
		status = models.VSStatusActive
	}
	_ = db.UpdateVSEventFields(ctx, event.ID, map[string]interface{}{
		"status":      status,
		"chain_state": "PENDING_CHAIN_JOIN",
	})

	if isVSChainReadyForWrites(event.ChainState) {
		go func(evID, email string) {
			if _, err := joinVSEventOnchain(evID, email); err != nil {
				log.Printf("[VS] join onchain failed: event=%s user=%s err=%v", evID, email, err)
				_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), evID, "CHAIN_JOIN_FAILED")
				return
			}
			_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), evID, "CHAIN_JOINED")
		}(event.ID, userEmail)
	}

	return db.GetVSEventByID(ctx, event.ID)
}

func ConfirmVSEventOutcome(ctx context.Context, eventID, userEmail string, outcome models.VSOutcome) (*models.VSEvent, error) {
	event, err := db.GetVSEventByID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if outcome != models.VSOutcomeYes && outcome != models.VSOutcomeNo {
		return nil, fmt.Errorf("invalid outcome")
	}
	if event.Status != models.VSStatusActive && event.Status != models.VSStatusOpen {
		return nil, fmt.Errorf("event is not active")
	}

	now := time.Now().UTC()
	if err := db.UpdateVSEventParticipantConfirmation(ctx, event.ID, userEmail, string(outcome), now); err != nil {
		return nil, err
	}

	parts, _ := db.GetVSEventParticipants(ctx, event.ID)
	yesCount, noCount := 0, 0
	for _, p := range parts {
		switch p.Confirmation {
		case string(models.VSOutcomeYes):
			yesCount++
		case string(models.VSOutcomeNo):
			noCount++
		}
	}

	resolved, finalOutcome := computeVSResolution(event.Mode, event.ParticipantTarget, event.Threshold, yesCount, noCount)

	if resolved {
		status := models.VSStatusResolved
		resolvedAt := time.Now().UTC()
		_ = db.UpdateVSEventFields(ctx, event.ID, map[string]interface{}{
			"status":      status,
			"outcome":     finalOutcome,
			"resolved_at": resolvedAt,
			"chain_state": "PENDING_CHAIN_RESOLVE",
		})
		if err := settleVSEventLedger(ctx, event.ID, models.VSOutcome(finalOutcome)); err != nil {
			log.Printf("[VS] ledger settle failed for %s: %v", event.ID, err)
		}
		if isVSChainReadyForWrites(event.ChainState) {
			go func(evID, email string, out models.VSOutcome) {
				tx, err := confirmVSEventOnchain(evID, email, out)
				if err != nil {
					log.Printf("[VS] final confirm onchain failed for %s: %v", evID, err)
					_ = db.UpdateVSEventFields(context.Background(), evID, map[string]interface{}{"chain_state": "CHAIN_RESOLVE_FAILED"})
					return
				}
				_ = db.UpdateVSEventChainResolved(context.Background(), evID, tx)
			}(event.ID, userEmail, outcome)
		}
	} else {
		_ = db.UpdateVSEventFields(ctx, event.ID, map[string]interface{}{"chain_state": "PENDING_CHAIN_CONFIRM"})
		if isVSChainReadyForWrites(event.ChainState) {
			go func(evID, email string, out models.VSOutcome) {
				if _, err := confirmVSEventOnchain(evID, email, out); err != nil {
					log.Printf("[VS] confirm onchain failed for %s: %v", evID, err)
					_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), evID, "CHAIN_CONFIRM_FAILED")
					return
				}
				_ = db.UpdateVSEventChainStateIfNotTerminal(context.Background(), evID, "CHAIN_CONFIRMED")
			}(event.ID, userEmail, outcome)
		}
	}

	return db.GetVSEventByID(ctx, event.ID)
}

func computeVSResolution(mode models.VSMode, participantTarget, threshold, yesCount, noCount int) (bool, string) {
	if mode == models.VSModeMutual {
		if participantTarget == 2 && yesCount == 2 {
			return true, string(models.VSOutcomeYes)
		}
		if participantTarget == 2 && noCount == 2 {
			return true, string(models.VSOutcomeNo)
		}
		return false, ""
	}
	if threshold <= 0 {
		threshold = 1
	}
	if yesCount >= threshold {
		return true, string(models.VSOutcomeYes)
	}
	if noCount >= threshold {
		return true, string(models.VSOutcomeNo)
	}
	return false, ""
}

func CancelVSEvent(ctx context.Context, eventID, requester string) (*models.VSEvent, error) {
	event, err := db.GetVSEventByID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if event.CreatorEmail != requester {
		return nil, fmt.Errorf("only creator can cancel")
	}
	if event.Status == models.VSStatusResolved || event.Status == models.VSStatusCancelled {
		return nil, fmt.Errorf("already terminal")
	}

	for _, p := range event.Participants {
		_ = services.UnlockBalance(ctx, p.UserEmail, p.LockedAmount, "USD")
	}

	_ = db.UpdateVSEventFields(ctx, event.ID, map[string]interface{}{
		"status":      models.VSStatusCancelled,
		"chain_state": "PENDING_CHAIN_CANCEL",
	})
	go func(evID string) {
		if _, err := cancelVSEventOnchain(evID, event.CreatorEmail); err != nil {
			log.Printf("[VS] cancel onchain failed for %s: %v", evID, err)
			_ = db.UpdateVSEventFields(context.Background(), evID, map[string]interface{}{"chain_state": "CHAIN_CANCEL_FAILED"})
			return
		}
		_ = db.UpdateVSEventChainCancelled(context.Background(), evID)
	}(event.ID)
	return db.GetVSEventByID(ctx, event.ID)
}

func settleVSEventLedger(ctx context.Context, eventID string, outcome models.VSOutcome) error {
	event, err := db.GetVSEventByID(ctx, eventID)
	if err != nil {
		return err
	}
	parts := event.Participants
	if len(parts) == 0 {
		return nil
	}

	for _, p := range parts {
		if err := services.DeductLockedBalance(ctx, p.UserEmail, p.LockedAmount); err != nil {
			return err
		}
	}

	winner := ""
	for _, p := range parts {
		if p.Confirmation == string(outcome) {
			winner = p.UserEmail
			break
		}
	}
	if winner == "" {
		// no clear winner path: refund everyone
		for _, p := range parts {
			if err := services.CreditBalance(ctx, p.UserEmail, p.LockedAmount, "USD"); err != nil {
				return err
			}
		}
		return nil
	}

	pot := float64(len(parts)) * event.StakeAmount
	return services.CreditBalance(ctx, winner, pot, "USD")
}

func buildVSEventData(evID string, input CreateVSEventInput) []byte {
	mode := uint8(0)
	if input.Mode == models.VSModeConsensus {
		mode = 1
	}
	bufLen := 1 + stringLen(evID) + stringLen(input.Title) + 8 + 1 + 1 + 8 + 8 + 1
	buf := make([]byte, bufLen)
	o := 0
	writeU8(buf, &o, discriminatorCreateVS)
	writeString(buf, &o, evID)
	writeString(buf, &o, input.Title)
	writeU64(buf, &o, uint64(input.StakeAmount*100))
	writeU8(buf, &o, mode)
	writeU8(buf, &o, uint8(input.Threshold))
	writeU64(buf, &o, uint64(input.JoinDeadlineUTC.Unix()))
	writeU64(buf, &o, uint64(input.ResolveDeadlineUTC.Unix()))
	writeU8(buf, &o, uint8(input.ParticipantTarget))
	return buf
}

func createVSEventOnchain(eventID string, input CreateVSEventInput) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	creatorKey, err := getUserSolanaPrivateKey(input.CreatorEmail)
	if err != nil {
		return "", err
	}
	feePayerKey, err := getFeePayerSolanaPrivateKey()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(eventID)
	if err != nil {
		return "", err
	}
	data := buildVSEventData(eventID, input)
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: creatorKey.PublicKey(), IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}, data)
	return sendAndConfirm(getFallbackRPCURLs(), []solana.Instruction{ix}, []solana.PrivateKey{creatorKey, feePayerKey}, feePayerKey.PublicKey())
}

func joinVSEventOnchain(eventID, userEmail string) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	userKey, err := getUserSolanaPrivateKey(userEmail)
	if err != nil {
		return "", err
	}
	feePayerKey, err := getFeePayerSolanaPrivateKey()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(eventID)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 1+stringLen(eventID))
	o := 0
	writeU8(buf, &o, discriminatorJoinVS)
	writeString(buf, &o, eventID)
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: userKey.PublicKey(), IsSigner: true, IsWritable: false},
	}, buf)
	return sendVSInstructionPreferEphemeral(ix, []solana.PrivateKey{userKey, feePayerKey}, feePayerKey.PublicKey())
}

func confirmVSEventOnchain(eventID, userEmail string, outcome models.VSOutcome) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	userKey, err := getUserSolanaPrivateKey(userEmail)
	if err != nil {
		return "", err
	}
	feePayerKey, err := getFeePayerSolanaPrivateKey()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(eventID)
	if err != nil {
		return "", err
	}
	ov := uint8(0)
	if outcome == models.VSOutcomeYes {
		ov = 1
	}
	buf := make([]byte, 1+stringLen(eventID)+1)
	o := 0
	writeU8(buf, &o, discriminatorConfirmVS)
	writeString(buf, &o, eventID)
	writeU8(buf, &o, ov)
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: userKey.PublicKey(), IsSigner: true, IsWritable: false},
	}, buf)
	return sendVSInstructionPreferEphemeral(ix, []solana.PrivateKey{userKey, feePayerKey}, feePayerKey.PublicKey())
}

func resolveVSEventOnchain(eventID, creatorEmail string, outcome models.VSOutcome, desc string) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	creatorKey, err := getUserSolanaPrivateKey(creatorEmail)
	if err != nil {
		return "", err
	}
	feePayerKey, err := getFeePayerSolanaPrivateKey()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(eventID)
	if err != nil {
		return "", err
	}
	ov := uint8(0)
	if outcome == models.VSOutcomeYes {
		ov = 1
	}
	buf := make([]byte, 1+stringLen(eventID)+1+stringLen(desc))
	o := 0
	writeU8(buf, &o, discriminatorResolveVS)
	writeString(buf, &o, eventID)
	writeU8(buf, &o, ov)
	writeString(buf, &o, desc)
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: creatorKey.PublicKey(), IsSigner: true, IsWritable: false},
	}, buf)
	return sendVSInstructionPreferEphemeral(ix, []solana.PrivateKey{creatorKey, feePayerKey}, feePayerKey.PublicKey())
}

func cancelVSEventOnchain(eventID, creatorEmail string) (string, error) {
	programID, err := getProgramID()
	if err != nil {
		return "", err
	}
	creatorKey, err := getUserSolanaPrivateKey(creatorEmail)
	if err != nil {
		return "", err
	}
	feePayerKey, err := getFeePayerSolanaPrivateKey()
	if err != nil {
		return "", err
	}
	marketPDA, _, err := deriveMarketPDA(eventID)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 1+stringLen(eventID))
	o := 0
	writeU8(buf, &o, discriminatorCancelVS)
	writeString(buf, &o, eventID)
	ix := solana.NewInstruction(programID, solana.AccountMetaSlice{
		{PublicKey: marketPDA, IsSigner: false, IsWritable: true},
		{PublicKey: creatorKey.PublicKey(), IsSigner: true, IsWritable: false},
	}, buf)
	return sendVSInstructionPreferEphemeral(ix, []solana.PrivateKey{creatorKey, feePayerKey}, feePayerKey.PublicKey())
}

func sendVSInstructionPreferEphemeral(
	ix solana.Instruction,
	signers []solana.PrivateKey,
	feePayer solana.PublicKey,
) (string, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		sig, err := sendToEphemeral([]solana.Instruction{ix}, signers, feePayer)
		if err == nil {
			return sig, nil
		}
		lastErr = err
		time.Sleep(time.Duration(300*(i+1)) * time.Millisecond)
	}
	errText := lastErr.Error()
	if strings.Contains(errText, "InvalidWritableAccount") ||
		strings.Contains(errText, "Custom:4") ||
		strings.Contains(errText, "Custom:18") ||
		strings.Contains(errText, "UninitializedAccount") ||
		strings.Contains(errText, "MarketNotStarted") {
		return sendAndConfirm(getFallbackRPCURLs(), []solana.Instruction{ix}, signers, feePayer)
	}
	return "", lastErr
}

func isVSChainReadyForWrites(chainState string) bool {
	switch chainState {
	case "CHAIN_CREATED",
		"CHAIN_JOINED",
		"CHAIN_CONFIRMED",
		"PENDING_CHAIN_JOIN",
		"PENDING_CHAIN_CONFIRM",
		"PENDING_CHAIN_RESOLVE",
		"PENDING_CHAIN_CANCEL":
		return true
	default:
		return false
	}
}

func getUserSolanaPrivateKey(email string) (solana.PrivateKey, error) {
	wallet, err := db.GetWalletByEmail(context.Background(), email)
	if err != nil {
		return nil, fmt.Errorf("wallet lookup failed for %s: %w", email, err)
	}
	dec, err := services.Decrypt(wallet.SolPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt sol private key for %s: %w", email, err)
	}
	w, err := solana.WalletFromPrivateKeyBase58(dec)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sol private key for %s: %w", email, err)
	}
	return w.PrivateKey, nil
}

func getFeePayerSolanaPrivateKey() (solana.PrivateKey, error) {
	secret := os.Getenv("VANT_FEE_PAYER_SOLANA")
	if secret == "" {
		return nil, fmt.Errorf("VANT_FEE_PAYER_SOLANA not set")
	}
	w, err := solana.WalletFromPrivateKeyBase58(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse VANT_FEE_PAYER_SOLANA: %w", err)
	}
	return w.PrivateKey, nil
}
