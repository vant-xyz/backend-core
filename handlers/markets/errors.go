package markets

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type normalizedAPIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	StatusCode int    `json:"-"`
}

func classifyMarketError(err error) normalizedAPIError {
	if err == nil {
		return normalizedAPIError{
			Code:       "UNKNOWN_ERROR",
			Message:    "An unexpected error occurred.",
			StatusCode: http.StatusBadRequest,
		}
	}

	raw := strings.ToLower(err.Error())

	switch {
	case strings.Contains(raw, "no liquidity"):
		return normalizedAPIError{
			Code:       "NO_LIQUIDITY",
			Message:    "No liquidity is available right now for this market order. Try a smaller size or a limit order.",
			Retryable:  true,
			StatusCode: http.StatusBadRequest,
		}
	case strings.Contains(raw, "insufficient balance"):
		return normalizedAPIError{
			Code:       "INSUFFICIENT_BALANCE",
			Message:    "Insufficient balance to complete this trade.",
			StatusCode: http.StatusBadRequest,
		}
	case strings.Contains(raw, "market not found"):
		return normalizedAPIError{
			Code:       "MARKET_NOT_FOUND",
			Message:    "This market was not found.",
			StatusCode: http.StatusNotFound,
		}
	case strings.Contains(raw, "not active"):
		return normalizedAPIError{
			Code:       "MARKET_INACTIVE",
			Message:    "This market is not active for trading.",
			StatusCode: http.StatusBadRequest,
		}
	default:
		return normalizedAPIError{
			Code:       "ORDER_REJECTED",
			Message:    "Unable to complete this request right now. Please try again.",
			StatusCode: http.StatusBadRequest,
		}
	}
}

func writeNormalizedMarketError(c *gin.Context, err error) {
	normalized := classifyMarketError(err)
	c.JSON(normalized.StatusCode, gin.H{
		"code":      normalized.Code,
		"message":   normalized.Message,
		"retryable": normalized.Retryable,
	})
}
