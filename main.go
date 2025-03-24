package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"pitch-deck-generator/prompts"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	storage "github.com/supabase-community/storage-go"
)

var (
	progressChannels = make(map[string]chan string)
	progressOwners   = make(map[string]string)
	progressMu       sync.RWMutex
)

type ProgressUpdate struct {
	Status      string `json:"status"`                // "processing", "completed" ou "failed"
	CurrentStep int    `json:"currentStep"`           // Index de l'étape actuelle (0 à n-1)
	Message     string `json:"message"`               // Message décrivant l'étape (ex: "Initializing generation...")
	DownloadUrl string `json:"downloadUrl,omitempty"` // URL disponible en cas de succès
	ViewUrl     string `json:"viewUrl,omitempty"`     // URL pour visualiser la présentation HTML
}

type PitchDeckData struct {
	// Step 1: General Project Information
	ProjectName string `json:"projectName"`
	BigIdea     string `json:"bigIdea"`

	// Step 2: Problem & Market Context
	Problem           string `json:"problem"`
	TargetAudience    string `json:"targetAudience"`
	ExistingSolutions string `json:"existingSolutions"`

	// Step 3: Solution & Competitive Advantage
	Solution        string `json:"solution"`
	Technology      string `json:"technology"`
	Differentiators string `json:"differentiators"`
	// CompetitiveAdvantage string `json:"competitiveAdvantage"`
	DevelopmentPlan string `json:"developmentPlan"`
	MarketSize      string `json:"marketSize"`

	// Step 4: Fundraising & Investment Details
	FundingAmount       string `json:"fundingAmount"`
	FundingUse          string `json:"fundingUse"`
	Valuation           string `json:"valuation"`
	InvestmentStructure string `json:"investmentStructure"`

	// Step 5: Market Opportunity
	TAM          string `json:"tam"` // Total Addressable Market
	SAM          string `json:"sam"` // Serviceable Available Market
	SOM          string `json:"som"` // Serviceable Obtainable Market
	TargetNiche  string `json:"targetNiche"`
	MarketTrends string `json:"marketTrends"`
	Industry     string `json:"industry"`

	// Step 6: Team & Experience
	WhyYou            string          `json:"whyYou"`
	TeamMembers       []TeamMemberNew `json:"teamMembers"`
	TeamQualification string          `json:"teamQualification"`

	// Step 7: Business & Revenue Model
	RevenueModel string `json:"revenueModel"`
	ScalingPlan  string `json:"scalingPlan"`
	GTMStrategy  string `json:"gtmStrategy"`

	// Step 8: Achievements & Milestones
	Achievements   string `json:"achievements"`
	NextMilestones string `json:"nextMilestones"`

	// Step 9: Thank You Slide
	ContactInfo  ContactInfo `json:"contactInfo"`
	KeyTakeaways string      `json:"keyTakeaways"`

	// Step 10: File Uploads
	CompanyLogo string `json:"companyLogo"` // Path to the company logo file
	TeamPhoto   string `json:"teamPhoto"`   // Path to the team photo file
	ProductDemo string `json:"productDemo"` // Path to the product demo image/screenshot
	Diagram     string `json:"diagram"`

	// Theme Selection
	Theme string `json:"theme"`
}

type TeamMemberNew struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	Experience string `json:"experience"`
}

