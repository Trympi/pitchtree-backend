package prompts

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Templates for different prompt types
const (
	// Template for slide generation prompt
	slideGenerationTemplate = `You are an expert presentation designer specializing in creating professional Marp markdown presentations. Your task is to transform the provided content into a compelling, well-structured pitch deck.

	Create a complete Marp markdown presentation using the following information:

	-- Project Information --
	Project Name: {{.ProjectName}}
	Big Idea: {{.BigIdea}}

	-- Market Analysis --
	Problem: {{.Problem}}
	Target Audience: {{.TargetAudience}}
	Existing Solutions: {{.ExistingSolutions}}

	-- Solution Details --
	Solution: {{.Solution}}
	Technology: {{.Technology}}
	Differentiators: {{.Differentiators}}
	Development Plan: {{.DevelopmentPlan}}

	-- Investment Information --
	Funding Amount: {{.FundingAmount}}
	Funding Use: {{.FundingUse}}
	Valuation: {{.Valuation}}
	Investment Structure: {{.InvestmentStructure}}

	-- Market Opportunity --
	TAM: {{.TAM}}
	SAM: {{.SAM}}
	SOM: {{.SOM}}
	Target Niche: {{.TargetNiche}}
	Market Trends: {{.MarketTrends}}
	Industry: {{.Industry}}

	-- Team Information --
	Why You: {{.WhyYou}}
	Team Members: {{.TeamMembers}}
	Team Qualification: {{.TeamQualification}}

	-- Business Model --
	Revenue Model: {{.RevenueModel}}
	Scaling Plan: {{.ScalingPlan}}
	GTM Strategy: {{.GTMStrategy}}

	-- Traction & Milestones --
	Achievements: {{.Achievements}}
	Next Milestones: {{.NextMilestones}}

	-- Contact Information --
	Email: {{.ContactInfo.Email}}
	LinkedIn: {{.ContactInfo.LinkedIn}}
	Other Socials: {{.ContactInfo.Socials}}
	Key Takeaways: {{.KeyTakeaways}}

	PRESENTATION STRUCTURE GUIDELINES:
	1. Start with a powerful title slide that includes the project name, a compelling tagline derived from the big idea, and a footer with the presenter's name (if provided).
	2. Create a logical flow of slides in this order:
	   - Problem & Market Need (emphasize pain points and market size)
	   - Solution & Value Proposition (highlight unique selling points)
	   - Market Opportunity (visualize with TAM, SAM, SOM funnel)
	   - Competitive Landscape (position your solution)
	   - Product/Technology Overview (emphasize differentiators)
	   - Business Model & Go-to-Market Strategy
	   - Team & Expertise (showcase qualifications)
	   - Traction & Milestones (past achievements and future roadmap)
	   - Funding Ask & Use of Funds
	   - Call to Action & Contact Information

	SLIDE OVERFLOW HANDLING:
	1. Limit text per slide:
	   - Use short phrases instead of sentences.
	   - If a slide is too dense, break it into multiple slides (e.g., "Part 1" & "Part 2").
	2. Utilize Marp slide directives:
	   - <!-- _class: split --> for two-column layouts to reduce overflow.
	   - <!-- _class: invert --> for highlighting essential content.

	FORMATTING GUIDELINES:
	1. Use the following Marp markdown header:
	` + "```" + `
	---
	marp: true
	theme: {{.Theme}}
	paginate: true
	backgroundColor: {{.BackgroundColor}}
	color: {{.TextColor}}
	header: '![w:60]({{.LogoPath}})'
	---
	` + "```" + `

	2. Create visually appealing slides:
	   - Use headers (# for titles, ## for section headers, ### for subsections)
	   - Use bullet points for lists (use "*")
	   - Use bold (**text**) for emphasis and italics (*text*) for secondary emphasis
	   - Create visual hierarchies with indentation and spacing
	   - Use emoji selectively for visual interest 📊 💡 🚀 🎯 💰
	   - Use tables for structured data comparisons (market analysis, competitive landscape)
	   - Use blockquotes (> text) for customer testimonials or important statements

	3. For each slide:
	   - Include a clear, concise title
	   - Limit content to 5-7 bullet points maximum
	   - Use simple, direct language
	   - Avoid paragraphs and long text blocks
	   - Use consistent formatting throughout

	4. Use slide directives for special formatting:
	   - <!-- _class: lead --> for title or section intro slides
	   - <!-- _class: invert --> for slides you want to emphasize
	   - <!-- _class: split --> for side-by-side content where available
	
	5. For images:
		- COMPANY LOGO: Place the company logo in the top right corner of each slide using the header directive in the Marp header
		- COMPANY LOGO: Place the company logo in the top right corner of each slide using the header directive in the Marp header
 		- Solution diagram: Include the solution diagram in the Solution or Technology slide with ![w:500]({{.DiagramPhotoPath}})
		- Team photos: ![w:100]({{.TeamPhotoPath}})

	7. For the title slide, use a larger version of the logo:
	` + "```" + `
	---
	marp: true
	theme: {{.Theme}}
	paginate: false
	backgroundColor: {{.BackgroundColor}}
	color: {{.TextColor}}
	header: '![w:60]({{.LogoPath}})'
	---

	# {{.ProjectName}}
	## *Your compelling tagline here*
	` + "```" + `

	Ensure the presentation is comprehensive yet concise, professional, and visually consistent. Create approximately 10-15 slides total.

	Your complete Marp markdown should be returned without any additional commentary or explanations. Do not include the triple backticks in your final output.`

	// Example Marp themes with their specific properties
	defaultTheme = `
---
marp: true
theme: default
paginate: true
backgroundColor: white
color: black
header: '![right:20 w:60]({{.LogoPath}})'
---
# Title Slide
## Subtitle
Presenter Name

---
## Key Points
- Clean, professional design
- High readability
- Excellent for business presentations
	`

	gaiaTheme = `
---
marp: true
theme: gaia
paginate: true
color: #333
header: '![right:20 w:60]({{.LogoPath}})'
---
# Title Slide
## Subtitle
Presenter Name

---
<!-- _class: lead -->
## Key Points
- Modern, minimalist design
- Excellent typography
- Great for creative presentations
	`

	uncoverTheme = `
---
marp: true
theme: uncover
paginate: true
color: #fff
header: '![right:20 w:60]({{.LogoPath}})'
---
# Title Slide
## Subtitle
Presenter Name

---
<!-- _class: invert -->
## Key Points
- Bold, striking design
- High contrast
- Perfect for impactful presentations
	`

	rosePineTheme = `
---
marp: true
theme: rose-pine
paginate: true
color: #e0def4
header: '![w:60]({{.LogoPath}})'
---
# Title Slide
## Subtitle
Presenter Name

---
## Key Points
- Elegant, soothing color palette
- Excellent for technical presentations
- Reduced eye strain for longer presentations
	`
)

