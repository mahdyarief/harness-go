package kb

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// Limits specific to the XLSX extractor. Row, column and sheet output is
// truncated; decompression, XML complexity and extracted-text limits reject the
// entire workbook before any partial result can escape.
const (
	// xlsxMaxSheets caps visible sheets read from one workbook.
	xlsxMaxSheets = 50
	// xlsxMaxRowsPerSheet caps rows scanned per sheet, empty rows included, so
	// a sheet with a stray cell at row 1,000,000 cannot stall the parser.
	xlsxMaxRowsPerSheet = 20_000
	// xlsxMaxCols caps columns kept per row. A real table rarely passes 100.
	xlsxMaxCols = 200
	// xlsxRowsPerSegment is how many sheet rows go into one segment, small
	// enough that the chunker keeps a segment's rows together.
	xlsxRowsPerSegment = 40
	// xlsxMaxCellRunes truncates a pathological cell (a pasted document).
	xlsxMaxCellRunes = 2000
	// xlsxMaxHeaderRunes truncates a header label repeated on every row.
	xlsxMaxHeaderRunes = 100
	// xlsxMaxEntries bounds the zip entries scanned.
	xlsxMaxEntries = 5000
	// xlsxCtxEvery is how many rows pass between ctx checks.
	xlsxCtxEvery = 128

	// These are workbook-wide object budgets, not ZIP header counts. Even an
	// unused shared string, style or relationship is deserialized by excelize.
	xlsxMaxXMLDepth        = 64
	xlsxMaxXMLTokens       = 1_000_000
	xlsxMaxXMLElements     = 400_000
	xlsxMaxXMLAttributes   = 1_000_000
	xlsxMaxAttrsPerElement = 64
	xlsxMaxAttributeBytes  = 4096
	xlsxMaxSharedStrings   = 50_000
	xlsxMaxRichRuns        = 100_000
	xlsxMaxRunsPerString   = 256
	xlsxMaxStringBytes     = 128 << 10 // allows 32,767 four-byte Excel characters
	// Bound allocation *inside* xml.Decoder.Token too (attributes, comments,
	// names and character data), before we can inspect its returned token.
	xlsxMaxXMLTokenBytes = 256 << 10
)

type xlsxExtractor struct{}

// NewXLSXExtractor reads a workbook through excelize's streaming row iterator.
// The first row of each visible sheet is the header; every following row is
// rendered "Header: value; Header: value" so a chunk carries its own meaning.
func NewXLSXExtractor() Extractor { return xlsxExtractor{} }

func (xlsxExtractor) Format() Format { return FormatXLSX }

// xlsxPreflightReader counts actual decompressor output, including non-XML
// entries. The per-token budget prevents xml.Decoder from first allocating an
// unbounded start tag or text token before the structural checks run.
type xlsxPreflightReader struct {
	ctx        context.Context
	r          io.Reader
	remaining  *int64
	tokenBytes int
	xml        bool
}

func (r *xlsxPreflightReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.xml {
		if r.tokenBytes <= 0 {
			return 0, ErrTooLarge
		}
		if len(p) > r.tokenBytes {
			p = p[:r.tokenBytes]
		}
	}
	if int64(len(p)) > *r.remaining+1 {
		p = p[:int(*r.remaining+1)]
	}
	n, err := r.r.Read(p)
	*r.remaining -= int64(n)
	r.tokenBytes -= n
	if *r.remaining < 0 {
		return n, ErrTooLarge
	}
	return n, err
}

type xlsxXMLBudget struct {
	tokens, elements, attributes, sharedStrings, richRuns int
}

