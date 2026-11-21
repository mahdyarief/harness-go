package kb

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits specific to the DOCX extractor. The shared ones (MaxDecompressedBytes,
// MaxExtractedChars) live in kb.go.
const (
	// docxMaxEntries bounds how many zip entries are inspected. A real document
	// holds tens to a few hundred (media, themes, rels).
	docxMaxEntries = 5000
	// docxMaxDepth bounds XML element nesting. Word output stays well under 40.
	docxMaxDepth = 128
	// docxMaxHeadingRunes keeps a mis-styled giant "heading" from bloating the
	// heading path of every following segment.
	docxMaxHeadingRunes = 200
	// docxMaxStyles bounds the style map built from word/styles.xml.
	docxMaxStyles = 10000
	// docxCtxEvery is how many XML tokens pass between ctx checks.
	docxCtxEvery = 512

	docxNSMain   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	docxNSStrict = "http://purl.oclc.org/ooxml/wordprocessingml/main"
	docxNSMC     = "http://schemas.openxmlformats.org/markup-compatibility/2006"
)

var (
	errDocxTooLarge = errors.New("kb: docx decompressed size over limit")
	errDocxDepth    = errors.New("xml nesting too deep")
	errDocxDirected = errors.New("xml directive (DOCTYPE) not allowed")
)

type docxExtractor struct{}

// NewDOCXExtractor reads word/document.xml with the standard library only:
// paragraphs, tables and heading structure. Headers, footers, comments and
// footnotes are ignored.
func NewDOCXExtractor() Extractor { return docxExtractor{} }

func (docxExtractor) Format() Format { return FormatDOCX }

// docxLimitedReader draws every byte from a budget shared across the entries
// read from one archive. It never trusts the zip header's declared size.
type docxLimitedReader struct {
	r      io.Reader
	budget *int64
}

func (l *docxLimitedReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	*l.budget -= int64(n)
	if *l.budget < 0 {
		return n, errDocxTooLarge
	}
	return n, err
}

func docxNewDecoder(r io.Reader) *xml.Decoder {
	dec := xml.NewDecoder(r)
	dec.Strict = true
	// Empty, non-nil: no custom entity is ever expanded. Only the five
	// predefined XML entities resolve; anything else is a syntax error.
	dec.Entity = map[string]string{}
	return dec
}

func docxIsW(space string) bool { return space == docxNSMain || space == docxNSStrict }

func docxAttr(t xml.StartElement, local string) string {
	for _, a := range t.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// docxUnsafeName flags zip entry names that try to climb out of the archive.
func docxUnsafeName(name string) bool {
	for _, part := range strings.Split(strings.ReplaceAll(name, "\\", "/"), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// docxLevelFromName maps a style name or id to a heading level: 0 for Title,
// 1..9 for Heading1..Heading9, -1 for anything else. The Indonesian "Judul"
// forms are accepted too, since Word localises style ids (the names inside
// styles.xml stay English, which is why styles.xml is consulted first).
func docxLevelFromName(s string) int {
	s = strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s))
	if s == "title" || s == "judul" {
		return 0
	}
	for _, prefix := range []string{"heading", "judul"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok && len(rest) == 1 && rest[0] >= '1' && rest[0] <= '9' {
			return int(rest[0] - '0')
		}
	}
	return -1
}

// docxCleanText trims a paragraph and flattens inner line breaks and tabs when
// flat is set (used for headings and table cells, which must stay one line).
func docxCleanText(s string, flat bool) string {
	s = strings.TrimSpace(s)
	if flat {
		s = strings.Join(strings.Fields(s), " ")
	}
	return s
}

// docxReadStyles builds styleId -> heading level from word/styles.xml. A
// failure other than cancellation or the size budget is not fatal: the caller
// falls back to matching the style id by name.
func docxReadStyles(ctx context.Context, f *zip.File, budget *int64) (map[string]int, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := docxNewDecoder(&docxLimitedReader{r: rc, budget: budget})

	styles := map[string]int{}
	var (
		depth, n  int
		inStyle   bool
		id        string
		nameLevel = -1
		outline   = -1
	)
	for {
		if n++; n%docxCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		tok, err := dec.Token()
		if err == io.EOF {
			return styles, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, errDocxDirected
		case xml.StartElement:
			if depth++; depth > docxMaxDepth {
				return nil, errDocxDepth
			}
			if !docxIsW(t.Name.Space) && t.Name.Space != "w" {
				continue
			}
			switch t.Name.Local {
			case "style":
				inStyle, id, nameLevel, outline = true, docxAttr(t, "styleId"), -1, -1
			case "name":
				if inStyle {
					nameLevel = docxLevelFromName(docxAttr(t, "val"))
				}
			case "outlineLvl":
				if v, err := strconv.Atoi(docxAttr(t, "val")); inStyle && err == nil && v >= 0 && v <= 8 {
					outline = v + 1
				}
			}
		case xml.EndElement:
			depth--
			if (docxIsW(t.Name.Space) || t.Name.Space == "w") && t.Name.Local == "style" && inStyle {
				inStyle = false
				level := nameLevel
				if level < 0 {
					level = outline
				}
				if level >= 0 && id != "" && len(styles) < docxMaxStyles {
					styles[id] = level
				}
			}
		}
	}
}

type docxHeading struct {
	level int
	text  string
}

// docxTable is one level of the table stack. A nested table collapses into the
// cell of its parent so the output stays one line per top-level row.
type docxTable struct {
	rows  []string
	cells []string
	cell  strings.Builder
}

type docxParser struct {
	segs         []Segment
	headings     []docxHeading
	headingTexts []string
	body         strings.Builder
	tables       []*docxTable
	chars        int
	meta         *ExtractionMetadata
}

func (p *docxParser) account(s string) error {
	p.chars += utf8.RuneCountInString(s) + 1
	if p.chars > MaxExtractedChars {
		return ErrTooLarge
	}
	return nil
}

func (p *docxParser) addLine(s string) error {
	if err := p.account(s); err != nil {
		return err
	}
	if p.body.Len() > 0 {
		p.body.WriteByte('\n')
	}
	p.body.WriteString(s)
	return nil
}

func (p *docxParser) flushSection() {
	text := strings.TrimSpace(p.body.String())
	p.body.Reset()
	if text == "" {
		return
	}
	var path []string
	if len(p.headings) > 0 {
		path = make([]string, len(p.headings))
		for i, h := range p.headings {
			path[i] = h.text
		}
	}
	p.segs = append(p.segs, Segment{Heading: path, Text: text})
}

func (p *docxParser) pushHeading(level int, text string) error {
	if r := []rune(text); len(r) > docxMaxHeadingRunes {
		p.meta.warn("heading_truncated")
		text = string(r[:docxMaxHeadingRunes])
	}
	if err := p.account(text); err != nil {
		return err
	}
	p.flushSection()
	for len(p.headings) > 0 && p.headings[len(p.headings)-1].level >= level {
		p.headings = p.headings[:len(p.headings)-1]
	}
	p.headings = append(p.headings, docxHeading{level: level, text: text})
	p.headingTexts = append(p.headingTexts, text)
	return nil
}

func (p *docxParser) endParagraph(raw string, level int) error {
	if n := len(p.tables); n > 0 {
		cell := docxCleanText(raw, true)
		if cell == "" {
			return nil
		}
		t := p.tables[n-1]
		if t.cell.Len() > 0 {
			t.cell.WriteByte(' ')
		}
		t.cell.WriteString(cell)
		return nil
	}
	if level >= 0 {
		if h := docxCleanText(raw, true); h != "" {
			return p.pushHeading(level, h)
		}
		return nil
	}
	if text := docxCleanText(raw, false); text != "" {
		return p.addLine(text)
	}
	return nil
}

func (p *docxParser) endRow() {
	t := p.tables[len(p.tables)-1]
	cells := t.cells
	t.cells = nil
	for _, c := range cells {
		if c != "" {
			t.rows = append(t.rows, strings.Join(cells, " | "))
			return
		}
	}
}

func (p *docxParser) endTable() error {
	n := len(p.tables)
	t := p.tables[n-1]
	p.tables = p.tables[:n-1]
	if n > 1 {
		if len(t.rows) > 0 {
			parent := p.tables[n-2]
			if parent.cell.Len() > 0 {
				parent.cell.WriteByte(' ')
			}
			parent.cell.WriteString(strings.Join(t.rows, "; "))
		}
		return nil
	}
	for _, row := range t.rows {
		if err := p.addLine(row); err != nil {
			return err
		}
	}
	return nil
}

func docxParseDocument(ctx context.Context, r io.Reader, styles map[string]int) ([]Segment, error) {
	return docxParseDocumentWithMetadata(ctx, r, styles, nil)
}

func docxParseDocumentWithMetadata(ctx context.Context, r io.Reader, styles map[string]int, meta *ExtractionMetadata) ([]Segment, error) {
	dec := docxNewDecoder(r)
	ps := &docxParser{meta: meta}

	var (
		depth, skip, n int
		inP            bool
		inT            bool
		inRun          int
		pprChange      int
		level          = -1
		para           strings.Builder
	)
	for {
		if n++; n%docxCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, errDocxDirected
		case xml.StartElement:
			if depth++; depth > docxMaxDepth {
				return nil, errDocxDepth
			}
			if t.Name.Local == "drawing" || t.Name.Local == "pict" || t.Name.Local == "object" ||
				(docxIsW(t.Name.Space) && t.Name.Local == "altChunk") {
				meta.warn("unsupported_content")
			}
			if skip > 0 {
				skip++
				continue
			}
			// Text boxes would nest paragraphs inside paragraphs, and
			// AlternateContent repeats its Choice inside a Fallback. Both are
			// skipped for the first iteration.
			if t.Name.Local == "Fallback" || t.Name.Local == "txbxContent" {
				skip = 1
				continue
			}
			if !docxIsW(t.Name.Space) {
				continue
			}
			switch t.Name.Local {
			case "p":
				inP, inRun, inT, pprChange, level = true, 0, false, 0, -1
				para.Reset()
			case "pPrChange":
				pprChange++
			case "pStyle":
				if inP && pprChange == 0 && inRun == 0 {
					id := docxAttr(t, "val")
					if lv, ok := styles[id]; ok {
						level = lv
					} else {
						level = docxLevelFromName(id)
					}
				}
			case "r":
				inRun++
			case "t":
				inT = inRun > 0 && inP
			case "tab":
				if inRun > 0 && inP {
					para.WriteByte('\t')
				}
			case "br", "cr":
				if inRun > 0 && inP {
					para.WriteByte('\n')
				}
			case "tbl":
				ps.tables = append(ps.tables, &docxTable{})
			case "tr":
				if len(ps.tables) > 0 {
					ps.tables[len(ps.tables)-1].cells = nil
				}
			case "tc":
				if len(ps.tables) > 0 {
					ps.tables[len(ps.tables)-1].cell.Reset()
				}
			}
		case xml.CharData:
			if skip > 0 && len(bytes.TrimSpace(t)) > 0 {
				meta.warn("unsupported_content")
			}
			if inT && skip == 0 {
				para.Write(t)
				if para.Len() > 4*MaxExtractedChars {
					return nil, ErrTooLarge
				}
			}
		case xml.EndElement:
			depth--
			if skip > 0 {
				skip--
				continue
			}
			if !docxIsW(t.Name.Space) {
				continue
			}
			switch t.Name.Local {
			case "p":
				if inP {
					inP, inT = false, false
					if err := ps.endParagraph(para.String(), level); err != nil {
						return nil, err
					}
					para.Reset()
				}
			case "pPrChange":
				if pprChange > 0 {
					pprChange--
				}
			case "r":
				if inRun > 0 {
					inRun--
				}
			case "t":
				inT = false
			case "tc":
				if k := len(ps.tables); k > 0 {
					tb := ps.tables[k-1]
					tb.cells = append(tb.cells, strings.TrimSpace(tb.cell.String()))
					tb.cell.Reset()
				}
			case "tr":
				if len(ps.tables) > 0 {
					ps.endRow()
				}
			case "tbl":
				if len(ps.tables) > 0 {
					if err := ps.endTable(); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for len(ps.tables) > 0 { // truncated table: keep what was read
		if err := ps.endTable(); err != nil {
			return nil, err
		}
	}
	ps.flushSection()
	if len(ps.segs) == 0 && len(ps.headingTexts) > 0 {
		ps.segs = append(ps.segs, Segment{Text: strings.Join(ps.headingTexts, "\n")})
	}
	return ps.segs, nil
}

func docxMapErr(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return ctx.Err()
	case errors.Is(err, ErrTooLarge), errors.Is(err, errDocxTooLarge):
		return ErrTooLarge
	case errors.Is(err, ErrCorrupt):
		return err
	}
	return fmt.Errorf("%w: %v", ErrCorrupt, err)
}

func (e docxExtractor) Extract(ctx context.Context, data []byte) ([]Segment, error) {
	return e.extract(ctx, data, nil)
}

func (e docxExtractor) ExtractWithMetadata(ctx context.Context, data []byte) ([]Segment, ExtractionMetadata, error) {
	meta := newExtractionMetadata()
	segs, err := e.extract(ctx, data, &meta)
	return extractionResult(segs, meta, err)
}

func (docxExtractor) extract(ctx context.Context, data []byte, meta *ExtractionMetadata) (segs []Segment, err error) {
	defer func() {
		if r := recover(); r != nil {
			segs, err = nil, ErrCorrupt
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	files := zr.File
	if len(files) > docxMaxEntries {
		files = files[:docxMaxEntries]
	}
	var doc, stylesFile *zip.File
	for _, f := range files {
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer") ||
			strings.HasPrefix(name, "word/footnotes") || strings.HasPrefix(name, "word/endnotes") ||
			strings.HasPrefix(name, "word/comments") || strings.HasPrefix(name, "word/media/") ||
			strings.HasPrefix(name, "word/embeddings/") {
			meta.warn("unsupported_parts")
		}
		if docxUnsafeName(f.Name) {
			return nil, fmt.Errorf("%w: unsafe entry path", ErrCorrupt)
		}
		switch f.Name {
		case "word/document.xml":
			if doc == nil {
				doc = f
			}
		case "word/styles.xml":
			if stylesFile == nil {
				stylesFile = f
			}
		}
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: word/document.xml missing", ErrCorrupt)
	}
	// A declared size is only a cheap early exit; the real bound is the reader.
	if doc.UncompressedSize64 > MaxDecompressedBytes {
		return nil, ErrTooLarge
	}
	budget := int64(MaxDecompressedBytes)

	var styles map[string]int
	if stylesFile != nil && stylesFile.UncompressedSize64 <= MaxDecompressedBytes {
		styles, err = docxReadStyles(ctx, stylesFile, &budget)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, errDocxTooLarge) {
				return nil, docxMapErr(ctx, err)
			}
			styles = nil // fall back to matching HeadingN by style id
		}
	}

	rc, err := doc.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer rc.Close()
	segs, err = docxParseDocumentWithMetadata(ctx, &docxLimitedReader{r: rc, budget: &budget}, styles, meta)
	if err != nil {
		return nil, docxMapErr(ctx, err)
	}
	if len(segs) == 0 {
		return nil, ErrEmpty
	}
	return segs, nil
}
