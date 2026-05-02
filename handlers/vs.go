package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func CreateVSEvent(c *gin.Context) {
	email := c.GetString("email")
	var req struct {
		Title              string  `json:"title" binding:"required"`
		Description        string  `json:"description"`
		Mode               string  `json:"mode" binding:"required"`
		Threshold          int     `json:"threshold"`
		StakeAmount        float64 `json:"stake_amount" binding:"required"`
		ParticipantTarget  int     `json:"participant_target" binding:"required"`
		JoinDeadlineUTC    int64   `json:"join_deadline_utc" binding:"required"`
		ResolveDeadlineUTC int64   `json:"resolve_deadline_utc" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}
	event, err := marketsvc.CreateVSEvent(c.Request.Context(), marketsvc.CreateVSEventInput{
		Title:              req.Title,
		Description:        req.Description,
		CreatorEmail:       email,
		Mode:               models.VSMode(req.Mode),
		Threshold:          req.Threshold,
		StakeAmount:        req.StakeAmount,
		ParticipantTarget:  req.ParticipantTarget,
		JoinDeadlineUTC:    time.Unix(req.JoinDeadlineUTC, 0).UTC(),
		ResolveDeadlineUTC: time.Unix(req.ResolveDeadlineUTC, 0).UTC(),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "event": event})
}

func JoinVSEvent(c *gin.Context) {
	email := c.GetString("email")
	eventID := c.Param("id")
	event, err := marketsvc.JoinVSEvent(c.Request.Context(), eventID, email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "event": event})
}

func ConfirmVSEvent(c *gin.Context) {
	email := c.GetString("email")
	eventID := c.Param("id")
	var req struct {
		Outcome string `json:"outcome" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}
	event, err := marketsvc.ConfirmVSEventOutcome(c.Request.Context(), eventID, email, models.VSOutcome(req.Outcome))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "event": event})
}

func CancelVSEvent(c *gin.Context) {
	email := c.GetString("email")
	eventID := c.Param("id")
	event, err := marketsvc.CancelVSEvent(c.Request.Context(), eventID, email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "event": event})
}

func GetVSEvent(c *gin.Context) {
	eventID := c.Param("id")
	event, err := db.GetVSEventByID(c.Request.Context(), eventID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Event not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "event": event})
}

func ListVSEvents(c *gin.Context) {
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	events, err := db.ListVSEvents(c.Request.Context(), status, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "events": events, "count": len(events)})
}
