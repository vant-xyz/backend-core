package handlers

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/services/jupiter"
)

// passthroughGet calls Jupiter and writes the raw response body to the client.
func passthroughGet(c *gin.Context, path string, params url.Values) {
	body, status, err := jupiter.Get(c.Request.Context(), path, params)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "upstream error: " + err.Error()})
		return
	}
	c.Data(status, "application/json; charset=utf-8", body)
}

// mergeQuery copies query params from the request into dst, skipping keys already set.
func mergeQuery(dst url.Values, c *gin.Context, keys ...string) {
	for _, k := range keys {
		if dst.Get(k) == "" {
			if v := c.Query(k); v != "" {
				dst.Set(k, v)
			}
		}
	}
}

// GetEvents proxies GET /v2/events → Jupiter GET /events
// Supported params: provider, includeMarkets, includeAllMarkets, start, end,
//
//	category, subcategory, sortBy, sortDirection, filter, tags
func GetEvents(c *gin.Context) {
	params := url.Values{}
	mergeQuery(params, c,
		"provider", "includeMarkets", "includeAllMarkets",
		"start", "end", "limit", "category", "subcategory",
		"sortBy", "sortDirection", "filter", "tags",
	)
	passthroughGet(c, "/events", params)
}

// GetWorldCupEvents is a convenience route that pre-fills the World Cup filter.
// GET /v2/events/worldcup — proxied as GET /events?category=sports&tags=soccer&includeMarkets=true
// Callers can still pass sortBy, sortDirection, filter, start, end.
func GetWorldCupEvents(c *gin.Context) {
	params := url.Values{
		"category":       {"sports"},
		"tags":           {"soccer"},
		"includeMarkets": {"true"},
	}
	mergeQuery(params, c, "provider", "includeAllMarkets", "sortBy", "sortDirection", "filter", "start", "end")
	passthroughGet(c, "/events", params)
}

// SearchEvents proxies GET /v2/events/search → Jupiter GET /events/search
// Required param: query. Optional: provider, limit.
func SearchEvents(c *gin.Context) {
	params := url.Values{}
	mergeQuery(params, c, "query", "provider", "limit")
	if params.Get("query") == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "query param required"})
		return
	}
	passthroughGet(c, "/events/search", params)
}

// GetEventScores proxies GET /v2/events/scores → Jupiter GET /events/scores
// Required param: eventIds (comma-separated, max 100)
func GetEventScores(c *gin.Context) {
	params := url.Values{}
	mergeQuery(params, c, "eventIds")
	if params.Get("eventIds") == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "eventIds param required"})
		return
	}
	passthroughGet(c, "/events/scores", params)
}

// GetEvent proxies GET /v2/events/:id → Jupiter GET /events/:id
func GetEvent(c *gin.Context) {
	id := c.Param("id")
	params := url.Values{}
	mergeQuery(params, c, "includeMarkets", "includeAllMarkets")
	passthroughGet(c, "/events/"+id, params)
}

// GetEventScore proxies GET /v2/events/:id/score → Jupiter GET /events/:id/score
func GetEventScore(c *gin.Context) {
	id := c.Param("id")
	passthroughGet(c, "/events/"+id+"/score", nil)
}