type ContactInfo struct {
	Email    string `json:"email"`
	Linkedin string `json:"linkedin"`
	Socials  string `json:"socials"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type InfomaniakRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Available themes
var availableThemes = map[string]bool{
	"default":   true,
	"gaia":      true,
	"uncover":   true,
	"rose-pine": true,
}

// New struct for Supabase pitch deck records
type PitchDeckRecord struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	PdfURL    string    `json:"pdf_url"`
	HtmlURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

// PitchDeckInfo contains information about a pitch deck
type PitchDeckInfo struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	PdfURL    string    `json:"pdf_url"`
	HtmlURL   string    `json:"html_url"`
	IsPublic  bool      `json:"is_public"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// JWTAuthMiddleware validates the Supabase JWT token
func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
			c.Abort()
			return
		}

		// Check if the header has the Bearer prefix
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header format must be Bearer {token}"})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// Get the JWT secret from environment variables
		jwtSecret := os.Getenv("SUPABASE_JWT_SECRET")
		if jwtSecret == "" {
			log.Println("Warning: SUPABASE_JWT_SECRET not set")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration error"})
			c.Abort()
			return
		}

		// Parse and validate the token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// Validate the algorithm
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Check if the token is valid
		if !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Extract claims if needed
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			// You can store user information in the context if needed
			userID, _ := claims["sub"].(string)
			c.Set("userID", userID)

			// Check if token is expired
			if exp, ok := claims["exp"].(float64); ok {
				if time.Now().Unix() > int64(exp) {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Aucun fichier .env trouvé, chargement des variables d'environnement par défaut.")
	}

	r := gin.Default()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Setup CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Create necessary directories
	os.MkdirAll("temp", os.ModePerm)
	os.MkdirAll("outputs", os.ModePerm)
	os.MkdirAll("uploads", os.ModePerm)

	// Serve static files
	r.Static("/static", "./static")
	r.Static("/download", "./outputs")
	r.Static("/pdfs", "./outputs")
	r.Static("/uploads", "./uploads")

	// Public routes
	r.GET("/api/progress/:deckId", func(c *gin.Context) {
		deckID := c.Param("deckId")

		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		var userID string

		// If auth header exists, validate the token
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString := parts[1]

				// Get the JWT secret from environment variables
				jwtSecret := os.Getenv("SUPABASE_JWT_SECRET")
				if jwtSecret != "" {
					// Parse and validate the token
					token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
						if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
							return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
						}
						return []byte(jwtSecret), nil
					})

					if err == nil && token.Valid {
						if claims, ok := token.Claims.(jwt.MapClaims); ok {
							userID, _ = claims["sub"].(string)
						}
					}
				}
			}
		}

		// For in-progress decks, check the progress channel
		progressMu.RLock()
		progressChan, exists := progressChannels[deckID]
		progressMu.RUnlock()

		if exists {
			// For in-progress decks, we need to check if the user is the owner
			// This requires storing the userID when creating the progress channel
			deckOwnerID, ownerExists := progressOwners[deckID]
			if !ownerExists || (userID != "" && deckOwnerID != userID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to view this progress"})
				return
			}

			// Set headers for SSE
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Connection", "keep-alive")

			// Stream events until the channel is closed or client disconnects
			c.Stream(func(w io.Writer) bool {
				if msg, ok := <-progressChan; ok {
					c.SSEvent("message", msg)
					return true
				}
				return false
			})
			return
		}

		// For completed decks, check if the user has permission to view it
		deckInfo, err := getPitchDeckInfo(deckID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Invalid deck ID"})
			return
		}

		// If the deck is not public and the user is not the owner, deny access
		isPublic := deckInfo.IsPublic
		if !isPublic && deckInfo.UserID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to view this progress"})
			return
		}

		// Return the completed status
		c.JSON(http.StatusOK, gin.H{
			"status":      "completed",
			"downloadUrl": deckInfo.PdfURL,
			"viewUrl":     deckInfo.HtmlURL,
		})
	})

	setupHtmlRoute(r)

	// Protected API routes - require authentication
	authRoutes := r.Group("/api")
	authRoutes.Use(JWTAuthMiddleware())
	{
		authRoutes.POST("/generate-pitch-deck", generatePitchDeck)
		authRoutes.POST("/upload-image", uploadImage)
		authRoutes.PATCH("/pitch-decks/:deckId/visibility", updateDeckVisibility)
		authRoutes.GET("/pitch-decks", listUserPitchDecks)
	}

	r.Run(":" + port)
}

func setupHtmlRoute(r *gin.Engine) {
	// Add endpoint to view HTML presentation without authentication
	r.GET("/view/:deckId", func(c *gin.Context) {
		deckID := c.Param("deckId")
		log.Printf("Attempting to view deck ID: %s", deckID)

		// Try to get deck info from Supabase
		deckInfo, err := getPitchDeckInfo(deckID)
		if err == nil && deckInfo.HtmlURL != "" && strings.HasPrefix(deckInfo.HtmlURL, "http") {
			// If we have a remote URL in Supabase, redirect to it
			c.Redirect(http.StatusFound, deckInfo.HtmlURL)
			return
		}

		// Serve the local HTML file directly without permission checks
		htmlFilePath := filepath.Join("outputs", deckID+".html")
		log.Printf("Looking for HTML file at: %s", htmlFilePath)

		// Check if the HTML file exists locally
		if _, err := os.Stat(htmlFilePath); os.IsNotExist(err) {
			log.Printf("HTML file not found at path: %s", htmlFilePath)

			// Try alternative path with different casing
			alternativePath := filepath.Join("outputs", deckID+".HTML")
			log.Printf("Trying alternative path: %s", alternativePath)

			if _, err := os.Stat(alternativePath); os.IsNotExist(err) {
				// List files in the outputs directory to help debug
				files, _ := os.ReadDir("outputs")
				log.Printf("Files in outputs directory:")
				for _, file := range files {
					log.Printf("- %s", file.Name())
				}

				c.JSON(http.StatusNotFound, gin.H{"error": "Presentation not found"})
				return
			} else {
				htmlFilePath = alternativePath
			}
		}

		// Read the HTML file
		htmlContent, err := os.ReadFile(htmlFilePath)
		if err != nil {
			log.Printf("Error reading HTML file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read presentation"})
			return
		}

		log.Printf("Successfully serving HTML content for deck ID: %s", deckID)

		// Serve the HTML content
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, string(htmlContent))
	})
}

