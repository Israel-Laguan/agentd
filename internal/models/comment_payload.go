package models

import (
	"fmt"
	"strings"
)

// FormatCommentPayload encodes author and body the way kanban persists comment events.
func FormatCommentPayload(author CommentAuthor, body string) string {
	return fmt.Sprintf("%s: %s", author, body)
}

// SplitCommentPayload decodes a comment event payload into author and body.
// Body is trimmed with strings.TrimSpace, matching kanban ListComments behavior.
func SplitCommentPayload(payload string) (author string, body string) {
	author, body, ok := strings.Cut(payload, ":")
	if !ok {
		return "", strings.TrimSpace(payload)
	}
	return strings.TrimSpace(author), strings.TrimSpace(body)
}
