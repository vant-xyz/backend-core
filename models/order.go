package models

import "time"

type OrderSide string

const (
	OrderSideYes OrderSide = "YES"
	OrderSideNo  OrderSide = "NO"
)

type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
)

type OrderStatus string

const (
	OrderStatusOpen            OrderStatus = "OPEN"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCancelled       OrderStatus = "CANCELLED"
)

type Order struct {
	ID            string      `json:"id" firestore:"id"`
	UserEmail     string      `json:"user_email" firestore:"user_email"`
	MarketID      string      `json:"market_id" firestore:"market_id"`
	Side          OrderSide   `json:"side" firestore:"side"`
	Type          OrderType   `json:"type" firestore:"type"`
	Price         float64     `json:"price" firestore:"price"`
	Quantity      float64     `json:"quantity" firestore:"quantity"`
	FilledQty     float64     `json:"filled_qty" firestore:"filled_qty"`
	RemainingQty  float64     `json:"remaining_qty" firestore:"remaining_qty"`
	Status        OrderStatus `json:"status" firestore:"status"`
	QuoteCurrency string      `json:"quote_currency" firestore:"quote_currency"`
	IsDemo        bool        `json:"is_demo" firestore:"is_demo" db:"is_demo"`
	CreatedAt     time.Time   `json:"created_at" firestore:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at" firestore:"updated_at"`
	ExpiresAt     *time.Time  `json:"expires_at,omitempty" firestore:"expires_at"`
}
