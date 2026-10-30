package stage

import (
	"context"
	"encoding/json"
	"fmt"

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
	_ = zero
	c.decode = func(ctx context.Context, gw spec.AIGateway, req spec.AIRequest) (json.RawMessage, int, spec.UsageDetails, error) {
		v, tokens, details, err := correction.GenerateJSONWithUsage[marked[T]](ctx, gw, req)
		if err != nil {
			return nil, tokens, details, err
		}
		if v.needsHuman {
			return v.raw, tokens, details, nil
		}
		raw, err := json.Marshal(v.value)
		return raw, tokens, details, err
	}
	return c
}

// marked decodes T and also reads the needs_human marker, which T does not
// declare. Without it the typed decode and re-marshal would drop the marker, so
// a stage could never ask for a person. A marked reply is a request for a human,
// not an answer: it skips T's validation and is passed on exactly as sent.
type marked[T any] struct {
	value      T
	needsHuman bool
	raw        json.RawMessage
}

func (m *marked[T]) UnmarshalJSON(b []byte) error {
	var marker struct {
		NeedsHuman bool `json:"needs_human"`
	}
	// A reply that is not a JSON object carries no marker; T decides if it is valid.
	if json.Unmarshal(b, &marker) == nil && marker.NeedsHuman {
		m.needsHuman = true
		m.raw = append(json.RawMessage(nil), b...)
		return nil
	}
	return json.Unmarshal(b, &m.value)
}

// Validate runs T's validation unless the reply asked for a person.
func (m *marked[T]) Validate() error {
	if m.needsHuman {
		return nil
	}
	if v, ok := any(m.value).(spec.Validatable); ok {
		return v.Validate()
	}
	if v, ok := any(&m.value).(spec.Validatable); ok {
		return v.Validate()
	}
	return nil
}

// SchemaHint repeats T's hint, so corrective retries still show the model the
// shape it must return.
func (marked[T]) SchemaHint() string {
	var zero T
	if h, ok := any(zero).(interface{ SchemaHint() string }); ok {
		return h.SchemaHint()
	}
	if h, ok := any(&zero).(interface{ SchemaHint() string }); ok {
		return h.SchemaHint()
	}
	return ""
}

// Decode runs req through gw and returns the validated JSON with the token
// usage of every attempt, retries included.
func (c Contract) Decode(ctx context.Context, gw spec.AIGateway, req spec.AIRequest) (json.RawMessage, int, spec.UsageDetails, error) {
	if c.decode == nil {
		return nil, 0, spec.UsageDetails{}, fmt.Errorf("stage: contract is zero value")
	}
	return c.decode(ctx, gw, req)
}