// Return available themes
func getAvailableThemes(c *gin.Context) {
	themes := make([]string, 0, len(availableThemes))
	for theme := range availableThemes {
		themes = append(themes, theme)
	}
	c.JSON(http.StatusOK, gin.H{
		"themes": themes,
	})
}

// Update the uploadImage function to use Supabase Storage
func uploadImage(c *gin.Context) {
	// Get user ID from context (set by JWTAuthMiddleware)
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found"})
		return
	}

	// Parse multipart form
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil { // 10 MB max
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse form"})
		return
	}

	// Get the file from the request
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file provided"})
		return
	}
	defer file.Close()

	// Initialize Supabase Storage client
	storageClient := initSupabaseStorage()
	if storageClient == nil {
		// Fall back to local storage if Supabase is not configured
		uploadImageLocally(c, file, header, userID.(string))
		return
	}

	// Read file content
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	// Generate a unique filename to avoid collisions
	fileExt := filepath.Ext(header.Filename)
	uniqueID := uuid.New().String()
	fileName := uniqueID + fileExt

	// Create a path with user ID for organization
	filePath := fmt.Sprintf("uploads/%s/%s", userID, fileName)

	// Determine content type based on file extension
	contentType := mime.TypeByExtension(fileExt)
	if contentType == "" {
		// If we can't determine from extension, try to detect from content
		contentType = http.DetectContentType(fileBytes)
	}

	// If still empty, default to a generic type
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Upload to Supabase Storage with content type
	fileOptions := storage.FileOptions{
		ContentType: &contentType,
	}

	_, err = storageClient.UploadFile("user-media", filePath, bytes.NewReader(fileBytes), fileOptions)
	if err != nil {
		log.Printf("Error uploading to Supabase: %v", err)
		// Fall back to local storage
		uploadImageLocally(c, bytes.NewReader(fileBytes), header, userID.(string))
		return
	}

	// Get the public URL
	supabaseURL := os.Getenv("SUPABASE_URL")
	filePath = strings.TrimPrefix(filePath, "/")
	publicURL := fmt.Sprintf("%s/storage/v1/object/public/user-media/%s",
		strings.TrimSuffix(supabaseURL, "/"),
		filePath)

	// Save the file metadata to the database (optional)
	// saveUserFileRecord(userID.(string), header.Filename, publicURL)

	// Return the URL to the client
	c.JSON(http.StatusOK, gin.H{
		"url":      publicURL,
		"filename": header.Filename,
	})
}

// Update the uploadImageLocally function to not return anything
func uploadImageLocally(c *gin.Context, file io.Reader, header *multipart.FileHeader, userID string) {
	// Create uploads directory if it doesn't exist
	os.MkdirAll("uploads", os.ModePerm)

	// Generate a unique filename
	fileExt := filepath.Ext(header.Filename)
	uniqueID := uuid.New().String()
	fileName := uniqueID + fileExt

	// Create the file path
	filePath := filepath.Join("uploads", fileName)

	// Create a new file
	dst, err := os.Create(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create file"})
		return
	}
	defer dst.Close()

	// Copy the file content
	if _, err = io.Copy(dst, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}

	// Return the URL to the client
	c.JSON(http.StatusOK, gin.H{
		"url":      "/uploads/" + fileName,
		"filename": header.Filename,
	})
}

// Add a function to save user file records to the database
func saveUserFileRecord(userID, originalName, fileURL string) error {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return fmt.Errorf("supabase credentials not set")
	}

	// Create the record
	type UserFileRecord struct {
		ID           string    `json:"id"`
		UserID       string    `json:"user_id"`
		OriginalName string    `json:"original_name"`
		FileURL      string    `json:"file_url"`
		CreatedAt    time.Time `json:"created_at"`
	}

	record := UserFileRecord{
		ID:           uuid.New().String(),
		UserID:       userID,
		OriginalName: originalName,
		FileURL:      fileURL,
		CreatedAt:    time.Now(),
	}

	// Convert to JSON
	jsonData, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Create the request
	apiURL := fmt.Sprintf("%s/rest/v1/user_files", supabaseURL)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)
	req.Header.Set("Prefer", "return=minimal")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to save record: %s", string(body))
	}

	return nil
}

