package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func APIKeyMiddleware() gin.HandlerFunc {
	masterKey := os.Getenv("API_MASTER_KEY")
	if masterKey == "" {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/health" || strings.HasPrefix(path, "/docs") {
			c.Next()
			return
		}
		if c.GetHeader("X-API-Key") != masterKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}
		c.Next()
	}
}

// IndexerKeyMiddleware guards /internal/* routes with a separate secret so
// the public API key cannot be used to credit balances directly.
func IndexerKeyMiddleware() gin.HandlerFunc {
	indexerKey := os.Getenv("INDEXER_SECRET_KEY")
	return func(c *gin.Context) {
		if indexerKey == "" || c.GetHeader("X-Indexer-Key") != indexerKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}
		c.Next()
	}
}
