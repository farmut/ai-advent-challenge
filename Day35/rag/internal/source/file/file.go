// Package file implements source.Source over the local filesystem.
package file

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"rag/internal/reader"
	"rag/internal/source"
)

// formats maps a file extension to the logical content format used by the
// chunker. Its key set is also the set of extensions picked up when walking a
// directory — extending it is the single place to add a file type.
var formats = map[string]string{
	".txt":      "text",
	".md":       "markdown",
	".markdown": "markdown",
	".pdf":      "pdf",
}

// Source walks a file or directory and yields one Document per readable file.
type Source struct {
	root string
}

// New returns a Source rooted at path, which may be a single file or a directory.
func New(path string) *Source { return &Source{root: path} }

// Iterate walks the root. A directory is walked recursively and only files with
// a known extension are read; a root that is a single file is always read, so an
// explicitly named file is indexed even if its extension is unusual (reader.Read
// then decides whether it can handle it).
func (s *Source) Iterate(ctx context.Context, fn func(source.Document) error) error {
	info, err := os.Stat(s.root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return s.emit(ctx, s.root, fn)
	}

	return filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := formats[strings.ToLower(filepath.Ext(p))]; !ok {
			return nil
		}
		return s.emit(ctx, p, fn)
	})
}

// emit reads one file and hands the resulting Document to fn.
func (s *Source) emit(ctx context.Context, path string, fn func(source.Document) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	content, err := reader.Read(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	return fn(source.Document{
		ID:      abs, // absolute path: stable id, unchanged by the cwd
		Title:   filepath.Base(abs),
		Path:    path,
		Content: content,
		Format:  formats[strings.ToLower(filepath.Ext(path))],
		Version: version(abs),
	})
}

// version is an opaque change marker: modification time plus size. It is only
// ever compared for equality against a previously stored value.
func version(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", info.ModTime().UTC().UnixNano(), info.Size())
}
