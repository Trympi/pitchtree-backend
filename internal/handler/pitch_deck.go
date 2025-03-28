package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"pitch-deck-generator/internal/model"
	"pitch-deck-generator/internal/progress"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type PitchDeckHandler struct {
	service  model.PitchDeckService
	progress *progress.Tracker
}

func NewPitchDeckHandler(service model.PitchDeckService, progress *progress.Tracker) *PitchDeckHandler {
	return &PitchDeckHandler{
		service:  service,
		progress: progress,
	}
}

func (h *PitchDeckHandler) Create(c *gin.Context) {
	var data model.PitchDeckData
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found"})
		return
	}

	deckInfo, err := h.service.Create(data, userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pitch deck generation started",
		"deckId":  deckInfo.ID,
	})
}

func (h *PitchDeckHandler) Get(c *gin.Context) {
	deckID := c.Param("deckId")
	deckInfo, err := h.service.Get(deckID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deck not found"})
		return
	}

	c.JSON(http.StatusOK, deckInfo)
}

func (h *PitchDeckHandler) UpdateVisibility(c *gin.Context) {
	deckID := c.Param("deckId")
	userID, _ := c.Get("userID")

	var req struct {
		IsPublic bool `json:"isPublic"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.service.UpdateVisibility(deckID, userID.(string), req.IsPublic)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Visibility updated successfully",
	})
}

func (h *PitchDeckHandler) ListUserDecks(c *gin.Context) {
	userID, _ := c.Get("userID")
	decks, err := h.service.ListUserDecks(userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"decks": decks,
	})
}

func (h *PitchDeckHandler) UploadImage(c *gin.Context) {
	// Get the file from the request

	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Create uploads directory if it doesn't exist
	if err := os.MkdirAll("uploads", os.ModePerm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upload directory"})
		return
	}

	// Generate unique filename
	ext := filepath.Ext(file.Filename)
	newFileName := uuid.New().String() + ext
	filePath := filepath.Join("uploads", newFileName)

	// Save the file
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}

	// Upload to storage
	url, err := h.service.UploadImage(filePath)
	if err != nil {
		// Clean up local file
		os.Remove(filePath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file"})
		return
	}

	// Clean up local file
	os.Remove(filePath)

	c.JSON(http.StatusOK, gin.H{
		"url": url,
	})
}

func (h *PitchDeckHandler) GetProgress(c *gin.Context) {
	deckID := c.Param("deckId")
	token := c.Query("token") // Get token from query parameter

	// Validate token and get userID
	userID, err := validateToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}

	// Get progress channel
	ch, exists := h.progress.GetChannel(deckID, userID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No progress found for this deck"})
		return
	}

	// Set headers for SSE
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Stream progress updates
	for update := range ch {
		c.SSEvent("message", update)
		c.Writer.Flush()
	}
}

// Add this helper function
func validateToken(tokenString string) (string, error) {
	jwtSecret := os.Getenv("SUPABASE_JWT_SECRET")

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if userID, ok := claims["sub"].(string); ok {
			return userID, nil
		}
	}
	return "", fmt.Errorf("user ID not found in token")
}

// Add other handler methods...