func generatePitchDeck(c *gin.Context) {
	var data PitchDeckData
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate theme selection
	if data.Theme == "" {
		data.Theme = "default"
	} else if !availableThemes[data.Theme] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid theme selected"})
		return
	}

	// Get user ID from context (set by JWTAuthMiddleware)
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found"})
		return
	}

	// Generate a unique deck ID
	deckID := uuid.New().String()

	// Create progress channel for this deck and store the owner
	progressMu.Lock()
	progressChannels[deckID] = make(chan string, 10) // buffered channel
	progressOwners[deckID] = userID.(string)         // Store the owner
	progressMu.Unlock()

	// Process pitch deck generation asynchronously
	go processPitchDeck(data, deckID, userID.(string))

	c.JSON(http.StatusOK, gin.H{
		"message": "Pitch deck generation started",
		"deckId":  deckID,
	})
}

func processPitchDeck(data PitchDeckData, deckID string, userID string) {
	progressMu.RLock()
	progressChan, exists := progressChannels[deckID]
	progressMu.RUnlock()
	if !exists {
		log.Printf("No progress channel found for deckID %s", deckID)
		return
	}

	// Initialize Supabase Storage client
	storageClient := initSupabaseStorage()

	// Étape 0 : Initialisation
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 0,
		Message:     "Initializing generation...",
	})

	// Create a directory for this specific deck's resources
	deckDir := filepath.Join("temp", deckID)
	os.MkdirAll(deckDir, os.ModePerm)

	// Étape 1 : Prep images if provided
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 1,
		Message:     "Processing images...",
	})

	// Copy any provided images to the deck's directory for proper inclusion in the markdown
	imagePaths := map[string]string{}

	// Handle company logo
	if data.CompanyLogo != "" {
		if strings.HasPrefix(data.CompanyLogo, "/uploads/") {
			// Local file
			destPath := copyImageToTemp(data.CompanyLogo, deckDir, "logo")
			if destPath != "" {
				imagePaths["logo"] = destPath
			}
		} else if strings.Contains(data.CompanyLogo, "supabase") {
			// Supabase URL - download the file
			destPath := downloadImageToTemp(data.CompanyLogo, deckDir, "logo")
			if destPath != "" {
				imagePaths["logo"] = destPath
			}
		}
	}

	// Handle team photo
	if data.TeamPhoto != "" {
		if strings.HasPrefix(data.TeamPhoto, "/uploads/") {
			// Local file
			destPath := copyImageToTemp(data.TeamPhoto, deckDir, "team")
			if destPath != "" {
				imagePaths["team"] = destPath
			}
		} else if strings.Contains(data.TeamPhoto, "supabase") {
			// Supabase URL - download the file
			destPath := downloadImageToTemp(data.TeamPhoto, deckDir, "team")
			if destPath != "" {
				imagePaths["team"] = destPath
			}
		}
	}

	// Handle product demo
	if data.ProductDemo != "" {
		if strings.HasPrefix(data.ProductDemo, "/uploads/") {
			// Local file
			destPath := copyImageToTemp(data.ProductDemo, deckDir, "product")
			if destPath != "" {
				imagePaths["product"] = destPath
			}
		} else if strings.Contains(data.ProductDemo, "supabase") {
			// Supabase URL - download the file
			destPath := downloadImageToTemp(data.ProductDemo, deckDir, "product")
			if destPath != "" {
				imagePaths["product"] = destPath
			}
		}
	}

	if data.Diagram != "" {
		if strings.HasPrefix(data.Diagram, "/uploads/") {
			// Local file
			destPath := copyImageToTemp(data.Diagram, deckDir, "product")
			if destPath != "" {
				imagePaths["product"] = destPath
			}
		} else if strings.Contains(data.Diagram, "supabase") {
			// Supabase URL - download the file
			destPath := downloadImageToTemp(data.Diagram, deckDir, "product")
			if destPath != "" {
				imagePaths["product"] = destPath
			}
		}
	}

	// Étape 2 : Traitement du contenu
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 2,
		Message:     "Processing content...",
	})

	// Create a data structure for the prompt with proper image paths
	promptData := prompts.PitchDeckData{
		ProjectName:       data.ProjectName,
		BigIdea:           data.BigIdea,
		Problem:           data.Problem,
		TargetAudience:    data.TargetAudience,
		ExistingSolutions: data.ExistingSolutions,
		Solution:          data.Solution,
		Technology:        data.Technology,
		Differentiators:   data.Differentiators,
		// CompetitiveAdvantage: data.CompetitiveAdvantage,
		DevelopmentPlan:     data.DevelopmentPlan,
		MarketSize:          data.MarketSize,
		FundingAmount:       data.FundingAmount,
		FundingUse:          data.FundingUse,
		Valuation:           data.Valuation,
		InvestmentStructure: data.InvestmentStructure,
		TAM:                 data.TAM,
		SAM:                 data.SAM,
		SOM:                 data.SOM,
		TargetNiche:         data.TargetNiche,
		MarketTrends:        data.MarketTrends,
		Industry:            data.Industry,
		WhyYou:              data.WhyYou,
		TeamQualification:   data.TeamQualification,
		RevenueModel:        data.RevenueModel,
		ScalingPlan:         data.ScalingPlan,
		GTMStrategy:         data.GTMStrategy,
		Achievements:        data.Achievements,
		NextMilestones:      data.NextMilestones,
		Theme:               data.Theme,
	}

	// Set image paths - use absolute URLs for Supabase-stored images
	if logoPath, ok := imagePaths["logo"]; ok {
		// For local development, use relative path
		if strings.HasPrefix(data.CompanyLogo, "/uploads/") {
			promptData.LogoPath = logoPath
		} else {
			// For Supabase storage, use the original URL
			promptData.LogoPath = data.CompanyLogo
		}
	} else {
		promptData.LogoPath = "./logo.png" // Default placeholder
	}

	if teamPhotoPath, ok := imagePaths["team"]; ok {
		if strings.HasPrefix(data.TeamPhoto, "/uploads/") {
			promptData.TeamPhotoPath = teamPhotoPath
		} else {
			promptData.TeamPhotoPath = data.TeamPhoto
		}
	}

	if productDemoPath, ok := imagePaths["product"]; ok {
		if strings.HasPrefix(data.ProductDemo, "/uploads/") {
			promptData.ProductDemoPath = productDemoPath
		} else {
			promptData.ProductDemoPath = data.ProductDemo
		}
	}

	if diagramPhotoPath, ok := imagePaths["product"]; ok {
		if strings.HasPrefix(data.Diagram, "/uploads/") {
			promptData.DiagramPhotoPath = diagramPhotoPath
		} else {
			promptData.DiagramPhotoPath = data.Diagram
		}
	}

	// Set contact info
	promptData.ContactInfo.Email = data.ContactInfo.Email
	promptData.ContactInfo.LinkedIn = data.ContactInfo.Linkedin
	promptData.ContactInfo.Socials = data.ContactInfo.Socials
	promptData.KeyTakeaways = data.KeyTakeaways

	// Process team members
	var teamMembers []prompts.TeamMemberNew
	for _, member := range data.TeamMembers {
		teamMembers = append(teamMembers, prompts.TeamMemberNew{
			Name:       member.Name,
			Role:       member.Role,
			Experience: member.Experience,
		})
	}
	promptData.TeamMembers = teamMembers

	marpContent, err := generateMarpMarkdown(promptData, imagePaths, deckID)
	if err != nil {
		log.Printf("Error generating Marp markdown: %v", err)
		sendProgressUpdate(progressChan, ProgressUpdate{
			Status:      "failed",
			CurrentStep: 2,
			Message:     "Error generating content",
		})
		close(progressChan)
		return
	}

	// Étape 3 : Création des slides
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 3,
		Message:     "Creating slides...",
	})
	mdFilePath := filepath.Join(deckDir, "presentation.md")
	if err := os.WriteFile(mdFilePath, []byte(marpContent), 0644); err != nil {
		log.Printf("Error saving markdown file: %v", err)
		sendProgressUpdate(progressChan, ProgressUpdate{
			Status:      "failed",
			CurrentStep: 3,
			Message:     "Error saving slides",
		})
		close(progressChan)
		return
	}

	// Étape 4 : Conversion en PDF
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 4,
		Message:     "Converting to PDF...",
	})
	pdfOutputPath := filepath.Join("outputs", deckID+".pdf")
	args := []string{
		"@marp-team/marp-cli",
		mdFilePath,
		"--pdf",
		"--output", pdfOutputPath,
		"--theme", data.Theme,
		"--allow-local-files", // Important to allow local images
	}
	cmd := exec.Command("npx", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Error converting to PDF: %v, stderr: %s", err, stderr.String())
		sendProgressUpdate(progressChan, ProgressUpdate{
			Status:      "failed",
			CurrentStep: 4,
			Message:     "Error converting to PDF",
		})
		close(progressChan)
		return
	}

	// Étape 4.5 : Conversion en HTML
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 5,
		Message:     "Converting to HTML...",
	})
	htmlOutputPath := filepath.Join("outputs", deckID+".html")
	htmlArgs := []string{
		"@marp-team/marp-cli",
		mdFilePath,
		"--html",
		"--output", htmlOutputPath,
		"--theme", data.Theme,
		"--allow-local-files",
	}
	htmlCmd := exec.Command("npx", htmlArgs...)
	htmlCmd.Stdout = &stdout
	htmlCmd.Stderr = &stderr
	if err := htmlCmd.Run(); err != nil {
		log.Printf("Error converting to HTML: %v, stderr: %s", err, stderr.String())
	}

	// Étape 5: Upload to Supabase Storage
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 6,
		Message:     "Uploading files to cloud storage...",
	})

	var pdfURL, htmlURL string

	if storageClient != nil {
		// Upload PDF to Supabase
		pdfFileName := deckID + ".pdf"
		uploadedPdfURL, err := uploadToSupabase(storageClient, pdfOutputPath, "pitch-decks", pdfFileName)
		if err != nil {
			log.Printf("Error uploading PDF to Supabase: %v", err)
			// Continue with local URLs if upload fails
			pdfURL = "/download/" + deckID + ".pdf"
		} else {
			pdfURL = uploadedPdfURL
		}

		// Upload HTML to Supabase Storage
		uploadedHtmlURL, err := uploadToSupabase(storageClient, htmlOutputPath, "pitch-decks", deckID+".html")
		if err != nil {
			log.Printf("Error uploading HTML to Supabase: %v", err)
			// Continue with local URLs if upload fails
			htmlURL = "/view/" + deckID
		} else {
			htmlURL = uploadedHtmlURL
		}

		// Save record to Supabase database
		err = savePitchDeckRecord(deckID, userID, data.ProjectName, pdfURL, htmlURL)
		if err != nil {
			log.Printf("Error saving pitch deck record: %v", err)
			// Continue with local URLs if saving fails
			if pdfURL == "" {
				pdfURL = "/download/" + deckID + ".pdf"
			}
			if htmlURL == "" || !strings.HasPrefix(htmlURL, "http") {
				htmlURL = "/view/" + deckID
			}
		}
	} else {
		// Use local URLs if Supabase is not configured
		pdfURL = "/download/" + deckID + ".pdf"
		htmlURL = "/view/" + deckID
	}

	// Send final progress update with URLs
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "completed",
		CurrentStep: 7,
		Message:     "Finalizing deck...",
		DownloadUrl: pdfURL,
		ViewUrl:     htmlURL,
	})

	// Update the status in Supabase
	err = updatePitchDeckStatus(deckID, "completed")
	if err != nil {
		log.Printf("Error updating pitch deck status: %v", err)
		// Continue anyway, as this is not critical
	}

	// close canal
	close(progressChan)

	// Clean canal
	progressMu.Lock()
	delete(progressChannels, deckID)
	delete(progressOwners, deckID) // Also remove the owner mapping
	progressMu.Unlock()
}