// xlsxScanXML never builds a DOM or trusts sst count/uniqueCount attributes.
// Local names are deliberately counted in every namespace and every part, so
// unused strings, inline strings and extension XML cannot evade the budgets.
func xlsxScanXML(r *xlsxPreflightReader, budget *xlsxXMLBudget) error {
	d := xml.NewDecoder(r)
	type frame struct {
		stringValue bool
		bytes, runs int
	}
	stack := make([]frame, 0, xlsxMaxXMLDepth)
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.tokenBytes = xlsxMaxXMLTokenBytes
		before := d.InputOffset()
		token, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		budget.tokens++
		if budget.tokens > xlsxMaxXMLTokens || d.InputOffset()-before > xlsxMaxXMLTokenBytes {
			return ErrTooLarge
		}
		switch t := token.(type) {
		case xml.StartElement:
			budget.elements++
			budget.attributes += len(t.Attr)
			if len(stack) >= xlsxMaxXMLDepth || budget.elements > xlsxMaxXMLElements ||
				budget.attributes > xlsxMaxXMLAttributes || len(t.Attr) > xlsxMaxAttrsPerElement {
				return ErrTooLarge
			}
			if len(t.Name.Local)+len(t.Name.Space) > xlsxMaxAttributeBytes {
				return ErrTooLarge
			}
			for _, attr := range t.Attr {
				if len(attr.Value) > xlsxMaxAttributeBytes ||
					len(attr.Name.Local)+len(attr.Name.Space) > xlsxMaxAttributeBytes {
					return ErrTooLarge
				}
			}
			if t.Name.Local == "si" {
				budget.sharedStrings++
				if budget.sharedStrings > xlsxMaxSharedStrings {
					return ErrTooLarge
				}
			}
			if t.Name.Local == "r" {
				budget.richRuns++
				if budget.richRuns > xlsxMaxRichRuns {
					return ErrTooLarge
				}
				for i := range stack {
					if stack[i].stringValue {
						stack[i].runs++
						if stack[i].runs > xlsxMaxRunsPerString {
							return ErrTooLarge
						}
					}
				}
			}
			stack = append(stack, frame{stringValue: t.Name.Local == "si" || t.Name.Local == "is" || t.Name.Local == "t"})
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(t) > xlsxMaxStringBytes {
				return ErrTooLarge
			}
			for i := range stack {
				// Count direct character data in every element across token
				// boundaries, plus all descendant text in a logical string.
				if i == len(stack)-1 || stack[i].stringValue {
					stack[i].bytes += len(t)
					if stack[i].bytes > xlsxMaxStringBytes {
						return ErrTooLarge
					}
				}
			}
		}
	}
}

// xlsxScanZip checks every XML/rels part before excelize deserializes objects,
// and counts real decompressed bytes for all entries, not ZIP header sizes.
func xlsxScanZip(ctx context.Context, data []byte) error {
	return xlsxScanZipWithMetadata(ctx, data, nil)
}

func xlsxScanZipWithMetadata(ctx context.Context, data []byte, meta *ExtractionMetadata) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(zr.File) > xlsxMaxEntries {
		return ErrTooLarge
	}
	budget := int64(MaxDecompressedBytes)
	var xmlBudget xlsxXMLBudget
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, part := range strings.Split(strings.ReplaceAll(f.Name, "\\", "/"), "/") {
			if part == ".." {
				return fmt.Errorf("%w: unsafe entry path", ErrCorrupt)
			}
		}
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(name, "xl/media/") || strings.HasPrefix(name, "xl/drawings/") ||
			strings.HasPrefix(name, "xl/embeddings/") || strings.HasPrefix(name, "xl/charts/") ||
			strings.HasPrefix(name, "xl/diagrams/") || strings.HasPrefix(name, "xl/ctrlprops/") ||
			strings.HasPrefix(name, "xl/activex/") {
			meta.warn("images_or_objects")
		}
		r := &xlsxPreflightReader{ctx: ctx, r: rc, remaining: &budget,
			xml: strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels")}
		if r.xml {
			err = xlsxScanXML(r, &xmlBudget)
		} else {
			_, err = io.Copy(io.Discard, r)
		}
		closeErr := rc.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, ErrTooLarge) {
			return ErrTooLarge
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if closeErr != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, closeErr)
		}
	}
	return nil
}

// xlsxCell flattens whitespace so a cell never breaks the one-line row format,
// and bounds its length.
func xlsxCell(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxRunes {
		s = string([]rune(s)[:maxRunes])
	}
	return s
}

