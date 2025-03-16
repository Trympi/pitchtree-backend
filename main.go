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

	// Step 10: File Uploads
	CompanyLogo bool `json:"companyLogo"`
	TeamPhoto   bool `json:"teamPhoto"`
	ProductDemo bool `json:"productDemo"`

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
	"default": true,
	"gaia":    true,
	"uncover": true,
	"bespoke": true,
}

func main() {
	r := gin.Default()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
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

	// Serve static files
	r.Static("/static", "./static")
	r.Static("/download", "./outputs")
	r.Static("/pdfs", "./outputs")

	// API Endpoints
	r.POST("/api/generate-pitch-deck", generatePitchDeck)
	r.GET("/api/available-themes", getAvailableThemes)

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

	// Start server
	r.Run(":8080")
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

	// Étape 1 : Traitement du contenu
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 1,
		Message:     "Processing content...",
	})
	marpContent, err := generateMarpMarkdown(data)
	if err != nil {
		log.Printf("Error generating Marp markdown: %v", err)
		sendProgressUpdate(progressChan, ProgressUpdate{
			Status:      "failed",
			CurrentStep: 1,
			Message:     "Error generating content",
		})
		close(progressChan)
		return
	}

	// Création des dossiers nécessaires
	os.MkdirAll("temp", os.ModePerm)
	os.MkdirAll("outputs", os.ModePerm)

	// Étape 2 : Création des slides
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 2,
		Message:     "Creating slides...",
	})
	mdFilePath := filepath.Join("temp", deckID+".md")
	if err := os.WriteFile(mdFilePath, []byte(marpContent), 0644); err != nil {
		log.Printf("Error saving markdown file: %v", err)
		sendProgressUpdate(progressChan, ProgressUpdate{
			Status:      "failed",
			CurrentStep: 2,
			Message:     "Error saving slides",
		})
		close(progressChan)
		return
	}

	// Étape 3 : Conversion en PDF
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "processing",
		CurrentStep: 3,
		Message:     "Converting to PDF...",
	})
	outputPath := filepath.Join("outputs", deckID+".pdf")
	args := []string{
		"@marp-team/marp-cli",
		mdFilePath,
		"--pdf",
		"--output", outputPath,
		"--theme", data.Theme,
	}
	cmd := exec.Command("npx", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Error converting to PDF: %v, stderr: %s", err, stderr.String())
		sendProgressUpdate(progressChan, ProgressUpdate{
			Status:      "failed",
			CurrentStep: 3,
			Message:     "Error converting to PDF",
		})
		close(progressChan)
		return
	}

	// Étape 4 : Finalisation
	sendProgressUpdate(progressChan, ProgressUpdate{
		Status:      "completed",
		CurrentStep: 4,
		Message:     "Finalizing deck...",
		DownloadUrl: "/download/" + deckID + ".pdf", // ou la route exacte pour le téléchargement
	})

	// Clôturer le canal
	close(progressChan)

	// Nettoyer le canal de la map globale
	progressMu.Lock()
	delete(progressChannels, deckID)
	progressMu.Unlock()
}

func sendProgressUpdate(progressChan chan string, update ProgressUpdate) {
	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Error marshalling progress update: %v", err)
		return
	}
	progressChan <- string(data)
}

func generateMarpMarkdown(data PitchDeckData) (string, error) {
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

	// 	// Append additional slides for Contact Info and Visual Assets (Step 10) independently
	// 	additionalMarkdown := fmt.Sprintf(`

	// ---
	// # Thank You & Contact Info
	// - **Email:** %s
	// - **LinkedIn:** %s
	// - **Socials:** %s
	// - **Key Takeaways:** %s

	// ---
	// # Visual Assets
	// - **Company Logo Provided:** %t
	// - **Team Photo Provided:** %t
	// - **Product Demo Provided:** %t
	// `, data.ContactInfo.Email, data.ContactInfo.Linkedin, data.ContactInfo.Socials, data.KeyTakeaways, data.CompanyLogo, data.TeamPhoto, data.ProductDemo)

	// 	marpContent += additionalMarkdown
	return marpContent, nil
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
