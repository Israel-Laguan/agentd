package worker

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	unsupportedPDFMarker   = "<!-- agentd: unsupported format (pdf conversion failed) -->"
	unsupportedPPTXMarker  = "<!-- agentd: unsupported format (pptx) -->"
	unsupportedImageMarker = "<!-- agentd: unsupported format (image) -->"
	embedSnippetTokens     = 500
)

// ConverterFunc converts a file at fullPath to markdown.
type ConverterFunc func(ctx context.Context, fullPath string, raw []byte) (string, error)

// FileConverter turns workspace files into markdown.
type FileConverter struct {
	convert ConverterFunc
}

func NewFileConverter() *FileConverter {
	return &FileConverter{convert: defaultConvert}
}

func NewFileConverterWith(fn ConverterFunc) *FileConverter {
	if fn == nil {
		fn = defaultConvert
	}
	return &FileConverter{convert: fn}
}

func defaultConvert(ctx context.Context, fullPath string, raw []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(fullPath))
	switch ext {
	case ".pdf":
		return convertPDF(ctx, fullPath)
	case ".pptx", ".ppt":
		return unsupportedPPTXMarker, nil
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff", ".tif":
		return unsupportedImageMarker, nil
	default:
		return string(raw), nil
	}
}

func convertPDF(ctx context.Context, fullPath string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return unsupportedPDFMarker, nil
	}
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", fullPath, "-")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return unsupportedPDFMarker, nil
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return unsupportedPDFMarker, nil
	}
	return text, nil
}