// Helper function to copy uploaded images to the temporary deck directory
func copyImageToTemp(sourcePath string, deckDir, prefix string) string {
	// Convert web path to filesystem path
	sourcePath = "." + sourcePath

	// Make sure the source file exists
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		log.Printf("Source image does not exist: %s", sourcePath)
		return ""
	}

	// Generate destination filename with extension preserved
	ext := filepath.Ext(sourcePath)
	destFileName := prefix + ext
	destPath := filepath.Join(deckDir, destFileName)

	// Copy the file
	input, err := os.ReadFile(sourcePath)
	if err != nil {
		log.Printf("Failed to read image file: %v", err)
		return ""
	}

	if err = os.WriteFile(destPath, input, 0644); err != nil {
		log.Printf("Failed to copy image to temp directory: %v", err)
		return ""
	}

	// Return the relative path for use in markdown
	return destFileName
}

func sendProgressUpdate(progressChan chan string, update ProgressUpdate) {
	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Error marshalling progress update: %v", err)
		return
	}
	progressChan <- string(data)
}

func generateMarpMarkdown(data prompts.PitchDeckData, imagePaths map[string]string, deckID string) (string, error) {
	// Generate the prompt
	prompt, err := prompts.GeneratePitchDeckPrompt(data)
	if err != nil {
		return "", err
	}

	// Call the Infomaniak API with the prompt
	apiKey := os.Getenv("INFOMANIAK_API_KEY")
	productID := os.Getenv("INFOMANIAK_PRODUCT_ID")
	if apiKey == "" || productID == "" {
		return "", fmt.Errorf("missing Infomaniak API credentials")
	}

	infomaniakReq := InfomaniakRequest{
		Model: "mistral24b",
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	jsonData, err := json.Marshal(infomaniakReq)
	if err != nil {
		return "", err
	}

	apiURL := fmt.Sprintf("https://api.infomaniak.com/1/ai/%s/openai/chat/completions", productID)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Error creating new request:", err)
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("infomaniak API error: %s", string(body))
	}

	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return "", err
	}

	if len(apiResponse.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	marpContent := apiResponse.Choices[0].Message.Content
	marpContent = cleanMarpContent(marpContent)

	// Add image slides if images were provided
	// imageMarkdown := generateImageMarkdown(imagePaths)
	// if imageMarkdown != "" {
	// 	marpContent += "\n" + imageMarkdown
	// }

	return marpContent, nil
}

