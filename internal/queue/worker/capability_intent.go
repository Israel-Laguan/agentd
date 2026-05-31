package worker

import (
	"regexp"
	"strings"

	"agentd/internal/models"
)

// Capability intent categories for external routing.
const (
	IntentGenerateImage  = "generate_image"
	IntentRealTimeSearch = "real_time_search"
	IntentBrowseURL      = "browse_url"
	IntentSpreadsheetOps = "spreadsheet_ops"
)

var (
	generateImageSignals  = []string{"image", "picture", "logo", "illustration", "render", "draw"}
	realTimeSearchSignals = []string{"latest", "today", "current", "news", "real-time", "search the web"}
	browseURLSignals      = []string{"url", "http", "https", "browse", "fetch page", "scrape"}
	spreadsheetOpsSignals = []string{"spreadsheet", "excel", "csv", "sheet", "pivot"}

	capabilityIntentSignals  map[string][]string
	capabilityIntentMatchers map[string]map[string]*regexp.Regexp
)

func init() {
	capabilityIntentSignals = map[string][]string{
		IntentGenerateImage:  generateImageSignals,
		IntentRealTimeSearch: realTimeSearchSignals,
		IntentBrowseURL:      browseURLSignals,
		IntentSpreadsheetOps: spreadsheetOpsSignals,
	}
	capabilityIntentMatchers = make(map[string]map[string]*regexp.Regexp, len(capabilityIntentSignals))
	for intent, keywords := range capabilityIntentSignals {
		matchers := make(map[string]*regexp.Regexp, len(keywords))
		for _, kw := range keywords {
			matchers[kw] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
		}
		capabilityIntentMatchers[intent] = matchers
	}
}

// IntentClassification is the output of keyword-based capability intent classification.
type IntentClassification struct {
	Intent     string
	Confidence float64
	Scores     map[string]int
}

// IntentClassifier scores task text into capability intent categories.
type IntentClassifier struct {
	minConfidence float64
}

// NewIntentClassifier returns a classifier with the given minimum confidence threshold.
func NewIntentClassifier(minConfidence float64) *IntentClassifier {
	return &IntentClassifier{minConfidence: minConfidence}
}

// Classify scores task title and description into an intent with confidence.
func (c *IntentClassifier) Classify(task models.Task) IntentClassification {
	text := strings.ToLower(task.Title + " " + task.Description)
	scores := make(map[string]int, len(capabilityIntentSignals))
	for intent, keywords := range capabilityIntentSignals {
		for _, kw := range keywords {
			scores[intent] += countCapabilityIntentKeyword(text, intent, kw)
		}
	}

	topIntent, topScore, secondScore := topTwoCapabilityScores(scores)
	confidence := 0.0
	if topScore > 0 {
		confidence = float64(topScore-secondScore) / float64(topScore)
		if confidence > 1 {
			confidence = 1
		}
	}

	if topScore == 0 || topScore == secondScore || confidence < c.minConfidence {
		return IntentClassification{Confidence: confidence, Scores: scores}
	}
	return IntentClassification{
		Intent:     topIntent,
		Confidence: confidence,
		Scores:     scores,
	}
}

func countCapabilityIntentKeyword(text, intent, kw string) int {
	matchers := capabilityIntentMatchers[intent]
	if matchers == nil {
		return 0
	}
	re := matchers[kw]
	if re == nil {
		return 0
	}
	return len(re.FindAllStringIndex(text, -1))
}

func topTwoCapabilityScores(scores map[string]int) (topIntent string, top, second int) {
	for intent, score := range scores {
		if score > top {
			second = top
			top = score
			topIntent = intent
		} else if score > second {
			second = score
		}
	}
	return topIntent, top, second
}
