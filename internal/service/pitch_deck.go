package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pitch-deck-generator/internal/model"
	"pitch-deck-generator/internal/progress"
	"pitch-deck-generator/prompts"

	"github.com/google/uuid"
)

type PitchDeckService struct {
	storage  model.StorageService
	progress *progress.Tracker
}

type InfomaniakRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func NewPitchDeckService(storage model.StorageService, progress *progress.Tracker) *PitchDeckService {
	return &PitchDeckService{
		storage:  storage,
		progress: progress,
	}
}

func (s *PitchDeckService) Create(data model.PitchDeckData, userID string) (*model.PitchDeckInfo, error) {
	// Generate unique ID for the deck
	deckID := uuid.New().String()

	// Create progress channel
	progressChan := s.progress.CreateChannel(deckID, userID)

	// Create deck info
	deckInfo := &model.PitchDeckInfo{
		ID:        deckID,
		UserID:    userID,
		Name:      data.ProjectName,
		Status:    "processing",
		CreatedAt: time.Now(),
	}

	// Start async processing
	go s.processDeck(data, deckInfo, progressChan)

	return deckInfo, nil
}

func (s *PitchDeckService) processDeck(data model.PitchDeckData, deckInfo *model.PitchDeckInfo, progressChan chan string) {
	// Create temporary directory for this deck
	deckDir := filepath.Join("temp", deckInfo.ID)
	os.MkdirAll(deckDir, os.ModePerm)
	// defer os.RemoveAll(deckDir)

	// Send initial progress update
	s.progress.SendUpdate(deckInfo.ID, progress.ProgressUpdate{
		Status:      "processing",
		CurrentStep: 1,
		Message:     "Processing images...",
	})

	// Process images
	imagePaths := s.processImages(data, deckDir)

	// Generate markdown content
	s.progress.SendUpdate(deckInfo.ID, progress.ProgressUpdate{
		Status:      "processing",
		CurrentStep: 2,
		Message:     "Generating content...",
	})

	markdown, err := s.generateMarkdown(data, imagePaths)
	if err != nil {
		s.handleError(deckInfo.ID, "Failed to generate content", err)
		return
	}

	// Save markdown file
	mdPath := filepath.Join(deckDir, "presentation.md")
	if err := os.WriteFile(mdPath, []byte(markdown), 0644); err != nil {
		s.handleError(deckInfo.ID, "Failed to save markdown", err)
		return
	}

	// Convert to PDF and HTML
	s.progress.SendUpdate(deckInfo.ID, progress.ProgressUpdate{
		Status:      "processing",
		CurrentStep: 3,
		Message:     "Converting to PDF and HTML...",
	})

	pdfPath := filepath.Join("outputs", deckInfo.ID+".pdf")
	htmlPath := filepath.Join("outputs", deckInfo.ID+".html")

	if err := s.convertToPDF(mdPath, pdfPath, data.Theme); err != nil {
		s.handleError(deckInfo.ID, "Failed to convert to PDF", err)
		return
	}

	if err := s.convertToHTML(mdPath, htmlPath, data.Theme); err != nil {
		s.handleError(deckInfo.ID, "Failed to convert to HTML", err)
		return
	}

	// Upload files to storage
	s.progress.SendUpdate(deckInfo.ID, progress.ProgressUpdate{
		Status:      "processing",
		CurrentStep: 4,
		Message:     "Uploading files...",
	})

	var pdfURL, htmlURL string

	// Verify if storage service is not nil
	if s.storage != nil {
		// Upload PDF
		pdfURL, err = s.storage.UploadFile(pdfPath, "pitch-decks", deckInfo.ID+".pdf")
		if err != nil {
			s.handleError(deckInfo.ID, "Failed to upload PDF", err)
			return
		}

		// Upload HTML
		htmlURL, err = s.storage.UploadFile(htmlPath, "pitch-decks", deckInfo.ID+".html")
		if err != nil {
			s.handleError(deckInfo.ID, "Failed to upload HTML", err)
			return
		}

		err = SavePitchDeckRecord(deckInfo.ID, deckInfo.UserID, data.ProjectName, pdfURL, htmlURL)
		if err != nil {
			log.Printf("Error saving pitch deck record in supabase: %v", err)
		}
	}

	// Update deck info with URLs
	deckInfo.PdfURL = pdfURL
	deckInfo.HtmlURL = htmlURL
	deckInfo.Status = "completed"

	// Send final update
	s.progress.SendUpdate(deckInfo.ID, progress.ProgressUpdate{
		Status:      "completed",
		CurrentStep: 5,
		Message:     "Generation completed",
		DownloadUrl: pdfURL,
		ViewUrl:     htmlURL,
	})

	// Update status in database
	if err := s.UpdateStatus(deckInfo.ID, "completed"); err != nil {
		log.Printf("Failed to persist completed status: %v", err)
	}

	// Cleanup local files after successful upload
	// os.Remove(pdfPath)
	// os.Remove(htmlPath)
	// os.RemoveAll(deckDir)

	// Close the channel
	s.progress.CloseChannel(deckInfo.ID)
}

