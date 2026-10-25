package intake

import "fmt"

// The result types of the intake stage's model calls. The stage owns them;
// the gateway only carries the raw JSON.

type ScopeAnalysis struct {
	SingleScope bool          `json:"single_scope"`
	Confidence  float64       `json:"confidence"`
	Scopes      []ScopeOption `json:"scopes"`
	Reason      string        `json:"reason"`
}

func (s ScopeAnalysis) Validate() error {
	if !s.SingleScope && len(s.Scopes) == 0 {
		return fmt.Errorf("missing scopes for multi-scope classification")
	}
	if !s.SingleScope {
		for _, sc := range s.Scopes {
			if sc.ID == "" || sc.Label == "" {
				return fmt.Errorf("multi-scope entries require an id and label")
			}
		}
	}
	return nil
}

type ScopeOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// IntentAnalysis is the result of intent classification by the Sales Rep agent.
type IntentAnalysis struct {
	Intent string `json:"intent"`
	Reason string `json:"reason"`
}

func (i IntentAnalysis) Validate() error {
	if i.Intent == "" {
		return fmt.Errorf("missing intent")
	}
	return nil
}
