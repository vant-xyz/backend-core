package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/services"
)

func GetUserProfile(c *gin.Context) {
	email, _ := c.Get("email")
	
	user, err := db.GetUserByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}

	wallet, _ := db.GetWalletByEmail(c.Request.Context(), email.(string))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"user":    user,
		"wallet":  wallet,
	})
}

func UpdateUserProfile(c *gin.Context) {
	email, _ := c.Get("email")
	
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}

	allowedUpdates := map[string]bool{
		"username":          true,
		"socials":           true,
		"profile_image_url": true,
		"full_name":         true,
	}

	updates := make(map[string]interface{})
	for k, v := range req {
		if allowedUpdates[k] {
			updates[k] = v
		}
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "No valid fields to update"})
		return
	}

	err := db.UpdateUser(c.Request.Context(), email.(string), updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	updatedUser, _ := db.GetUserByEmail(c.Request.Context(), email.(string))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Profile updated successfully",
		"user":    updatedUser,
	})
}

func UploadProfileImage(c *gin.Context) {
	email, _ := c.Get("email")

	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "No image uploaded"})
		return
	}

	openedFile, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Error opening file"})
		return
	}
	defer openedFile.Close()

	publicID := "profile_" + email.(string)
	url, err := services.UploadImage(c.Request.Context(), openedFile, publicID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Upload failed: " + err.Error()})
		return
	}

	updates := map[string]interface{}{
		"profile_image_url": url,
	}
	err = db.UpdateUser(c.Request.Context(), email.(string), updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to update profile with image URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"message":           "Image uploaded successfully",
		"profile_image_url": url,
	})
}