func (s *PitchDeckService) Get(deckID string) (*model.PitchDeckInfo, error) {
	// Make request to Supabase
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?id=eq.%s", supabaseURL, deckID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var decks []model.PitchDeckInfo
	if err := json.NewDecoder(resp.Body).Decode(&decks); err != nil {
		return nil, err
	}

	if len(decks) == 0 {
		return nil, fmt.Errorf("deck not found")
	}

	return &decks[0], nil
}

func SavePitchDeckRecord(deckID, userID, name, pdfURL, htmlURL string) error {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return fmt.Errorf("supabase credentials not set")
	}

	// Create the record
	record := model.PitchDeckInfo{
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

func (s *PitchDeckService) UpdateVisibility(deckID string, userID string, isPublic bool) error {
	// Verify ownership
	deck, err := s.Get(deckID)
	if err != nil {
		return err
	}

	if deck.UserID != userID {
		return fmt.Errorf("unauthorized")
	}

	// Update in Supabase
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	data := map[string]bool{"is_public": isPublic}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?id=eq.%s", supabaseURL, deckID)
	req, err := http.NewRequest("PATCH", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update visibility")
	}

	return nil
}

func (s *PitchDeckService) ListUserDecks(userID string) ([]model.PitchDeckInfo, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?user_id=eq.%s&order=created_at.desc", supabaseURL, userID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var decks []model.PitchDeckInfo
	if err := json.NewDecoder(resp.Body).Decode(&decks); err != nil {
		return nil, err
	}

	return decks, nil
}

func (s *PitchDeckService) UpdateStatus(deckID string, status string) error {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	data := map[string]string{"status": status}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if supabaseURL == "" || supabaseKey == "" {
		return fmt.Errorf("supabase credentials not set")
	}

	apiURL := fmt.Sprintf("%s/rest/v1/pitch_decks?id=eq.%s", supabaseURL, deckID)
	req, err := http.NewRequest("PATCH", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

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

	log.Println("Updating status at URL:", apiURL)

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Println("Response:", resp.StatusCode, string(body))
		return fmt.Errorf("failed to update status: %s", string(body))
	}

	return nil
}

// Helper methods
func (s *PitchDeckService) handleError(deckID, message string, err error) {
	s.progress.SendUpdate(deckID, progress.ProgressUpdate{
		Status:  "failed",
		Message: fmt.Sprintf("%s: %v", message, err),
	})
	s.UpdateStatus(deckID, "failed")
}

func (s *PitchDeckService) processImages(data model.PitchDeckData, deckDir string) map[string]string {
	imagePaths := make(map[string]string)

	// Process company logo
	if data.CompanyLogo != "" {
		if logoPath := s.downloadImage(data.CompanyLogo, deckDir, "logo"); logoPath != "" {
			imagePaths["logo"] = logoPath
		}
	}

	// Process team photo
	if data.TeamPhoto != "" {
		if teamPath := s.downloadImage(data.TeamPhoto, deckDir, "team"); teamPath != "" {
			imagePaths["team"] = teamPath
		}
	}

	// Process diagram
	if data.Diagram != "" {
		if diagramPath := s.downloadImage(data.Diagram, deckDir, "diagram"); diagramPath != "" {
			imagePaths["diagram"] = diagramPath
		}
	}

	return imagePaths
}

func (s *PitchDeckService) downloadImage(imageURL, deckDir, prefix string) string {
	// Validate URL format
	if !strings.HasPrefix(imageURL, "http") {
		return imageURL // Return as-is if it's a local path
	}

	// Create HTTP client with timeout
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
		log.Printf("Failed to download image, status: %d", resp.StatusCode)
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

	return destFileName
}

func (s *PitchDeckService) generateMarkdown(data model.PitchDeckData, imagePaths map[string]string) (string, error) {
	// Get API keys from environment variables
	googleKey := os.Getenv("GEMINI_API_KEY")
	if googleKey == "" {
		return "", fmt.Errorf("missing Gemini API key")
	}

	// Convert model.PitchDeckData to prompts.PitchDeckData
	promptData := prompts.PitchDeckData{
		// Project Information
		ProjectName: data.ProjectName,
		BigIdea:     data.BigIdea,

		// Market Analysis
		Problem:           data.Problem,
		TargetAudience:    data.TargetAudience,
		ExistingSolutions: data.ExistingSolutions,

		// Solution Details
		Solution:        data.Solution,
		Technology:      data.Technology,
		Differentiators: data.Differentiators,
		DevelopmentPlan: data.DevelopmentPlan,

		// Investment Information
		FundingAmount:       data.FundingAmount,
		FundingUse:          data.FundingUse,
		Valuation:           data.Valuation,
		InvestmentStructure: data.InvestmentStructure,

		// Market Opportunity
		TAM:          data.TAM,
		SAM:          data.SAM,
		SOM:          data.SOM,
		TargetNiche:  data.TargetNiche,
		MarketTrends: data.MarketTrends,
		Industry:     data.Industry,

		// Team Information
		WhyYou:            data.WhyYou,
		TeamQualification: data.TeamQualification,

		// Theme and Visual Settings
		Theme: data.Theme,

		// Image Paths
		LogoPath:         imagePaths["logo"],
		TeamPhotoPath:    imagePaths["team"],
		DiagramPhotoPath: imagePaths["diagram"],
	}

	// Convert team members
	var teamMembers []prompts.TeamMemberNew
	for _, member := range data.TeamMembers {
		teamMembers = append(teamMembers, prompts.TeamMemberNew{
			Name:       member.Name,
			Role:       member.Role,
			Experience: member.Experience,
		})
	}
	promptData.TeamMembers = teamMembers

	// Set contact info
	promptData.ContactInfo.Email = data.ContactInfo.Email
	promptData.ContactInfo.LinkedIn = data.ContactInfo.Linkedin
	promptData.ContactInfo.Socials = data.ContactInfo.Socials
	promptData.KeyTakeaways = data.KeyTakeaways

	// Generate the prompt using the template
	prompt, err := prompts.GeneratePitchDeckPrompt(promptData)
	if err != nil {
		return "", fmt.Errorf("failed to generate prompt: %w", err)
	}

	// Gemini API request structure

	type GeminiPart struct {
		Text string `json:"text"`
	}
	type GeminiContent struct {
		Parts []GeminiPart `json:"parts"`
	}
	type GeminiRequest struct {
		Contents []GeminiContent `json:"contents"`
	}

	requestPayload := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []GeminiPart{
					{
						Text: prompt,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(requestPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Gemini API endpoint for text generation (use gemini-1.5-flash-latest)
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash-latest:generateContent?key=%s", googleKey)

	// Create and execute the HTTP request
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status: %d, body: %s", resp.StatusCode, string(body))
	}

	// Define the expected response structure
	type GeminiResponse struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	var geminiResponse GeminiResponse
	err = json.Unmarshal(body, &geminiResponse)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(body))
	}

	// Extract the generated text
	var markdown string
	if len(geminiResponse.Candidates) > 0 && len(geminiResponse.Candidates[0].Content.Parts) > 0 {
		markdown = geminiResponse.Candidates[0].Content.Parts[0].Text
	} else {
		return "", fmt.Errorf("no generated text found in response: %s", string(body))
	}

	markdown = cleanMarpContent(markdown)

	log.Println("markdown", markdown)

	return markdown, nil
}

// func (s *PitchDeckService) generateMarkdown(data model.PitchDeckData, imagePaths map[string]string) (string, error) {
// 	// Call the Infomaniak API with the prompt
// 	apiKey := os.Getenv("INFOMANIAK_API_KEY")
// 	productID := os.Getenv("INFOMANIAK_PRODUCT_ID")
// 	if apiKey == "" || productID == "" {
// 		return "", fmt.Errorf("missing Infomaniak API credentials")
// 	}

// 	googleKey := os.Getenv("GEMINI_API_KEY")
// 	if googleKey == "" {
// 		return "", fmt.Errorf("missing Gemini API key")
// 	}

// 	// Convert model.PitchDeckData to prompts.PitchDeckData
// 	promptData := prompts.PitchDeckData{
// 		// Project Information
// 		ProjectName: data.ProjectName,
// 		BigIdea:     data.BigIdea,

// 		// Market Analysis
// 		Problem:           data.Problem,
// 		TargetAudience:    data.TargetAudience,
// 		ExistingSolutions: data.ExistingSolutions,

// 		// Solution Details
// 		Solution:        data.Solution,
// 		Technology:      data.Technology,
// 		Differentiators: data.Differentiators,
// 		DevelopmentPlan: data.DevelopmentPlan,

// 		// Investment Information
// 		FundingAmount:       data.FundingAmount,
// 		FundingUse:          data.FundingUse,
// 		Valuation:           data.Valuation,
// 		InvestmentStructure: data.InvestmentStructure,

// 		// Market Opportunity
// 		TAM:          data.TAM,
// 		SAM:          data.SAM,
// 		SOM:          data.SOM,
// 		TargetNiche:  data.TargetNiche,
// 		MarketTrends: data.MarketTrends,
// 		Industry:     data.Industry,

// 		// Team Information
// 		WhyYou:            data.WhyYou,
// 		TeamQualification: data.TeamQualification,

// 		// Theme and Visual Settings
// 		Theme: data.Theme,

// 		// Image Paths
// 		LogoPath:         imagePaths["logo"],
// 		TeamPhotoPath:    imagePaths["team"],
// 		DiagramPhotoPath: imagePaths["diagram"],
// 	}

// 	// Convert team members
// 	var teamMembers []prompts.TeamMemberNew
// 	for _, member := range data.TeamMembers {
// 		teamMembers = append(teamMembers, prompts.TeamMemberNew{
// 			Name:       member.Name,
// 			Role:       member.Role,
// 			Experience: member.Experience,
// 		})
// 	}
// 	promptData.TeamMembers = teamMembers
// 	// Set contact info
// 	promptData.ContactInfo.Email = data.ContactInfo.Email
// 	promptData.ContactInfo.LinkedIn = data.ContactInfo.Linkedin
// 	promptData.ContactInfo.Socials = data.ContactInfo.Socials
// 	promptData.KeyTakeaways = data.KeyTakeaways

// 	// Generate the prompt using the template
// 	prompt, err := prompts.GeneratePitchDeckPrompt(promptData)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to generate prompt: %w", err)
// 	}

// 	geminiReq := map[string]interface{}{
// 		"model": "gemini-1.5-flash",
// 		"messages": []map[string]string{
// 			{"role": "user", "content": prompt},
// 		},
// 		"temperature": 0.7,
// 		"max_tokens":  4000,
// 	}

// 	jsonData, err := json.Marshal(geminiReq)
// 	if err != nil {
// 		return "", err
// 	}

// 	// Call Gemini API
// 	apiURL := "https://generativelanguage.googleapis.com/v1/models/gemini-pro:generateText?key=" + googleKey
// 	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
// 	if err != nil {
// 		return "", fmt.Errorf("failed to create request: %w", err)
// 	}

// 	req.Header.Set("Content-Type", "application/json")

// 	client := &http.Client{}
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to send request: %w", err)
// 	}
// 	defer resp.Body.Close()

// 	var result struct {
// 		Candidates []struct {
// 			Output string `json:"output"`
// 		} `json:"candidates"`
// 	}

// 	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
// 		return "", fmt.Errorf("failed to decode response: %w", err)
// 	}

// 	if len(result.Candidates) == 0 {
// 		return "", fmt.Errorf("no content generated")
// 	}

// 	markdown := result.Candidates[0].Output
// 	markdown = cleanMarpContent(markdown)

// 	// infomaniakReq := InfomaniakRequest{
// 	// 	Model: "mistral24b",
// 	// 	Messages: []Message{
// 	// 		{
// 	// 			Role:    "user",
// 	// 			Content: prompt,
// 	// 		},
// 	// 	},
// 	// 	Temperature: 0.7,
// 	// 	MaxTokens:   4000,
// 	// }

// 	// jsonData, err := json.Marshal(infomaniakReq)
// 	// if err != nil {
// 	// 	return "", err
// 	// }

// 	// // Call Infomaniak API
// 	// apiURL := fmt.Sprintf("https://api.infomaniak.com/1/ai/%s/openai/chat/completions", productID)
// 	// req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
// 	// if err != nil {
// 	// 	return "", fmt.Errorf("failed to create request: %w", err)
// 	// }

// 	// req.Header.Set("Content-Type", "application/json")
// 	// req.Header.Set("Authorization", "Bearer "+apiKey)

// 	// client := &http.Client{}
// 	// resp, err := client.Do(req)
// 	// if err != nil {
// 	// 	return "", fmt.Errorf("failed to send request: %w", err)
// 	// }
// 	// defer resp.Body.Close()

// 	// var result struct {
// 	// 	Choices []struct {
// 	// 		Message struct {
// 	// 			Content string `json:"content"`
// 	// 		} `json:"message"`
// 	// 	} `json:"choices"`
// 	// }

// 	// if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
// 	// 	return "", fmt.Errorf("failed to decode response: %w", err)
// 	// }

// 	// if len(result.Choices) == 0 {
// 	// 	return "", fmt.Errorf("no content generated")
// 	// }

// 	// Get the generated markdown
// 	// markdown := result.Choices[0].Message.Content
// 	// markdown = cleanMarpContent(markdown)

// 	return markdown, nil
// }

// Add this helper function

// func cleanMarpContent(content string) string {
// 	content = strings.TrimSpace(content)
// 	if strings.HasPrefix(content, "```") && strings.HasSuffix(content, "```") {
// 		lines := strings.Split(content, "\n")
// 		if len(lines) > 2 {
// 			firstLine := strings.ToLower(lines[0])
// 			if strings.Contains(firstLine, "marp") || strings.Contains(firstLine, "markdown") {
// 				return strings.Join(lines[1:len(lines)-1], "\n")
// 			} else {
// 				return content
// 			}
// 		}
// 	}
// 	return content
// }

// extractMarkdownContent extracts markdown content between triple backticks
func cleanMarpContent(text string) string {
	lines := regexp.MustCompile(`\r?\n`).Split(text, -1)

	firstBacktickLine := -1
	lastBacktickLine := -1

	// Find first and last lines with triple backticks
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			if firstBacktickLine == -1 {
				firstBacktickLine = i
			}
			lastBacktickLine = i
		}
	}

	// If we found backticks, extract the content
	if firstBacktickLine != -1 && lastBacktickLine != -1 && lastBacktickLine > firstBacktickLine {
		// Extract content between the backtick lines, excluding the lines with backticks themselves
		// firstBacktickLine+1 skips the opening backtick line
		// lastBacktickLine as the end index (exclusive in Go slices) excludes the closing backtick line
		content := lines[firstBacktickLine+1 : lastBacktickLine]
		return strings.Join(content, "\n")
	}

	// If no backticks found, return the entire text
	return text
}

