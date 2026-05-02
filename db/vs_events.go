package db

import (
	"context"
	"fmt"
	"time"

	"github.com/vant-xyz/backend-code/models"
)

func SaveVSEvent(ctx context.Context, e *models.VSEvent) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO vs_events (
			id,title,description,creator_email,mode,threshold,stake_amount,participant_target,
			status,outcome,outcome_description,creation_tx_hash,settlement_tx_hash,chain_state,
			join_deadline_utc,resolve_deadline_utc,created_at,updated_at,resolved_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19
		)
	`, e.ID, e.Title, e.Description, e.CreatorEmail, string(e.Mode), e.Threshold, e.StakeAmount,
		e.ParticipantTarget, string(e.Status), string(e.Outcome), e.OutcomeDescription, e.CreationTxHash,
		e.SettlementTxHash, e.ChainState, e.JoinDeadlineUTC, e.ResolveDeadlineUTC, e.CreatedAt, e.UpdatedAt, e.ResolvedAt)
	return err
}

func UpdateVSEventChainStateIfNotTerminal(ctx context.Context, eventID, newState string) error {
	_, err := Pool.Exec(ctx, `
		UPDATE vs_events
		SET chain_state = $1, updated_at = $2
		WHERE id = $3
		  AND chain_state NOT IN (
		    'CHAIN_RESOLVED',
		    'CHAIN_CANCELLED',
		    'PENDING_CHAIN_RESOLVE',
		    'PENDING_CHAIN_CANCEL',
		    'CHAIN_CREATE_FAILED',
		    'CHAIN_DELEGATE_FAILED',
		    'CHAIN_JOIN_FAILED',
		    'CHAIN_CONFIRM_FAILED',
		    'CHAIN_RESOLVE_FAILED',
		    'CHAIN_CANCEL_FAILED'
		  )
	`, newState, time.Now().UTC(), eventID)
	return err
}

func UpdateVSEventChainResolved(ctx context.Context, eventID, txHash string) error {
	_, err := Pool.Exec(ctx, `
		UPDATE vs_events
		SET settlement_tx_hash = $1, chain_state = 'CHAIN_RESOLVED', updated_at = $2
		WHERE id = $3
	`, txHash, time.Now().UTC(), eventID)
	return err
}

func UpdateVSEventChainCancelled(ctx context.Context, eventID string) error {
	_, err := Pool.Exec(ctx, `
		UPDATE vs_events
		SET chain_state = 'CHAIN_CANCELLED', updated_at = $1
		WHERE id = $2
	`, time.Now().UTC(), eventID)
	return err
}

func SaveVSEventParticipant(ctx context.Context, p *models.VSEventParticipant) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO vs_event_participants (id,vs_event_id,user_email,joined_at,locked_amount,confirmation,confirmed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, p.ID, p.VSEventID, p.UserEmail, p.JoinedAt, p.LockedAmount, p.Confirmation, p.ConfirmedAt)
	return err
}

func GetVSEventByID(ctx context.Context, id string) (*models.VSEvent, error) {
	row := Pool.QueryRow(ctx, `
		SELECT id,title,description,creator_email,mode,threshold,stake_amount,participant_target,
		status,outcome,outcome_description,creation_tx_hash,settlement_tx_hash,chain_state,
		join_deadline_utc,resolve_deadline_utc,created_at,updated_at,resolved_at
		FROM vs_events WHERE id=$1
	`, id)
	var e models.VSEvent
	var mode, status, outcome string
	if err := row.Scan(&e.ID, &e.Title, &e.Description, &e.CreatorEmail, &mode, &e.Threshold, &e.StakeAmount,
		&e.ParticipantTarget, &status, &outcome, &e.OutcomeDescription, &e.CreationTxHash, &e.SettlementTxHash,
		&e.ChainState, &e.JoinDeadlineUTC, &e.ResolveDeadlineUTC, &e.CreatedAt, &e.UpdatedAt, &e.ResolvedAt); err != nil {
		return nil, err
	}
	e.Mode = models.VSMode(mode)
	e.Status = models.VSStatus(status)
	e.Outcome = models.VSOutcome(outcome)
	parts, err := GetVSEventParticipants(ctx, id)
	if err != nil {
		return nil, err
	}
	e.Participants = parts
	return &e, nil
}

func GetVSEventParticipants(ctx context.Context, eventID string) ([]models.VSEventParticipant, error) {
	rows, err := Pool.Query(ctx, `
		SELECT id,vs_event_id,user_email,joined_at,locked_amount,confirmation,confirmed_at
		FROM vs_event_participants WHERE vs_event_id=$1 ORDER BY joined_at ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.VSEventParticipant{}
	for rows.Next() {
		var p models.VSEventParticipant
		if err := rows.Scan(&p.ID, &p.VSEventID, &p.UserEmail, &p.JoinedAt, &p.LockedAmount, &p.Confirmation, &p.ConfirmedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func ListVSEvents(ctx context.Context, status string, limit int) ([]models.VSEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows any
	_ = rows
	query := `
		SELECT id,title,description,creator_email,mode,threshold,stake_amount,participant_target,
		status,outcome,outcome_description,creation_tx_hash,settlement_tx_hash,chain_state,
		join_deadline_utc,resolve_deadline_utc,created_at,updated_at,resolved_at
		FROM vs_events`
	args := []interface{}{}
	if status != "" {
		query += ` WHERE status=$1 ORDER BY created_at DESC LIMIT $2`
		args = append(args, status, limit)
	} else {
		query += ` ORDER BY created_at DESC LIMIT $1`
		args = append(args, limit)
	}
	pgRows, err := Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()
	out := []models.VSEvent{}
	for pgRows.Next() {
		var e models.VSEvent
		var mode, st, oc string
		if err := pgRows.Scan(&e.ID, &e.Title, &e.Description, &e.CreatorEmail, &mode, &e.Threshold, &e.StakeAmount,
			&e.ParticipantTarget, &st, &oc, &e.OutcomeDescription, &e.CreationTxHash, &e.SettlementTxHash,
			&e.ChainState, &e.JoinDeadlineUTC, &e.ResolveDeadlineUTC, &e.CreatedAt, &e.UpdatedAt, &e.ResolvedAt); err != nil {
			return nil, err
		}
		e.Mode = models.VSMode(mode)
		e.Status = models.VSStatus(st)
		e.Outcome = models.VSOutcome(oc)
		out = append(out, e)
	}
	return out, pgRows.Err()
}

func UpdateVSEventFields(ctx context.Context, eventID string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	i := 1
	set := ""
	args := []interface{}{}
	for k, v := range fields {
		if set != "" {
			set += ", "
		}
		set += fmt.Sprintf("%s = $%d", k, i)
		args = append(args, v)
		i++
	}
	set += fmt.Sprintf(", updated_at = $%d", i)
	args = append(args, time.Now().UTC())
	i++
	args = append(args, eventID)
	_, err := Pool.Exec(ctx, fmt.Sprintf("UPDATE vs_events SET %s WHERE id = $%d", set, i), args...)
	return err
}

func UpdateVSEventParticipantConfirmation(ctx context.Context, eventID, email, confirmation string, confirmedAt time.Time) error {
	_, err := Pool.Exec(ctx, `
		UPDATE vs_event_participants
		SET confirmation=$1, confirmed_at=$2
		WHERE vs_event_id=$3 AND user_email=$4
	`, confirmation, confirmedAt, eventID, email)
	return err
}
