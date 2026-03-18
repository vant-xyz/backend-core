package services

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		allowedOrigins := []string{
			"https://usevant.xyz",
			"https://vant.davidnzube.xyz",
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

		if strings.HasPrefix(origin, "http://localhost:3000") {
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
	clients    map[*websocket.Conn]bool
	broadcast  chan PriceData
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan PriceData),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
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
				client.Close()
			}
			h.mu.Unlock()
		case priceData := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				err := client.WriteJSON(priceData)
				if err != nil {
					log.Printf("error: %v", err)
					client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

var (
	PriceHub    *Hub
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

func HandlePriceWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	PriceHub.register <- conn
}
