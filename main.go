package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

var (
	progressChannels = make(map[string]chan string)
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
	Solution             string `json:"solution"`
	Technology           string `json:"technology"`
	Differentiators      string `json:"differentiators"`
	CompetitiveAdvantage string `json:"competitiveAdvantage"`
	DevelopmentPlan      string `json:"developmentPlan"`
	MarketSize           string `json:"marketSize"`

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

	// Step 10: File Uploads - Modified to store image paths instead of booleans
	CompanyLogo string `json:"companyLogo"` // Path to the company logo file
	TeamPhoto   string `json:"teamPhoto"`   // Path to the team photo file
	ProductDemo string `json:"productDemo"` // Path to the product demo image/screenshot

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
		AllowHeaders:     []string{"Origin", "Content-Type"},
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

	// API Endpoints
	r.POST("/api/generate-pitch-deck", generatePitchDeck)
	r.GET("/api/available-themes", getAvailableThemes)

	// New endpoint for image uploads
	r.POST("/api/upload-image", uploadImage)

	r.GET("/api/progress/:deckId", func(c *gin.Context) {
		deckID := c.Param("deckId")

		progressMu.RLock()
		progressChan, exists := progressChannels[deckID]
		progressMu.RUnlock()
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Invalid deck ID"})
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
	})

	setupHtmlRoute(r)

	r.Run(":" + port)
}

func setupHtmlRoute(r *gin.Engine) {
	// Add endpoint to view HTML presentation
	r.GET("/view/:deckId", func(c *gin.Context) {
		deckID := c.Param("deckId")
		htmlFilePath := filepath.Join("outputs", deckID+".html")

		// Check if the HTML file exists
		if _, err := os.Stat(htmlFilePath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Presentation not found"})
			return
		}

		// Read the HTML file
		htmlContent, err := os.ReadFile(htmlFilePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read presentation"})
			return
		}

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

// New function for handling image uploads
func uploadImage(c *gin.Context) {
	// Get the file from the request
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No image file provided"})
		return
	}

	// Check file type
	if !isValidImageType(file.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image format. Only jpg, jpeg, png, gif, and svg are allowed"})
		return
	}

	// Generate a unique filename to prevent collisions
	filename := uuid.New().String() + filepath.Ext(file.Filename)
	filePath := filepath.Join("uploads", filename)

	// Save the file
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image"})
		return
	}

	// Return the file path (relative to root) for the client to use
	c.JSON(http.StatusOK, gin.H{
		"path": "/uploads/" + filename,
		"url":  "/uploads/" + filename,
	})
}

// Helper function to check if the uploaded file is a valid image type
func isValidImageType(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	validExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".svg":  true,
	}
	return validExts[ext]
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

	// Generate a unique deck ID
	deckID := uuid.New().String()

	// progress channel for this deck
	progressMu.Lock()
	progressChannels[deckID] = make(chan string, 10) // buffered channel
	progressMu.Unlock()

	// Process pitch deck generation asynchronously
	go processPitchDeck(data, deckID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Pitch deck generation started",
		"deckId":  deckID,
	})
}

// func convertMarkdownToHTML(mdFilePath string, outputPath string) error {
// 	args := []string{
// 		"@marp-team/marp-cli",
// 		mdFilePath,
// 		"--html",
// 		"--output", outputPath,
// 		"--allow-local-files",
// 	}
// 	cmd := exec.Command("npx", args...)
// 	var stdout, stderr bytes.Buffer
// 	cmd.Stdout = &stdout
// 	cmd.Stderr = &stderr
// 	if err := cmd.Run(); err != nil {
// 		log.Printf("Error converting to HTML: %v, stderr: %s", err, stderr.String())
// 		return err
// 	}
// 	return nil
// }

func processPitchDeck(data PitchDeckData, deckID string) {
	progressMu.RLock()
	progressChan, exists := progressChannels[deckID]
	progressMu.RUnlock()
	if !exists {
		log.Printf("No progress channel found for deckID %s", deckID)
		return
	}

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
	if data.CompanyLogo != "" && strings.HasPrefix(data.CompanyLogo, "/uploads/") {
		destPath := copyImageToTemp(data.CompanyLogo, deckDir, "logo")
		if destPath != "" {
			imagePaths["logo"] = destPath
		}
	}

	if data.TeamPhoto != "" && strings.HasPrefix(data.TeamPhoto, "/uploads/") {
		destPath := copyImageToTemp(data.TeamPhoto, deckDir, "team")
		if destPath != "" {
			imagePaths["team"] = destPath
		}
	}

	if data.ProductDemo != "" && strings.HasPrefix(data.ProductDemo, "/uploads/") {
		destPath := copyImageToTemp(data.ProductDemo, deckDir, "product")
		if destPath != "" {
			imagePaths["product"] = destPath
		}
	}

	// Étape 2 : Traitement du contenu
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 2,
		Message:     "Processing content...",
	})
	marpContent, err := generateMarpMarkdown(data, imagePaths, deckID)
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

	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "completed",
		CurrentStep: 6,
		Message:     "Finalizing deck...",
		DownloadUrl: "/download/" + deckID + ".pdf",
		ViewUrl:     "/view/" + deckID,
	})

	// close canal
	close(progressChan)

	// Clean canal
	progressMu.Lock()
	delete(progressChannels, deckID)
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

