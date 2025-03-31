package prompts

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

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

// Templates for different prompt types
const (
	slideGenerationTemplate = `
You are an expert presentation designer specializing in Marp markdown presentations. Create a professional pitch deck using the following information:

**PROJECT OVERVIEW**

- **Project Information**
  - Project Name: {{.ProjectName}}
  - Big Idea: {{.BigIdea}}

- **Market Analysis**
  - Problem: {{.Problem}}
  - Target Audience: {{.TargetAudience}}
  - Existing Solutions: {{.ExistingSolutions}}

- **Solution Details**
  - Solution: {{.Solution}}
  - Technology: {{.Technology}}
  - Differentiators: {{.Differentiators}}
  - Development Plan: {{.DevelopmentPlan}}

- **Investment Information**
  - Funding Amount: {{.FundingAmount}}
  - Funding Use: {{.FundingUse}}
  - Valuation: {{.Valuation}}
  - Investment Structure: {{.InvestmentStructure}}

- **Market Opportunity**
  - TAM: {{.TAM}}
  - SAM: {{.SAM}}
  - SOM: {{.SOM}}
  - Target Niche: {{.TargetNiche}}
  - Market Trends: {{.MarketTrends}}
  - Industry: {{.Industry}}

- **Team Information**
  - Why You: {{.WhyYou}}
  - Team Members: {{.TeamMembers}}
  - Team Qualification: {{.TeamQualification}}

- **Business Model**
  - Revenue Model: {{.RevenueModel}}
  - Scaling Plan: {{.ScalingPlan}}
  - GTM Strategy: {{.GTMStrategy}}

- **Traction & Milestones**
  - Achievements: {{.Achievements}}
  - Next Milestones: {{.NextMilestones}}

- **Contact Information**
  - Email: {{.ContactInfo.Email}}
  - LinkedIn: {{.ContactInfo.LinkedIn}}
  - Other Socials: {{.ContactInfo.Socials}}
  - Key Takeaways: {{.KeyTakeaways}}

**PRESENTATION REQUIREMENTS:**

1. Use this Marp structure and place the logo in the top right corner of each slide:
---
marp: true
theme: {{.Theme}}
paginate: true
backgroundColor: {{.BackgroundColor}}
color: {{.TextColor}}
---

<style>
  section {
    position: relative;
  }

  .top-right-logo {
    position: absolute;
    top: 20px;
    right: 20px;
    width: 80px;
    z-index: 1000;
  }
</style>

<div class="top-right-logo">
  <img src="{{.LogoPath}}" alt="Logo" width="80">
</div>

2. Create 10-13 slides following this structure:
   - Problem & Market Need (emphasize pain points and market size)
   - Solution & Value Proposition (highlight unique selling points)
   - Market Opportunity (visualize with TAM, SAM, SOM funnel), ![w:400]({{.DiagramPhotoPath}})
   - Competitive Landscape (position your solution)
   - Product/Technology Overview (emphasize differentiators)
   - Business Model & Go-to-Market Strategy
   - Team & Expertise (showcase qualifications), ![w:60]({{.TeamPhotoPath}})
   - Traction & Milestones (past achievements and future roadmap)
   - Funding Ask & Use of Funds
   - Call to Action & Contact Information

**IMPORTANT GUIDELINES:**

1. Always begin with a short title slide with a title, a brief description, and the author's name (if provided, use CEO). The title should be an H1 header, the description should be regular text, and the author's name should be regular text.
2. Ensure that the content on each slide fits inside the slide. Never create paragraphs.
3. Always use bullet points and other formatting options to make the content more readable. (don't use fragment)
4. Prefer multi-line code blocks over inline code blocks for any code longer than a few words. Even if the code is a single line, use a multi-line code block.
5. Do not end with --- (three dashes) on a new line, as this will end the presentation with an empty slide.
6. Use bold (**text**) for emphasis and italics (*text*) for secondary emphasis.
7. Create visual hierarchies with indentation and spacing.
8. Use tables for structured data comparisons (market analysis, competitive landscape).
9. Use blockquotes (> text) for customer testimonials or important statements.

---
`
	// Example Marp themes with their specific properties
	defaultTheme = `
---
marp: true
theme: default
paginate: true
backgroundColor: white
color: black
header: '![right:20 w:80]({{.LogoPath}})'
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
header: '![right:20 w:80]({{.LogoPath}})'
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
header: '![right:20 w:80]({{.LogoPath}})'
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
header: '![right:20 w:80]({{.LogoPath}})'
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
		data.LogoPath = "./logo.png"
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
