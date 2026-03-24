package markets

import (
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type OrderbookUpdate struct {
	MarketID string      `json:"market_id"`
	Type     string      `json:"type"`
	Data     interface{} `json:"data"`
}

type orderbookClient struct {
	conn     *websocket.Conn
	email    string
	marketID string
	send     chan OrderbookUpdate
}

type OrderbookHub struct {
	mu         sync.RWMutex
	clients    map[*orderbookClient]bool
	register   chan *orderbookClient
	unregister chan *orderbookClient
	broadcast  chan OrderbookUpdate
}

var (
	orderbookHubOnce sync.Once
	globalOrderbookHub *OrderbookHub
)

func GetOrderbookHub() *OrderbookHub {
	orderbookHubOnce.Do(func() {
		globalOrderbookHub = &OrderbookHub{
			clients:    make(map[*orderbookClient]bool),
			register:   make(chan *orderbookClient, 64),
			unregister: make(chan *orderbookClient, 64),
			broadcast:  make(chan OrderbookUpdate, 512),
		}
		go globalOrderbookHub.run()
	})
	return globalOrderbookHub
}

func (h *OrderbookHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[OrderbookHub] Client registered: email=%s market=%s", client.email, client.marketID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[OrderbookHub] Client unregistered: email=%s market=%s", client.email, client.marketID)

		case update := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				if client.marketID != update.MarketID {
					continue
				}
				select {
				case client.send <- update:
				default:
					// Client send buffer full — drop update rather than block the hub.
					log.Printf("[OrderbookHub] Dropped update for slow client: email=%s market=%s",
						client.email, client.marketID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// BroadcastToMarket sends an update to all clients subscribed to a market.
func (h *OrderbookHub) BroadcastToMarket(marketID string, update OrderbookUpdate) {
	h.broadcast <- update
}

// BroadcastToUser sends an update to all connections for a specific user
// regardless of which market they are subscribed to.
func (h *OrderbookHub) BroadcastToUser(email string, update OrderbookUpdate) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client.email == email {
			select {
			case client.send <- update:
			default:
				log.Printf("[OrderbookHub] Dropped user update for slow client: email=%s", email)
			}
		}
	}
}

// BroadcastOrderbookUpdate is the function called by the matching engine and
// settlement service to push updates without needing a hub reference.
func BroadcastOrderbookUpdate(marketID, updateType string, data interface{}) {
	GetOrderbookHub().BroadcastToMarket(marketID, OrderbookUpdate{
		MarketID: marketID,
		Type:     updateType,
		Data:     data,
	})
}

// HandleOrderbookWS upgrades the connection and registers the client with the
// hub. The marketID is read from the URL param so clients subscribe per market.
func HandleOrderbookWS(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	conn, err := orderbookUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[OrderbookHub] WebSocket upgrade failed for %s: %v", userEmail, err)
		return
	}

	client := &orderbookClient{
		conn:     conn,
		email:    userEmail,
		marketID: marketID,
		send:     make(chan OrderbookUpdate, 64),
	}

	hub := GetOrderbookHub()
	hub.register <- client

	go client.writePump(hub)
	client.readPump(hub)
}

// writePump drains the client's send channel and writes to the WebSocket.
func (c *orderbookClient) writePump(hub *OrderbookHub) {
	defer func() {
		c.conn.Close()
	}()

	for update := range c.send {
		if err := c.conn.WriteJSON(update); err != nil {
			log.Printf("[OrderbookHub] Write error for %s: %v", c.email, err)
			return
		}
	}
}

// readPump reads from the WebSocket to detect disconnects.
// We don't expect client messages on this channel — it is server-push only.
func (c *orderbookClient) readPump(hub *OrderbookHub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived,
			) {
				log.Printf("[OrderbookHub] Unexpected close for %s: %v", c.email, err)
			}
			break
		}
	}
}

var orderbookUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		allowed := []string{
			"https://vantic.xyz",
			"http://localhost:3000",
		}
		for _, a := range allowed {
			if origin == a {
				return true
			}
		}
		if origin == "" || strings.HasPrefix(origin, "http://localhost:") {
			return true
		}
		return false
	},
}