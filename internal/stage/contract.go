package stage

import (
	"context"
	"encoding/json"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
)

// Contract is the JSON shape a stage must return. It wraps the existing
// correction.GenerateJSONWithUsage mechanism, so decoding, Validate() and the
// corrective retries are the ones every JSON call already uses.
type Contract struct {
	hint   string
	decode func(context.Context, spec.AIGateway, spec.AIRequest) (json.RawMessage, int, spec.UsageDetails, error)
}

// NewContract builds the Contract for T. T needs Validate() and may provide
// SchemaHint(), which corrective retries repeat back to the model.
func NewContract[T any]() Contract {
	var zero T
	c := Contract{}
	if h, ok := any(&zero).(interface{ SchemaHint() string }); ok {
		c.hint = h.SchemaHint()
	}
	c.decode = func(ctx context.Context, gw spec.AIGateway, req spec.AIRequest) (json.RawMessage, int, spec.UsageDetails, error) {
		v, tokens, details, err := correction.GenerateJSONWithUsage[T](ctx, gw, req)
		if err != nil {
			return nil, tokens, details, err
		}
		raw, err := json.Marshal(v)
		return raw, tokens, details, err
	}
	return c
}

// Decode runs req through gw and returns the validated JSON with the token
// usage of every attempt, retries included.
func (c Contract) Decode(ctx context.Context, gw spec.AIGateway, req spec.AIRequest) (json.RawMessage, int, spec.UsageDetails, error) {
	return c.decode(ctx, gw, req)
}
