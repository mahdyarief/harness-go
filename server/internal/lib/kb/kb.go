// Package kb turns an uploaded file into searchable text. It is pure: bytes in,
// segments and chunks out, with no database, disk, network or clock. Storing the
// file and indexing the result belong to the callers.
package kb

import (
	"context"
	"errors"
)

// Format is the canonical, lower-case file kind. It is decided from the file's
// content, never trusted from the client's extension or Content-Type alone.
type Format string

const (
	FormatMarkdown Format = "md"
	FormatText     Format = "txt"
	FormatCSV      Format = "csv"
	FormatDOCX     Format = "docx"
	FormatXLSX     Format = "xlsx"
	FormatPDF      Format = "pdf"
)

// Limits shared by every extractor. They bound hostile input: an upload is
// untrusted bytes that a parser will walk.
const (
	// MaxUploadBytes is the largest accepted file. It sits under the server's
	// 4 MiB body limit to leave room for multipart overhead.
	MaxUploadBytes = 3 << 20
	// MaxExtractedChars caps the text one document may yield.
	MaxExtractedChars = 2_000_000
	// MaxSegments and MaxChunks bound per-document indexing work.
	MaxSegments = 4096
	MaxChunks   = 4096
	// MaxIndexedBytes caps aggregate metadata and emitted chunk bytes.
	MaxIndexedBytes = 16 << 20
	// MaxDecompressedBytes caps the total uncompressed size of a zip container
	// (docx, xlsx), the defence against zip bombs.
	MaxDecompressedBytes = 32 << 20
)

var (
	// ErrUnsupportedFormat means the content is not one of the accepted formats.
	ErrUnsupportedFormat = errors.New("kb: unsupported file format")
	// ErrCorrupt means the file claims a format but cannot be parsed as one.
	ErrCorrupt = errors.New("kb: file is corrupt or unreadable")
	// ErrTooLarge means the file, or the text extracted from it, exceeds a limit.
	ErrTooLarge = errors.New("kb: file exceeds the size limit")
	// ErrEmpty means the file parsed but contained no text (for example a scanned
	// PDF with no text layer).
	ErrEmpty = errors.New("kb: file contains no extractable text")
)

// Segment is a unit of extracted text with the context needed to cite it. An
// extractor emits one segment per natural division of its format: a Markdown
// section, a spreadsheet sheet, a PDF page.
type Segment struct {
	// Heading is the section path, outermost first. It may be empty.
	Heading []string
	// Locator pins the segment in the source for citation, in Indonesian like
	// the rest of the user-facing text: "halaman 3", "sheet Harga", "baris 12-40".
	Locator string
	Text    string
}

// Extractor reads one format. Implementations must be pure, honour ctx
// cancellation, never panic on hostile input (recover and return ErrCorrupt),
// and enforce MaxExtractedChars and, for containers, MaxDecompressedBytes.
type Extractor interface {
	Format() Format
	Extract(ctx context.Context, data []byte) ([]Segment, error)
}

// ExtractionMetadata describes detected limitations, not a guarantee that every
// feature of a container format is supported. Coverage counts parser work, not
// emitted segments; a nil Total means the denominator was not observed.
type ExtractionMetadata struct {
	Status           string               `json:"status"`
	ExtractorVersion string               `json:"extractor_version"`
	Warnings         []string             `json:"warnings"`
	Coverage         []ExtractionCoverage `json:"coverage"`
}

type ExtractionCoverage struct {
	Unit      string `json:"unit"`
	Scope     string `json:"scope"`
	Processed int    `json:"processed"`
	Total     *int   `json:"total"`
}

const extractorVersion = "kb-v1"

func newExtractionMetadata() ExtractionMetadata {
	return ExtractionMetadata{
		Status: "complete", ExtractorVersion: extractorVersion,
		Warnings: []string{}, Coverage: []ExtractionCoverage{},
	}
}

// Only fixed codes are added by built-ins. Bound and deduplicate even when a
// document repeats the same unsupported feature millions of times.
func (m *ExtractionMetadata) warn(code string) {
	if m == nil {
		return
	}
	for _, existing := range m.Warnings {
		if existing == code {
			return
		}
	}
	m.Status = "limited"
	if len(m.Warnings) < 32 {
		m.Warnings = append(m.Warnings, code)
	}
}

func (m *ExtractionMetadata) cover(unit, scope string, processed int, total *int) {
	if m != nil {
		m.Coverage = append(m.Coverage, ExtractionCoverage{unit, scope, processed, total})
	}
}

// extractionResult never lets partial output or metadata escape on failure.
func extractionResult(segs []Segment, meta ExtractionMetadata, err error) ([]Segment, ExtractionMetadata, error) {
	if err != nil {
		return nil, ExtractionMetadata{}, err
	}
	return segs, meta, nil
}

// Chunk is one indexable passage. Position is the zero-based order within the
// document and is stable for a given input.
type Chunk struct {
	Position int
	// HeadingPath joins Segment.Heading with " > " for display and ranking.
	HeadingPath string
	Locator     string
	Content     string
}
