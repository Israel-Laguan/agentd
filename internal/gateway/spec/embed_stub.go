package spec

import "context"

// NoopEmbed satisfies AIGateway.Embed for test doubles that do not use embeddings.
func NoopEmbed(context.Context, EmbedRequest) (EmbedResponse, error) {
	return EmbedResponse{}, nil
}
