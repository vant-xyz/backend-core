package models

import "time"

type VSMode string

type VSStatus string

type VSOutcome string

const (
	VSModeMutual    VSMode = "mutual"
	VSModeConsensus VSMode = "consensus"
)

const (
	VSStatusOpen      VSStatus = "open"
	VSStatusActive    VSStatus = "active"
	VSStatusResolved  VSStatus = "resolved"
	VSStatusCancelled VSStatus = "cancelled"
	VSStatusDisputed  VSStatus = "disputed"
)

const (
	VSOutcomeYes VSOutcome = "YES"
	VSOutcomeNo  VSOutcome = "NO"
)

type VSEvent struct {
	ID                 string               `json:"id"`
	Title              string               `json:"title"`
	Description        string               `json:"description"`
	CreatorEmail       string               `json:"creator_email"`
	IsDemo             bool                 `json:"is_demo"`
	Mode               VSMode               `json:"mode"`
	Threshold          int                  `json:"threshold"`
	StakeAmount        float64              `json:"stake_amount"`
	ParticipantTarget  int                  `json:"participant_target"`
	Status             VSStatus             `json:"status"`
	Outcome            VSOutcome            `json:"outcome,omitempty"`
	OutcomeDescription string               `json:"outcome_description,omitempty"`
	CreationTxHash     string               `json:"creation_tx_hash,omitempty"`
	SettlementTxHash   string               `json:"settlement_tx_hash,omitempty"`
	ChainState         string               `json:"chain_state"`
	JoinDeadlineUTC    time.Time            `json:"join_deadline_utc"`
	ResolveDeadlineUTC time.Time            `json:"resolve_deadline_utc"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
	ResolvedAt         *time.Time           `json:"resolved_at,omitempty"`
	Participants       []VSEventParticipant `json:"participants,omitempty"`
}

type VSEventParticipant struct {
	ID           string     `json:"id"`
	VSEventID    string     `json:"vs_event_id"`
	UserEmail    string     `json:"user_email"`
	JoinedAt     time.Time  `json:"joined_at"`
	LockedAmount float64    `json:"locked_amount"`
	Confirmation string     `json:"confirmation,omitempty"`
	ConfirmedAt  *time.Time `json:"confirmed_at,omitempty"`
}