// func generateMarpHeader(logoPath, theme string) string {
// 	// If no logo is provided, just return basic header
// 	if logoPath == "" {
// 		return "---\nmarp: true\ntheme: " + theme + "\npaginate: true\n---\n\n"
// 	}

// 	// Create header with CSS for logo in footer
// 	header := "---\n"
// 	header += "marp: true\n"
// 	header += "theme: " + theme + "\n"
// 	header += "paginate: true\n"
// 	header += "style: |\n"
// 	header += "  .logo-footer {\n"
// 	header += "    position: absolute;\n"
// 	header += "    left: 10px;\n"
// 	header += "    bottom: 10px;\n"
// 	header += "    max-height: 15px;\n"
// 	header += "    z-index: 10;\n"
// 	header += "  }\n"
// 	header += "footer: '<img src=\"" + logoPath + "\" class=\"logo-footer\" alt=\"Company Logo\">'\n"
// 	header += "---\n\n"
// 	header += "# " + "Project Pitch Deck\n\n"

// 	return header
// }

// Generate markdown for images
// func generateImageMarkdown(imagePaths map[string]string) string {
// 	var imageSlides strings.Builder

// 	// Check if we have any images to add
// 	if len(imagePaths) == 0 {
// 		return ""
// 	}

// 	// Team photo slide
// 	if teamPath, exists := imagePaths["team"]; exists {
// 		imageSlides.WriteString(fmt.Sprintf(`
// ---
// # Our Team