type TeamMemberNew struct {
	Name       string
	Role       string
	Experience string
}

// PitchDeckData contains all the information needed for a pitch deck
type PitchDeckData struct {
	// Project Information
	ProjectName string
	BigIdea     string

	// Market Analysis
	Problem           string
	TargetAudience    string
	ExistingSolutions string

	// Solution Details
	Solution        string
	Technology      string
	Differentiators string
	// CompetitiveAdvantage string
	DevelopmentPlan string
	MarketSize      string

	// Investment Information
	FundingAmount       string
	FundingUse          string
	Valuation           string
	InvestmentStructure string

	// Market Opportunity
	TAM          string
	SAM          string
	SOM          string
	TargetNiche  string
	MarketTrends string
	Industry     string

	// Team Information
	WhyYou            string
	TeamMembers       []TeamMemberNew
	TeamQualification string

	// Business Model
	RevenueModel string
	ScalingPlan  string
	GTMStrategy  string

	// Traction & Milestones
	Achievements   string
	NextMilestones string

	// Contact Information
	ContactInfo struct {
		Email    string
		LinkedIn string
		Socials  string
	}
	KeyTakeaways string

	// Theme and Visual Settings
	Theme           string
	BackgroundColor string
	TextColor       string

	// Image Paths
	LogoPath         string
	TeamPhotoPath    string
	ProductDemoPath  string
	DiagramPhotoPath string
}

// GeneratePitchDeckPrompt creates a prompt for the LLM to generate a pitch deck
func GeneratePitchDeckPrompt(data PitchDeckData) (string, error) {
	// Set default theme if not specified
	if data.Theme == "" {
		data.Theme = "default"
	}

	// Set default colors based on theme
	setThemeDefaults(&data)

	// Handle logo path
	if data.LogoPath == "" {
		data.LogoPath = "./logo.png" // Default placeholder
	}

	// Create the template
	tmpl, err := template.New("pitchDeckPrompt").Parse(slideGenerationTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse pitch deck template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute pitch deck template: %w", err)
	}

	return buf.String(), nil
}

// GetThemeExample returns an example of the specified theme
func GetThemeExample(themeName string) string {
	switch strings.ToLower(themeName) {
	case "gaia":
		return gaiaTheme
	case "uncover":
		return uncoverTheme
	case "rose-pine":
		return rosePineTheme
	default:
		return defaultTheme
	}
}

// setThemeDefaults sets default colors based on the selected theme
func setThemeDefaults(data *PitchDeckData) {
	switch strings.ToLower(data.Theme) {
	case "gaia":
		if data.BackgroundColor == "" {
			data.BackgroundColor = "#fff"
		}
		if data.TextColor == "" {
			data.TextColor = "#333"
		}
	case "uncover":
		if data.BackgroundColor == "" {
			data.BackgroundColor = "#333"
		}
		if data.TextColor == "" {
			data.TextColor = "#fff"
		}
	case "rose-pine":
		if data.BackgroundColor == "" {
			data.BackgroundColor = "#191724"
		}
		if data.TextColor == "" {
			data.TextColor = "#e0def4"
		}
	default: // default theme
		if data.BackgroundColor == "" {
			data.BackgroundColor = "white"
		}
		if data.TextColor == "" {
			data.TextColor = "black"
		}
	}
}

// ProcessTeamMembers formats team member information for the prompt
func ProcessTeamMembers(members []TeamMember) string {
	var sb strings.Builder
	for i, m := range members {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%s (%s): %s", m.Name, m.Role, m.Experience))
	}
	return sb.String()
}

// TeamMember represents a team member's information
type TeamMember struct {
	Name       string
	Role       string
	Experience string
}
