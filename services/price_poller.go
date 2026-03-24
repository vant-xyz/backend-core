package services

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

type Client struct {
	conn  *websocket.Conn
	email string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		allowedOrigins := []string{
			"https://vantic.xyz",
			"http://localhost:3000",
		}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
		}
		if origin == "" {
			return true
		}
		if strings.HasPrefix(origin, "http://localhost:") {
			return true
		}
		return false
	},
}

type PriceData struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
	Time   int64  `json:"time"`
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan PriceData
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan PriceData),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.conn.Close()
			}
			h.mu.Unlock()
		case priceData := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				err := client.conn.WriteJSON(priceData)
				if err != nil {
					log.Printf("error: %v", err)
					client.conn.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// BroadcastToUser sends a typed message to all connections for a specific user.
// For balance updates, use BroadcastBalanceUpdate instead — it fetches and
// sends the full balance object so the frontend doesn't need to re-fetch.
func (h *Hub) BroadcastToUser(email, messageType string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.email == email {
			client.conn.WriteJSON(gin.H{"type": messageType})
		}
	}
}

// BroadcastBalanceUpdate fetches the latest balance for a user and pushes
// the full balance object over the WebSocket connection. The frontend receives
// a typed message and can update its state without an additional REST call.
func (h *Hub) BroadcastBalanceUpdate(email string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	balance, err := db.GetBalanceByEmail(ctx, email)
	if err != nil {
		log.Printf("[WS] Failed to fetch balance for broadcast to %s: %v", email, err)
		h.BroadcastToUser(email, "BALANCE_UPDATE")
		return
	}

	realNaira, demoNaira := ResolveNairaBalances(balance)
	balance.TotalNaira = realNaira
	balance.TotalDemoNaira = demoNaira

	payload := gin.H{
		"type":    "BALANCE_UPDATE",
		"balance": balance,
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.email == email {
			client.conn.WriteJSON(payload)
		}
	}
}

var (
	PriceHub     *Hub
	latestPrices = make(map[string]PriceData)
	priceMu      sync.RWMutex
)

func StartPricePoller() {
	PriceHub = NewHub()
	go PriceHub.Run()

	symbols := []string{"BTC-USD", "ETH-USD", "SOL-USD", "USD-NGN"}

	go func() {
		for {
			for _, symbol := range symbols {
				price, err := fetchPrice(symbol)
				if err != nil {
					log.Printf("Price Poller Warning: Could not fetch %s: %v", symbol, err)
					continue
				}

				data := PriceData{
					Symbol: symbol,
					Price:  price,
					Time:   time.Now().Unix(),
				}

				priceMu.Lock()
				latestPrices[symbol] = data
				priceMu.Unlock()

				PriceHub.broadcast <- data
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

func GetLatestPrices() map[string]PriceData {
	priceMu.RLock()
	defer priceMu.RUnlock()

	copyMap := make(map[string]PriceData)
	for k, v := range latestPrices {
		copyMap[k] = v
	}
	return copyMap
}

func fetchPrice(symbol string) (string, error) {
	resp, err := http.Get("https://api.coinbase.com/v2/prices/" + symbol + "/spot")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", http.ErrHandlerTimeout
	}

	var result struct {
		Data struct {
			Amount string `json:"amount"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Data.Amount, nil
}

func HandlePriceWS(w http.ResponseWriter, r *http.Request, email string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}

	// Send current prices immediately on connect so frontend doesn't wait 5s
	prices := GetLatestPrices()
	for _, p := range prices {
		conn.WriteJSON(p)
	}

	client := &Client{conn: conn, email: email}
	PriceHub.register <- client
}

// SendBalanceUpdateToUser is called from handlers after any balance-mutating
// operation (sell, fund, order fill, payout). It triggers a full balance push
// over the user's active WebSocket connection.
func SendBalanceUpdateToUser(email string) {
	if PriceHub == nil {
		return
	}
	go PriceHub.BroadcastBalanceUpdate(email)
}

// WS message types sent to the frontend:
//
//	PriceData          — live asset price update every 5 seconds
//	BALANCE_UPDATE     — full balance object after any balance mutation
//	ORDER_UPDATE       — order status changed (filled, cancelled)
//	POSITION_UPDATE    — position created or settled
//
// Frontend should switch on the "type" field. PriceData has no "type" field —
// it has "symbol", "price", "time" instead.

// ResolveNairaBalancesExported is the exported version used by the WS broadcast.
// Delegates to the existing currency service function.
func ResolveNairaBalancesExported(balance *models.Balance) (float64, float64) {
	return ResolveNairaBalances(balance)
}