package model

import "time"

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
	DevelopmentPlan string `json:"developmentPlan"`
	MarketSize      string `json:"marketSize"`

	// Step 4: Fundraising & Investment Details
	FundingAmount       string `json:"fundingAmount"`
	FundingUse          string `json:"fundingUse"`
	Valuation           string `json:"valuation"`
	InvestmentStructure string `json:"investmentStructure"`

	// Step 5: Market Opportunity
	TAM          string `json:"tam"`
	SAM          string `json:"sam"`
	SOM          string `json:"som"`
	TargetNiche  string `json:"targetNiche"`
	MarketTrends string `json:"marketTrends"`
	Industry     string `json:"industry"`

	// Step 6: Team & Experience
	WhyYou            string       `json:"whyYou"`
	TeamMembers       []TeamMember `json:"teamMembers"`
	TeamQualification string       `json:"teamQualification"`
	ContactInfo       ContactInfo  `json:"contactInfo"`
	KeyTakeaways      string       `json:"keyTakeaways"`

	// Images
	CompanyLogo string `json:"companyLogo"`
	TeamPhoto   string `json:"teamPhoto"`
	Diagram     string `json:"diagram"`

	// Theme Selection
	Theme string `json:"theme"`
}

type TeamMember struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	Experience string `json:"experience"`
}

type ContactInfo struct {
	Email    string `json:"email"`
	Linkedin string `json:"linkedin"`
	Socials  string `json:"socials"`
}

type PitchDeckService interface {
	Create(data PitchDeckData, userID string) (*PitchDeckInfo, error)
	Get(deckID string) (*PitchDeckInfo, error)
	UpdateVisibility(deckID string, userID string, isPublic bool) error
	ListUserDecks(userID string) ([]PitchDeckInfo, error)
	UpdateStatus(deckID string, status string) error
	UploadImage(filePath string) (string, error)
}

type StorageService interface {
	UploadFile(filePath, bucketName, fileName string) (string, error)
	DownloadFile(url string, destPath string) error
}
