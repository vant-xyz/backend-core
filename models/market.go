package models

import "time"

type MarketType string

const (
	MarketTypeCAPPM MarketType = "CAPPM"
	MarketTypeGEM   MarketType = "GEM"
)

type MarketDirection string

const (
	DirectionAbove MarketDirection = "Above"
	DirectionBelow MarketDirection = "Below"
)

type MarketOutcome string

const (
	OutcomeYes MarketOutcome = "YES"
	OutcomeNo  MarketOutcome = "NO"
)

type MarketStatus string

const (
	MarketStatusActive   MarketStatus = "active"
	MarketStatusResolved MarketStatus = "resolved"
)

type Market struct {
	ID              string          `json:"id" firestore:"id"`
	MarketType      MarketType      `json:"market_type" firestore:"market_type"`
	Status          MarketStatus    `json:"status" firestore:"status"`
	QuoteCurrency   string          `json:"quote_currency" firestore:"quote_currency"`
	Title           string          `json:"title" firestore:"title"`
	Description     string          `json:"description" firestore:"description"`
	DataProvider    string          `json:"data_provider" firestore:"data_provider"`
	CreatorAddress  string          `json:"creator_address" firestore:"creator_address"`
	MarketPDA       string          `json:"market_pda" firestore:"market_pda"`
	StartTimeUTC    time.Time       `json:"start_time_utc" firestore:"start_time_utc"`
	EndTimeUTC      time.Time       `json:"end_time_utc" firestore:"end_time_utc"`
	DurationSeconds uint64          `json:"duration_seconds" firestore:"duration_seconds"`
	CreatedAt       time.Time       `json:"created_at" firestore:"created_at"`
	CreationTxHash  string          `json:"creation_tx_hash" firestore:"creation_tx_hash"`

	Asset        string          `json:"asset,omitempty" firestore:"asset"`
	Direction    MarketDirection `json:"direction,omitempty" firestore:"direction"`
	TargetPrice  uint64          `json:"target_price,omitempty" firestore:"target_price"`
	CurrentPrice uint64          `json:"current_price,omitempty" firestore:"current_price"`

	Outcome            MarketOutcome `json:"outcome,omitempty" firestore:"outcome"`
	OutcomeDescription string        `json:"outcome_description,omitempty" firestore:"outcome_description"`
	EndPrice           uint64        `json:"end_price,omitempty" firestore:"end_price"`
	SettlementTxHash   string        `json:"settlement_tx_hash,omitempty" firestore:"settlement_tx_hash"`
	ResolvedAt         *time.Time    `json:"resolved_at,omitempty" firestore:"resolved_at"`

	AssetImage         string `json:"asset_image,omitempty" firestore:"asset_image"`
	MarketImageSmall   string `json:"market_image_small,omitempty" firestore:"market_image_small"`
	MarketImageBanner  string `json:"market_image_banner,omitempty" firestore:"market_image_banner"`

	Category string `json:"category" firestore:"category"`
}