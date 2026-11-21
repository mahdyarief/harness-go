package kb

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// csvMaxRows and csvMaxCols bound how much a hostile CSV can make us walk.
	csvMaxRows = 50_000
	csvMaxCols = 200
	// csvRowsPerSegment is how many data rows share one Segment.
	csvRowsPerSegment = 40
)

type csvExtractor struct{}

// NewCSVExtractor treats the first record as the header and emits one Segment
// per group of rows. Each row is rendered "Kolom: nilai; Kolom: nilai" so a
// chunk stays meaningful without the header.
func NewCSVExtractor() Extractor { return csvExtractor{} }

func (csvExtractor) Format() Format { return FormatCSV }

func (e csvExtractor) Extract(ctx context.Context, data []byte) ([]Segment, error) {
	return e.extract(ctx, data, nil)
}

func (e csvExtractor) ExtractWithMetadata(ctx context.Context, data []byte) ([]Segment, ExtractionMetadata, error) {
	meta := newExtractionMetadata()
	segs, err := e.extract(ctx, data, &meta)
	return extractionResult(segs, meta, err)
}

func (csvExtractor) extract(ctx context.Context, data []byte, meta *ExtractionMetadata) (segs []Segment, err error) {
	defer func() {
		if r := recover(); r != nil {
			segs, err = nil, ErrCorrupt
		}
	}()
	text, err := mdPrepareWithMetadata(ctx, data, meta)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmpty
	}

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = csvDetectDelimiter(text)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.ReuseRecord = false

	header, err := r.Read()
	if err != nil {
		return nil, csvMapErr(err)
	}
	if len(header) > csvMaxCols {
		return nil, ErrTooLarge
	}
	names := make([]string, len(header))
	for i, h := range header {
		names[i] = strings.TrimSpace(h)
	}

	var (
		lines    []string
		firstRow int
		rowNo    = 1 // header is row 1
		chars    int
	)
	flush := func(lastRow int) {
		if len(lines) == 0 {
			return
		}
		segs = append(segs, Segment{
			Locator: fmt.Sprintf("baris %d-%d", firstRow, lastRow),
			Text:    strings.Join(lines, "\n"),
		})
		lines = nil
	}

	for {
		if rowNo%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvMapErr(err)
		}
		rowNo++
		if rowNo-1 > csvMaxRows {
			return nil, ErrTooLarge
		}
		if len(rec) > csvMaxCols {
			return nil, ErrTooLarge
		}
		if (rowNo-2)%csvRowsPerSegment == 0 {
			firstRow = rowNo
		}
		if line := csvRenderRow(names, rec); line != "" {
			chars += utf8.RuneCountInString(line) + 1
			if chars > MaxExtractedChars {
				return nil, ErrTooLarge
			}
			lines = append(lines, line)
		}
		if (rowNo-1)%csvRowsPerSegment == 0 {
			flush(rowNo)
		}
	}
	flush(rowNo)

	if len(segs) == 0 {
		return nil, ErrEmpty
	}
	total := rowNo - 1 // records, excluding the header; quoted newlines are not rows
	meta.cover("rows", "document", total, &total)
	return segs, nil
}

func csvMapErr(err error) error {
	if errors.Is(err, io.EOF) {
		return ErrEmpty
	}
	return ErrCorrupt
}

// csvRenderRow renders one record as "Kolom: nilai; Kolom: nilai", skipping
// empty cells. Cells beyond the header are named "Kolom N".
func csvRenderRow(names, rec []string) string {
	var b strings.Builder
	for i, v := range rec {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		if name == "" {
			name = "Kolom " + strconv.Itoa(i+1)
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

// csvDetectDelimiter picks the most frequent of , ; tab on the first line,
// ignoring quoted text. Ties and no candidates mean a comma.
func csvDetectDelimiter(text string) rune {
	counts := map[rune]int{}
	inQuote := false
	for _, c := range text {
		if c == '\n' && !inQuote {
			break
		}
		switch c {
		case '"':
			inQuote = !inQuote
		case ',', ';', '\t':
			if !inQuote {
				counts[c]++
			}
		}
	}
	best, bestN := ',', counts[',']
	for _, c := range []rune{';', '\t'} {
		if counts[c] > bestN {
			best, bestN = c, counts[c]
		}
	}
	return best
}
