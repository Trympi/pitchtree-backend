package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"pitch-deck-generator/internal/handler"
	"pitch-deck-generator/internal/middleware"
	"pitch-deck-generator/internal/progress"
	"pitch-deck-generator/internal/service"
	"pitch-deck-generator/internal/storage"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using default environment variables")
	}

	// Initialize components
	storageService, err := storage.NewSupabaseStorage()
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	log.Println("start the server")

	progressTracker := progress.NewTracker()

	pitchDeckService := service.NewPitchDeckService(storageService, progressTracker)
	pitchDeckHandler := handler.NewPitchDeckHandler(pitchDeckService, progressTracker)

	// Setup router
	r := gin.Default()

	// Configure middleware
	r.Use(middleware.CORS())

	// Setup routes
	api := r.Group("/api")
	{
		api.POST("/pitch-decks", middleware.JWTAuth(), pitchDeckHandler.Create)
		api.GET("/pitch-decks/:deckId", middleware.JWTAuth(), pitchDeckHandler.Get)
		api.PATCH("/pitch-decks/:deckId/visibility", middleware.JWTAuth(), pitchDeckHandler.UpdateVisibility)
		api.GET("/pitch-decks", middleware.JWTAuth(), pitchDeckHandler.ListUserDecks)
		api.POST("/upload-image", middleware.JWTAuth(), pitchDeckHandler.UploadImage)
		api.GET("/progress/:deckId", pitchDeckHandler.GetProgress)
	}

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
