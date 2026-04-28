package models

import "time"

type PositionStatus string

const (
	PositionStatusActive  PositionStatus = "ACTIVE"
	PositionStatusClosed  PositionStatus = "CLOSED"
	PositionStatusSettled PositionStatus = "SETTLED"
)

type Position struct {
	ID            string         `json:"id" firestore:"id"`
	UserEmail     string         `json:"user_email" firestore:"user_email"`
	MarketID      string         `json:"market_id" firestore:"market_id"`
	Side          OrderSide      `json:"side" firestore:"side"`
	Shares        float64        `json:"shares" firestore:"shares"`
	AvgEntryPrice float64        `json:"avg_entry_price" firestore:"avg_entry_price"`
	RealizedPnL   float64        `json:"realized_pnl" firestore:"realized_pnl"`
	PayoutAmount  float64        `json:"payout_amount" firestore:"payout_amount"`
	Status        PositionStatus `json:"status" firestore:"status"`
	QuoteCurrency string         `json:"quote_currency" firestore:"quote_currency"`
	IsDemo        bool           `json:"is_demo" firestore:"is_demo" db:"is_demo"`
	CreatedAt     time.Time      `json:"created_at" firestore:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at" firestore:"updated_at"`
	SettledAt     *time.Time     `json:"settled_at,omitempty" firestore:"settled_at"`
}