// xlsxRenderRow renders the non-empty cells of a row as "Header: value". It
// returns "" for a row with no content.
func xlsxRenderRow(headers, cells []string) string {
	var b strings.Builder
	n := min(len(cells), xlsxMaxCols)
	for i := 0; i < n; i++ {
		v := xlsxCell(cells[i], xlsxMaxCellRunes)
		if v == "" {
			continue
		}
		name := ""
		if i < len(headers) {
			name = headers[i]
		}
		if name == "" {
			name, _ = excelize.ColumnNumberToName(i + 1)
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(v)
	}
	return b.String()
}

type xlsxBuilder struct {
	segs  []Segment
	chars int
	meta  *ExtractionMetadata
}

func (b *xlsxBuilder) add(sheet string, first, last int, lines []string) error {
	text := strings.Join(lines, "\n")
	b.chars += utf8.RuneCountInString(text) + 1
	if b.chars > MaxExtractedChars {
		return ErrTooLarge
	}
	loc := fmt.Sprintf("sheet %s, baris %d", sheet, first)
	if last > first {
		loc = fmt.Sprintf("sheet %s, baris %d-%d", sheet, first, last)
	}
	b.segs = append(b.segs, Segment{Heading: []string{sheet}, Locator: loc, Text: text})
	return nil
}

// xlsxReadSheet streams one sheet into segments of xlsxRowsPerSegment rows.
func xlsxReadSheet(ctx context.Context, f *excelize.File, sheet string, b *xlsxBuilder) error {
	rows, err := f.Rows(sheet)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer rows.Close()

	var (
		headers          []string
		haveHeader       bool
		headerRow        int
		lines            []string
		group            = -1
		firstRow, lastAt int
		rowNum           int
		capped           bool
	)
	flush := func() error {
		if len(lines) == 0 {
			return nil
		}
		err := b.add(sheet, firstRow, lastAt, lines)
		lines = lines[:0]
		return err
	}
	for rows.Next() {
		rowNum++
		if rowNum > xlsxMaxRowsPerSheet {
			rowNum-- // the lookahead row was not processed
			capped = true
			b.meta.warn("row_limit")
			break
		}
		if rowNum%xlsxCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		cells, err := rows.Columns()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if len(cells) > xlsxMaxCols {
			b.meta.warn("column_limit")
		}
		if !haveHeader {
			// The first non-empty row is the header.
			n := min(len(cells), xlsxMaxCols)
			hdr := make([]string, n)
			any := false
			for i := 0; i < n; i++ {
				if b.meta != nil && utf8.RuneCountInString(strings.Join(strings.Fields(cells[i]), " ")) > xlsxMaxHeaderRunes {
					b.meta.warn("header_truncated")
				}
				hdr[i] = xlsxCell(cells[i], xlsxMaxHeaderRunes)
				any = any || hdr[i] != ""
			}
			if any {
				headers, haveHeader, headerRow = hdr, true, rowNum
			}
			continue
		}
		if b.meta != nil {
			for _, cell := range cells[:min(len(cells), xlsxMaxCols)] {
				if utf8.RuneCountInString(strings.Join(strings.Fields(cell), " ")) > xlsxMaxCellRunes {
					b.meta.warn("cell_truncated")
				}
			}
		}
		line := xlsxRenderRow(headers, cells)
		if line == "" {
			continue
		}
		if g := (rowNum - headerRow - 1) / xlsxRowsPerSegment; g != group {
			if err := flush(); err != nil {
				return err
			}
			group, firstRow = g, rowNum
		}
		lines = append(lines, line)
		lastAt = rowNum
	}
	if err := rows.Error(); err != nil {
		if !errors.Is(err, excelize.ErrMaxRows) {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		capped = true
		b.meta.warn("row_limit")
	}
	processed := rowNum
	if haveHeader {
		processed-- // leading/trailing empty rows still consume the row budget
	}
	var total *int
	if !capped {
		n := processed
		total = &n
	}
	b.meta.cover("rows", sheet, processed, total)
	return flush()
}

func (e xlsxExtractor) Extract(ctx context.Context, data []byte) ([]Segment, error) {
	return e.extract(ctx, data, nil)
}

func (e xlsxExtractor) ExtractWithMetadata(ctx context.Context, data []byte) ([]Segment, ExtractionMetadata, error) {
	meta := newExtractionMetadata()
	segs, err := e.extract(ctx, data, &meta)
	return extractionResult(segs, meta, err)
}

func (xlsxExtractor) extract(ctx context.Context, data []byte, meta *ExtractionMetadata) (segs []Segment, err error) {
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
	if err := xlsxScanZipWithMetadata(ctx, data, meta); err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    MaxDecompressedBytes,
		UnzipXMLSizeLimit: MaxDecompressedBytes, // keep worksheets in memory, never spill to a temp file
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer f.Close()

	b := &xlsxBuilder{meta: meta}
	var visible []string
	for _, name := range f.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ok, err := f.GetSheetVisible(name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if !ok {
			meta.warn("hidden_sheets")
			continue
		}
		visible = append(visible, name)
	}
	total := len(visible)
	processed := min(total, xlsxMaxSheets)
	meta.cover("sheets", "visible", processed, &total)
	if total > xlsxMaxSheets {
		meta.warn("sheet_limit")
	}
	for _, name := range visible[:processed] {
		if err := xlsxReadSheet(ctx, f, name, b); err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return nil, ctx.Err()
			}
			return nil, err
		}
	}
	if len(b.segs) == 0 {
		return nil, ErrEmpty
	}
	return b.segs, nil
}