// ![Team Photo](%s)

// `, teamPath))
// 	}

// 	// Product demo slide
// 	if productPath, exists := imagePaths["product"]; exists {
// 		imageSlides.WriteString(fmt.Sprintf(`
// ---
// # Product Demo

// ![Product Demo](%s)

// `, productPath))
// 	}

// 	return imageSlides.String()
// }

func cleanMarpContent(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") && strings.HasSuffix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			firstLine := strings.ToLower(lines[0])
			if strings.Contains(firstLine, "marp") || strings.Contains(firstLine, "markdown") {
				return strings.Join(lines[1:len(lines)-1], "\n")
			} else {
				return content
			}
		}
	}
	return content
}

func convertTeamMembers(members []TeamMemberNew) []prompts.TeamMemberNew {
	converted := make([]prompts.TeamMemberNew, len(members))
	for i, m := range members {
		converted[i] = prompts.TeamMemberNew{
			Name:       m.Name,
			Role:       m.Role,
			Experience: m.Experience,
		}
	}
	return converted
}

// Add a function to initialize Supabase Storage client
func initSupabaseStorage() *storage.Client {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY") // Use service key for admin operations

	if supabaseURL == "" || supabaseKey == "" {
		log.Println("Warning: Supabase credentials not set, storage features will be disabled")
		return nil
	}

	return storage.NewClient(supabaseURL+"/storage/v1", supabaseKey, nil)
}

// Upload a file to Supabase Storage with the correct MIME type
func uploadToSupabase(storageClient *storage.Client, filePath, bucketName, fileName string) (string, error) {
	if storageClient == nil {
		return "", fmt.Errorf("storage client not initialized")
	}

	// Read the file
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Detect MIME type based on file extension
	contentType := mime.TypeByExtension(filepath.Ext(fileName))

	// Force HTML files to be served as "text/html"
	if filepath.Ext(fileName) == ".html" || filepath.Ext(fileName) == ".htm" {
		contentType = "text/html"
	}

	if contentType == "" {
		contentType = "application/octet-stream" // Default fallback
	}

	// Ensure fileName doesn't have a leading slash
	fileName = strings.TrimPrefix(fileName, "/")

	// Upload to Supabase Storage with correct content type
	_, err = storageClient.UploadFile(
		bucketName,
		fileName,
		bytes.NewReader(fileContent),
		storage.FileOptions{ContentType: &contentType},
	)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Get the public URL - Fix the double slash issue
	supabaseURL := os.Getenv("SUPABASE_URL")
	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s",
		strings.TrimSuffix(supabaseURL, "/"), // Remove trailing slash if present
		bucketName,
		fileName)

	return publicURL, nil
}

