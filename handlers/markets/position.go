package markets

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func ClosePosition(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	positionID := c.Param("id")
	if positionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Position ID required"})
		return
	}

	var req struct {
		Shares float64 `json:"shares"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	if req.Shares <= 0 {
		if sharesStr := c.Query("shares"); sharesStr != "" {
			shares, err := strconv.ParseFloat(sharesStr, 64)
			if err == nil && shares > 0 {
				req.Shares = shares
			}
		}
	}

	position, proceeds, err := marketsvc.ClosePosition(c.Request.Context(), marketsvc.ClosePositionInput{
		PositionID: positionID,
		UserEmail:  userEmail,
		Shares:     req.Shares,
	})
	if err != nil {
		writeNormalizedMarketError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"position": position,
		"proceeds": proceeds,
	})
}
