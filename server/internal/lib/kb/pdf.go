package kb

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// pdfCtxEvery is how many text glyphs pass between ctx checks. The library
// walks content streams synchronously, so a coarse check is enough to keep
// cancellation responsive on hostile input.
const pdfCtxEvery = 2048

// pdfMaxPages bounds how many pages are read. A PDF with more pages than
// this is rejected before any row is walked.
const pdfMaxPages = 4096

// pdfMaxGlyphs caps the total number of glyphs the extractor will walk
// across all pages. Real documents stay well under this; hostile content
// must not be allowed to balloon memory.
const pdfMaxGlyphs = 2_000_000

// pdfSpaceMin is the horizontal gap (in points) between two consecutive
// glyphs on the same baseline that signals a space. It sits above normal
// kerning and below the advance of a word space in body text.
const pdfSpaceMin = 1.5

// pdfYTolerance is the maximum vertical distance (in points) between two
// glyphs that share a baseline. 0.5 points tolerates subpoint rounding
// without merging distinct rows.
const pdfYTolerance = 0.5

type pdfExtractor struct{}

// pdfWord is one recovered word on a baseline. end is the estimated right
// edge used to decide whether the next glyph starts a new word.
type pdfWord struct {
	x   float64
	s   string
	end float64
}

// pdfRow is one visual line of text on a page.
type pdfRow struct {
	y     float64
	words []pdfWord
}

// NewPDFExtractor reads a PDF's text streams with a pure-Go library and
// reconstructs word and line layout from glyph positions. The library emits
// one Text per character of a Tj/TJ string run; subset fonts report W=0, so
// the horizontal advance is derived from the next glyph's X for words and
// from X+W for fonts that carry widths.
func NewPDFExtractor() Extractor { return pdfExtractor{} }

func (pdfExtractor) Format() Format { return FormatPDF }

func (pdfExtractor) Extract(ctx context.Context, data []byte) ([]Segment, error) {
	return pdfExtractor{}.extract(ctx, data, nil)
}

func (pdfExtractor) ExtractWithMetadata(ctx context.Context, data []byte) ([]Segment, ExtractionMetadata, error) {
	meta := newExtractionMetadata()
	segs, err := pdfExtractor{}.extract(ctx, data, &meta)
	return extractionResult(segs, meta, err)
}

func (pdfExtractor) extract(ctx context.Context, data []byte, meta *ExtractionMetadata) (segs []Segment, err error) {
	defer func() {
		if r := recover(); r != nil {
			segs, err = nil, fmt.Errorf("%w: %v", ErrCorrupt, r)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	if len(data) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	numPages := r.NumPage()
	if numPages > pdfMaxPages {
		return nil, fmt.Errorf("%w: %d pages", ErrTooLarge, numPages)
	}
	processedPages := 0
	charsLeft := MaxExtractedChars
	for i := 1; i <= numPages; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c := r.Page(i).Content()
		if len(c.Text) > pdfMaxGlyphs {
			return nil, fmt.Errorf("%w: %d glyphs on page %d", ErrTooLarge, len(c.Text), i)
		}
		text, err := pdfPageText(ctx, c.Text)
		if err != nil {
			return nil, err
		}
		if text != "" {
			runeCount := utf8.RuneCountInString(text)
			if runeCount > charsLeft {
				return nil, ErrTooLarge
			}
			if len(segs) >= MaxSegments {
				return nil, ErrTooLarge
			}
			charsLeft -= runeCount
			segs = append(segs, Segment{Locator: fmt.Sprintf("halaman %d", i), Text: text})
		} else if meta != nil {
			// A page without a text layer is usually a scan; the rest of
			// the document may still index, but the gap must be visible.
			meta.warn("pdf_page_without_text")
		}
		processedPages++
	}
	if meta != nil && processedPages > 0 {
		meta.cover("page", "document", processedPages, &numPages)
	}
	if len(segs) == 0 {
		if meta != nil {
			meta.warn("pdf_no_text")
		}
		return nil, ErrEmpty
	}
	return segs, nil
}

// pdfPageText reconstructs the visual reading order of one page: glyphs are
// grouped into rows by baseline (PDF Y grows upward, so rows sort
// descending), words within a row are separated by horizontal gap, and a
// wide gap is treated as a line break inside the row.
func pdfPageText(ctx context.Context, glyphs []pdf.Text) (string, error) {
	if len(glyphs) == 0 {
		return "", nil
	}
	// Group glyphs into baselines (rows). The library emits one Text per
	// character and may interleave the runs of a baseline, so glyphs are
	// bucketed by baseline first and assembled per row afterwards.
	rowOf := make(map[float64]*pdfRow, 16)
	var order []float64
	for i, g := range glyphs {
		if i%pdfCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		switch g.S {
		case "", "\t", "\n":
			// The library appends a "\n" glyph after every TJ run; it is a
			// no-op here and is dropped rather than turned into a space.
			continue
		case " ":
			// A real space glyph inside a string. Attach it to the current
			// word on its baseline, suppressing the positional gap check so
			// the same space is not inserted twice.
			if r := baselineFor(rowOf, g.Y); r != nil && len(r.words) > 0 {
				r.words[len(r.words)-1].s += " "
			}
			continue
		}
		end := g.X
		if g.W > 0 {
			end = g.X + g.W
		}
		r := baselineFor(rowOf, g.Y)
		if r == nil {
			order = append(order, g.Y)
			rowOf[g.Y] = &pdfRow{y: g.Y, words: []pdfWord{{x: g.X, s: g.S, end: end}}}
			continue
		}
		if n := len(r.words); n > 0 {
			prev := &r.words[n-1]
			if g.X-prev.end > pdfSpaceMin {
				// New word on the same baseline.
				r.words = append(r.words, pdfWord{x: g.X, s: g.S, end: end})
				continue
			}
			// Continue the current word: append and extend its right edge.
			prev.s += g.S
			prev.end = end
			continue
		}
		r.words = append(r.words, pdfWord{x: g.X, s: g.S, end: end})
	}

	sort.SliceStable(order, func(a, b int) bool { return order[a] > order[b] })

	var b strings.Builder
	for ri, y := range order {
		if ri > 0 {
			b.WriteByte('\n')
		}
		for wi, w := range rowOf[y].words {
			if wi > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strings.TrimRight(w.s, " "))
		}
	}
	return pdfCleanText(b.String()), nil
}

// baselineFor returns the row for a baseline within tolerance, creating it if
// the nearest existing baseline is within pdfYTolerance.
func baselineFor(rowOf map[float64]*pdfRow, y float64) *pdfRow {
	if r, ok := rowOf[y]; ok {
		return r
	}
	for ey, r := range rowOf {
		if absDelta(ey, y) <= pdfYTolerance {
			return r
		}
	}
	return nil
}

// pdfCleanText drops the replacement codepoints and control characters that
// the decoder emits for spacing glyphs inside TJ arrays, and trims edges.
func pdfCleanText(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			// Drop replacement codepoints used as spacers.
			continue
		case r == '\u00A0':
			b.WriteByte(' ')
		case unicode.IsControl(r) && r != '\n':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func absDelta(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