// Add a function to save pitch deck record to Supabase database
func savePitchDeckRecord(deckID, userID, name, pdfURL, htmlURL string) error {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return fmt.Errorf("supabase credentials not set")
	}

	// Create the record
	record := PitchDeckInfo{
		ID:        deckID,
		UserID:    userID,
		Name:      name,
		PdfURL:    pdfURL,
		HtmlURL:   htmlURL,
		IsPublic:  false,       // Default to private
		Status:    "completed", // Set status to completed
		CreatedAt: time.Now(),
	}

	// Convert to JSON
	jsonData, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Create the request
	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks", supabaseURL)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)
	req.Header.Set("Prefer", "return=minimal")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to save record: %s", string(body))
	}

	return nil
}

// Add a function to download images from URLs to the temp directory
func downloadImageToTemp(imageURL, deckDir, prefix string) string {
	// Log the URL being requested
	log.Printf("Attempting to download image from: %s", imageURL)

	// Validate URL format
	_, err := url.Parse(imageURL)
	if err != nil {
		log.Printf("Invalid image URL format: %v", err)
		return ""
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Get(imageURL)
	if err != nil {
		log.Printf("Failed to download image from URL: %v", err)
		return ""
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download image, status: %d, URL: %s", resp.StatusCode, imageURL)
		return ""
	}

	// Determine file extension from Content-Type
	contentType := resp.Header.Get("Content-Type")
	ext := ".jpg" // Default extension

	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	case "image/svg+xml":
		ext = ".svg"
	}

	// If Content-Type is not reliable, try to get extension from URL
	if ext == ".jpg" && strings.Contains(imageURL, ".") {
		urlExt := filepath.Ext(imageURL)
		if urlExt != "" {
			ext = urlExt
		}
	}

	// Generate destination filename
	destFileName := prefix + ext
	destPath := filepath.Join(deckDir, destFileName)

	// Create the file
	out, err := os.Create(destPath)
	if err != nil {
		log.Printf("Failed to create file: %v", err)
		return ""
	}
	defer out.Close()

	// Copy the response body to the file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Printf("Failed to save image: %v", err)
		return ""
	}

	// Return the relative path for use in markdown
	return destFileName
}

// getPitchDeckInfo retrieves information about a pitch deck from Supabase
func getPitchDeckInfo(deckID string) (*PitchDeckInfo, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return nil, fmt.Errorf("supabase credentials not set")
	}

	// Create the request to get the pitch deck record
	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?id=eq.%s&select=*", supabaseURL, deckID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get record: %s", string(body))
	}

	// Parse the response
	var decks []PitchDeckInfo
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &decks); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(decks) == 0 {
		return nil, fmt.Errorf("pitch deck not found")
	}

	return &decks[0], nil
}

// Function to update deck visibility
func updateDeckVisibility(c *gin.Context) {
	deckID := c.Param("deckId")
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found"})
		return
	}

	// Parse request body
	var requestBody struct {
		IsPublic bool `json:"isPublic"`
	}

	if err := c.ShouldBindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get the deck info to verify ownership
	deckInfo, err := getPitchDeckInfo(deckID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deck not found"})
		return
	}

	// Verify ownership
	if deckInfo.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to update this deck"})
		return
	}

	// Update the visibility
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Supabase credentials not set"})
		return
	}

	updateData := map[string]bool{
		"is_public": requestBody.IsPublic,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create update payload"})
		return
	}

	// Create the request
	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?id=eq.%s", supabaseURL, deckID)
	req, err := http.NewRequest("PATCH", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)
	req.Header.Set("Prefer", "return=minimal")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send request"})
		return
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to update visibility: %s", string(body))})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Deck visibility updated successfully",
		"isPublic": requestBody.IsPublic,
	})
}

// Function to list user's pitch decks
func listUserPitchDecks(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found"})
		return
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Supabase credentials not set"})
		return
	}

	// Create the request to get the user's pitch decks
	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?user_id=eq.%s&order=created_at.desc", supabaseURL, userID.(string))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	// Set headers
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send request"})
		return
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get decks: %s", string(body))})
		return
	}

	// Parse the response
	var decks []PitchDeckInfo
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if err := json.Unmarshal(body, &decks); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse response"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"decks": decks,
	})
}

// Function to update pitch deck status in Supabase
func updatePitchDeckStatus(deckID string, status string) error {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return fmt.Errorf("supabase credentials not set")
	}

	// Create the update payload
	updateData := map[string]string{
		"status": status,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal update data: %w", err)
	}

	// Create the request
	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?id=eq.%s", supabaseURL, deckID)
	req, err := http.NewRequest("PATCH", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)
	req.Header.Set("Prefer", "return=minimal")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update status: %s", string(body))
	}

	return nil
}
