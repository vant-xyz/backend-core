package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
)

type pricePoint struct {
	T        int64 `json:"t"`        // unix seconds
	YesPrice int64 `json:"yesPrice"` // micro-USD
	NoPrice  int64 `json:"noPrice"`
}

type marketHistory struct {
	MarketID string       `json:"marketId"`
	Title    string       `json:"title"`
	Data     []pricePoint `json:"data"`
}

type priceHistoryResponse struct {
	EventID string          `json:"eventId"`
	Markets []marketHistory `json:"markets"`
}

// GetEventPriceHistory handles GET /v2/events/:id/price-history
// Optional query param: range = 1d | 1w | 1m | all (default: all)
func GetEventPriceHistory(c *gin.Context) {
	eventID := c.Param("id")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "event id required"})
		return
	}

	since := sinceFromRange(c.Query("range"))

	rows, err := db.GetEventPriceHistory(c.Request.Context(), eventID, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "db error: " + err.Error()})
		return
	}

	// Group by marketId, preserving insertion order of first appearance.
	order := []string{}
	byMarket := map[string]*marketHistory{}

	for _, s := range rows {
		if _, exists := byMarket[s.MarketID]; !exists {
			order = append(order, s.MarketID)
			byMarket[s.MarketID] = &marketHistory{
				MarketID: s.MarketID,
				Title:    s.MarketTitle,
				Data:     []pricePoint{},
			}
		}
		byMarket[s.MarketID].Data = append(byMarket[s.MarketID].Data, pricePoint{
			T:        s.RecordedAt.Unix(),
			YesPrice: s.YesPrice,
			NoPrice:  s.NoPrice,
		})
	}

	markets := make([]marketHistory, 0, len(order))
	for _, id := range order {
		markets = append(markets, *byMarket[id])
	}

	c.JSON(http.StatusOK, priceHistoryResponse{
		EventID: eventID,
		Markets: markets,
	})
}

func sinceFromRange(r string) time.Time {
	now := time.Now()
	switch r {
	case "1d":
		return now.Add(-24 * time.Hour)
	case "1w":
		return now.Add(-7 * 24 * time.Hour)
	case "1m":
		return now.Add(-30 * 24 * time.Hour)
	default:
		return time.Time{} // zero = all history
	}
}
