package agentic

import (
	"context"
	"strings"
)

// cutLog collects, for one turn, the layers that shortened the input before
// the gateway saw it. Three layers can cut, in this order: the working zone
// (queue.agentic_context), the character budget with the configured strategy,
// and the agentic head cut. The record carries the names so a report shows
// which layer actually ran.
type cutLog struct{ layers []string }

type cutLogKey struct{}

func withCutLog(ctx context.Context) (context.Context, *cutLog) {
	log := &cutLog{}
	return context.WithValue(ctx, cutLogKey{}, log), log
}

// noteCut records the layer when it shrank the input; a no-op without a log.
func noteCut(ctx context.Context, layer string, before, after int) {
	log, _ := ctx.Value(cutLogKey{}).(*cutLog)
	if log != nil && after < before {
		log.layers = append(log.layers, layer)
	}
}

func (l *cutLog) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(l.layers, ",")
}

func cutsFrom(ctx context.Context) string {
	log, _ := ctx.Value(cutLogKey{}).(*cutLog)
	return log.String()
}