func generateMarpMarkdown(data PitchDeckData, imagePaths map[string]string, deckID string) (string, error) {
	// Format team members for the prompt
	teamInfo := formatTeamMembersNew(data.TeamMembers)

	// Build the prompt using the new fields but excluding contact info and visual assets
	prompt := fmt.Sprintf(`
	You are an expert in crafting Marp markdown presentations. Use the following data to generate a complete, ready-to-use pitch deck in Marp markdown format.

-- Project Overview --
Project Name: %s
Big Idea: %s

-- Market Context --
Problem: %s
Target Audience: %s
Existing Solutions: %s

-- Solution & Competitive Advantage --
Solution: %s
Technology: %s
Differentiators: %s
Competitive Advantage: %s
Development Plan: %s
Market Size: %s

-- Fundraising & Investment Details --
Funding Amount: %s
Funding Use: %s
Valuation: %s
Investment Structure: %s

-- Market Opportunity --
TAM: %s
SAM: %s
SOM: %s
Target Niche: %s
Market Trends: %s

-- Team & Experience --
Why You: %s
Team Members: %s
Team Qualification: %s

-- Business & Revenue Model --
Revenue Model: %s
Scaling Plan: %s
GTM Strategy: %s

-- Achievements & Milestones --
Achievements: %s
Next Milestones: %s

-- Contact Information --
Email: %s
LinkedIn: %s
Socials: %s
Key Takeaways: %s

Ensure the document is fully formatted in Marp markdown with the necessary directives at the top (e.g., marp: true, theme, paginate, backgroundColor, color).
`,
		data.ProjectName, data.BigIdea,
		data.Problem, data.TargetAudience, data.ExistingSolutions,
		data.Solution, data.Technology, data.Differentiators, data.CompetitiveAdvantage, data.DevelopmentPlan, data.MarketSize,
		data.FundingAmount, data.FundingUse, data.Valuation, data.InvestmentStructure,
		data.TAM, data.SAM, data.SOM, data.TargetNiche, data.MarketTrends,
		data.WhyYou, teamInfo, data.TeamQualification,
		data.RevenueModel, data.ScalingPlan, data.GTMStrategy,
		data.Achievements, data.NextMilestones,
		data.ContactInfo.Email, data.ContactInfo.Linkedin, data.ContactInfo.Socials, data.KeyTakeaways,
	)

	// Call the Infomaniak API with the prompt
	apiKey := os.Getenv("INFOMANIAK_API_KEY")
	productID := os.Getenv("INFOMANIAK_PRODUCT_ID")
	if apiKey == "" || productID == "" {
		return "", fmt.Errorf("missing Infomaniak API credentials")
	}

	infomaniakReq := InfomaniakRequest{
		Model: "mixtral",
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
	imageMarkdown := generateImageMarkdown(imagePaths)
	if imageMarkdown != "" {
		marpContent += "\n" + imageMarkdown
	}

	return marpContent, nil
}

// Generate markdown for images
func generateImageMarkdown(imagePaths map[string]string) string {
	var imageSlides strings.Builder

	// Check if we have any images to add
	if len(imagePaths) == 0 {
		return ""
	}

	// Company logo slide
	if logoPath, exists := imagePaths["logo"]; exists {
		imageSlides.WriteString(fmt.Sprintf(`
---
# Company Logo

![Company Logo](./%s)

`, logoPath))
	}

	// Team photo slide
	if teamPath, exists := imagePaths["team"]; exists {
		imageSlides.WriteString(fmt.Sprintf(`
---
# Our Team

![Team Photo](./%s)

`, teamPath))
	}

	// Product demo slide
	if productPath, exists := imagePaths["product"]; exists {
		imageSlides.WriteString(fmt.Sprintf(`
---
# Product Demo

![Product Demo](./%s)

`, productPath))
	}

	return imageSlides.String()
}

func cleanMarpContent(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") && strings.HasSuffix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			return strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	return content
}

func formatTeamMembersNew(members []TeamMemberNew) string {
	var sb strings.Builder
	for i, m := range members {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%s (%s): %s", m.Name, m.Role, m.Experience))
	}
	return sb.String()
}
