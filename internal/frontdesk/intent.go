package frontdesk

import (
	"context"
	"strings"

	"agentd/internal/gateway"
)

// InputFile represents a file attachment from an inbound chat request.
type InputFile struct {
	Name    string
	Path    string
	Content string
}

// FileRef is a stashed file reference used during planning.
type FileRef struct {
	Name string
	Path string
}

// LastUserMessage returns the content of the most recent "user" message.
func LastUserMessage(messages []gateway.PromptMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

// PrepareIntent stashes oversized content and resolves file references,
// returning the processed intent text and any file refs for downstream use.
func PrepareIntent(stash *FileStash, message string, files []InputFile) (string, []FileRef, error) {
	intent := message
	if stash == nil {
		return intent, nil, nil
	}

	var refs []FileRef
	if path, err := stash.Stash(intent); err != nil {
		return "", nil, err
	} else if path != "" {
		refs = append(refs, FileRef{Name: "user-message.txt", Path: path})
		intent = "User message was too large and was saved as a file reference."
	}

	for _, file := range files {
		ref, err := prepareFileRef(stash, file)
		if err != nil {
			return "", nil, err
		}
		if ref.Path != "" {
			refs = append(refs, ref)
		}
	}
	if len(refs) > 0 {
		intent = FormatFileReferenceIntent(intent, refs)
	}
	return intent, refs, nil
}

func prepareFileRef(stash *FileStash, file InputFile) (FileRef, error) {
	name := strings.TrimSpace(file.Name)
	if name == "" {
		name = "attachment.txt"
	}
	if file.Path != "" {
		return FileRef{Name: name, Path: strings.TrimSpace(file.Path)}, nil
	}
	if file.Content == "" {
		return FileRef{}, nil
	}
	path, err := stash.Stash(file.Content)
	if err != nil {
		return FileRef{}, err
	}
	if path == "" {
		path, err = stash.Write(name, file.Content)
		if err != nil {
			return FileRef{}, err
		}
	}
	return FileRef{Name: name, Path: path}, nil
}

// FormatFileReferenceIntent appends stashed-file metadata to the intent text.
func FormatFileReferenceIntent(intent string, refs []FileRef) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(intent))
	for _, ref := range refs {
		b.WriteString("\n\n[agentd file reference]\n")
		if ref.Name != "" {
			b.WriteString("name: ")
			b.WriteString(ref.Name)
			b.WriteString("\n")
		}
		b.WriteString("path: ")
		b.WriteString(ref.Path)
	}
	return b.String()
}

// IntentWithFileContents reads stashed files and appends their (possibly
// truncated) contents to the planning intent.
func IntentWithFileContents(
	ctx context.Context,
	stash *FileStash,
	truncator gateway.Truncator,
	budget int,
	intent string,
	files []FileRef,
) (string, error) {
	if stash == nil {
		return intent, nil
	}
	if truncator == nil {
		truncator = gateway.StrategyTruncator{Strategy: gateway.MiddleOutStrategy{}}
	}
	var b strings.Builder
	b.WriteString(intent)
	for _, file := range files {
		content, err := stash.Read(file.Path, gateway.MiddleOutStrategy{}, 0)
		if err != nil {
			return "", err
		}
		truncated, err := truncator.Apply(ctx, []gateway.PromptMessage{{Role: "user", Content: content}}, budget)
		if err != nil {
			return "", err
		}
		if len(truncated) > 0 {
			content = truncated[0].Content
		}
		b.WriteString("\n\n[agentd file content]\n")
		if file.Name != "" {
			b.WriteString("name: ")
			b.WriteString(file.Name)
			b.WriteString("\n")
		}
		b.WriteString("path: ")
		b.WriteString(file.Path)
		b.WriteString("\ncontent:\n")
		b.WriteString(content)
	}
	return b.String(), nil
}