// func cleanMarkdown(content string) string {
// 	content = strings.TrimSpace(content)
// 	// Remove markdown code block if present
// 	if strings.HasPrefix(content, "```markdown") || strings.HasPrefix(content, "```marp") {
// 		lines := strings.Split(content, "\n")
// 		if len(lines) > 2 && strings.HasSuffix(content, "```") {
// 			// Remove first and last line (the code block markers)
// 			return strings.Join(lines[1:len(lines)-1], "\n")
// 		}
// 	}
// 	return content
// }

func (s *PitchDeckService) insertImages(markdown string, imagePaths map[string]string) string {
	// Insert logo on first slide
	if logo, ok := imagePaths["logo"]; ok {
		markdown = strings.Replace(
			markdown,
			"# "+strings.Split(markdown, "\n")[0],
			fmt.Sprintf("# %s\n\n![Company Logo w:80](%s)",
				strings.Split(markdown, "\n")[0],
				logo),
			1,
		)
	}

	// Insert other images at appropriate sections
	if demo, ok := imagePaths["demo"]; ok {
		markdown = strings.Replace(
			markdown,
			"# Our Solution",
			fmt.Sprintf("# Our Solution\n\n![Product Demo w:600px](%s)", demo),
			1,
		)
	}

	if diagram, ok := imagePaths["diagram"]; ok {
		markdown = strings.Replace(
			markdown,
			"# Market Opportunity",
			fmt.Sprintf("# Market Opportunity\n\n![Market Diagram width:50px](%s)", diagram),
			1,
		)
	}

	if team, ok := imagePaths["team"]; ok {
		markdown = strings.Replace(
			markdown,
			"# Our Team",
			fmt.Sprintf("# Our Team\n\n![Team Photo width:400px](%s)", team),
			1,
		)
	}

	return markdown
}

func (s *PitchDeckService) convertToPDF(mdPath, pdfPath, theme string) error {
	args := []string{
		"@marp-team/marp-cli",
		mdPath,
		"--pdf",
		"--output", pdfPath,
		"--theme", theme,
		"--allow-local-files",
	}
	cmd := exec.Command("npx", args...)
	return cmd.Run()
}

func (s *PitchDeckService) convertToHTML(mdPath, htmlPath, theme string) error {
	args := []string{
		"@marp-team/marp-cli",
		mdPath,
		"--html",
		"--output", htmlPath,
		"--theme", theme,
		"--allow-local-files",
	}
	cmd := exec.Command("npx", args...)
	return cmd.Run()
}

func (s *PitchDeckService) UploadImage(filePath string) (string, error) {
	// Generate unique filename for storage
	fileName := "images/" + filepath.Base(filePath)

	// Upload to storage
	url, err := s.storage.UploadFile(filePath, "pitch-decks", fileName)
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %w", err)
	}

	return url, nil
}
