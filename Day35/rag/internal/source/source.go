// Package source abstracts where documents to index come from: the local
// filesystem, a wiki, or anything else that can enumerate documents.
package source

import "context"

// Document is one indexable unit of content.
type Document struct {
	// ID is the stable identity of the document and becomes ChunkMeta.Source.
	// It must survive a rename: an absolute path for files, a page id for wiki
	// pages (never a slug).
	ID string
	// URL is the public link a user can follow ("" when there is none).
	URL string
	// Title is the human-readable name of the document.
	Title string
	// Path is the display location — a file path or a wiki slug.
	Path string
	// Content is the plain-text/markdown body.
	Content string
	// Format is the logical content format: "markdown" | "text" | "pdf".
	Format string
	// Version is an opaque revision marker (mtime, revision id, hash). The
	// indexer compares it against the stored state to skip unchanged documents.
	Version string
}

// Source enumerates the documents to index.
type Source interface {
	// Iterate calls fn once per document, in an unspecified order.
	// Documents rejected by the source's own filtering or access checks are not
	// passed to fn at all. An error returned by fn aborts the walk and is
	// returned to the caller.
	Iterate(ctx context.Context, fn func(Document) error) error
}
