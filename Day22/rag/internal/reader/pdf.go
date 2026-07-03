package reader

import (
	"strings"

	"github.com/gen2brain/go-fitz"
)

// readPDF extracts plain text from a PDF file using go-fitz (MuPDF wrapper).
// MuPDF correctly handles CID/Type0 fonts (Cyrillic, CJK, etc.) and preserves
// word spacing, unlike pure-Go alternatives that return glyph advances as zero.
//
// License note: go-fitz / MuPDF is distributed under AGPL-3.
func readPDF(path string) (string, error) {
	doc, err := fitz.New(path)
	if err != nil {
		return "", err
	}
	defer doc.Close()

	var sb strings.Builder

	for n := 0; n < doc.NumPage(); n++ {
		text, err := doc.Text(n)
		if err != nil {
			continue
		}
		sb.WriteString(text)
		sb.WriteByte('\n')
	}

	return strings.TrimSpace(sb.String()), nil
}
