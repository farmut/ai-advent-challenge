package reader

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Read returns the plain-text content of a file.
// Supported extensions: .txt, .md / .markdown, .pdf
func Read(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt":
		return readTxt(path)
	case ".md", ".markdown":
		return readMarkdown(path)
	case ".pdf":
		return readPDF(path)
	default:
		return "", fmt.Errorf("unsupported file extension %q", ext)
	}
}
