package routing

import "agentd/internal/gateway/spec"

// withAnsweringModel records the model that answered in the response's usage
// details, so TOKEN_USAGE names it even when a proxy such as LiteLLM fell back
// to a different model than the one requested (SP-021, B-054). It copies the
// details instead of writing through the provider's pointer.
func withAnsweringModel(resp spec.AIResponse) spec.AIResponse {
	if resp.ModelUsed == "" {
		return resp
	}
	details := spec.UsageDetails{}
	if resp.UsageDetails != nil {
		details = *resp.UsageDetails
	}
	details.Model = resp.ModelUsed
	resp.UsageDetails = &details
	return resp
}
