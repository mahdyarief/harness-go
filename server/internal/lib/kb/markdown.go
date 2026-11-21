package kb

import (
	"bytes"
	"context"
	"strings"
	"unicode/utf8"
)

// mdCtxCheckEvery is how many lines are processed between context checks.
const mdCtxCheckEvery = 512

type markdownExtractor struct{}

// NewMarkdownExtractor splits Markdown into one Segment per ATX heading
// section, with the heading path outermost first.
func NewMarkdownExtractor() Extractor { return markdownExtractor{} }

func (markdownExtractor) Format() Format { return FormatMarkdown }

func (e markdownExtractor) Extract(ctx context.Context, data []byte) ([]Segment, error) {
	return e.extract(ctx, data, nil)
}

func (markdownExtractor) extract(ctx context.Context, data []byte, meta *ExtractionMetadata) (segs []Segment, err error) {
	defer func() {
		if r := recover(); r != nil {
			segs, err = nil, ErrCorrupt
		}
	}()
	text, err := mdPrepareWithMetadata(ctx, data, meta)
	if err != nil {
		return nil, err
	}
	stripped := mdStripFrontMatter(text)
	if len(stripped) != len(text) {
		meta.warn("front_matter_omitted")
	}
	text = stripped

	type heading struct {
		level int
		text  string
	}
	var (
		stack   []heading
		body    []string
		cur     []string // heading path of the section being collected
		fence   string   // opening fence marker while inside a fenced block
		lineCnt int
	)
	flush := func() error {
		joined := strings.Trim(strings.Join(body, "\n"), "\r\n")
		body = body[:0]
		if strings.TrimSpace(joined) == "" {
			return nil
		}
		if len(segs) >= MaxSegments {
			return ErrTooLarge
		}
		segs = append(segs, Segment{Heading: append([]string(nil), cur...), Text: joined})
		return nil
	}

	for line := range strings.Lines(text) {
		line = strings.TrimSuffix(line, "\n")
		lineCnt++
		if lineCnt%mdCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if marker, ok := mdFenceMarker(line); ok {
			if fence == "" {
				fence = marker
			} else if marker[0] == fence[0] && len(marker) >= len(fence) && strings.TrimSpace(line[strings.Index(line, marker)+len(marker):]) == "" {
				fence = ""
			}
			body = append(body, line)
			continue
		}
		if fence == "" {
			if level, title, ok := mdParseHeading(line); ok {
				if err := flush(); err != nil {
					return nil, err
				}
				for len(stack) > 0 && stack[len(stack)-1].level >= level {
					stack = stack[:len(stack)-1]
				}
				if utf8.RuneCountInString(title) > 1000 {
					meta.warn("heading_truncated")
				}
				stack = append(stack, heading{level, chunkMetadata(title, 1000)})
				cur = cur[:0]
				for _, h := range stack {
					if h.text != "" {
						cur = append(cur, h.text)
					}
				}
				continue
			}
		}
		body = append(body, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}

	if len(segs) == 0 {
		return nil, ErrEmpty
	}
	return segs, nil
}

type textExtractor struct{}

// NewTextExtractor returns one Segment for the whole file; the chunker splits
// it later.
func NewTextExtractor() Extractor { return textExtractor{} }

func (textExtractor) Format() Format { return FormatText }

func (e textExtractor) Extract(ctx context.Context, data []byte) ([]Segment, error) {
	return e.extract(ctx, data, nil)
}

func (textExtractor) extract(ctx context.Context, data []byte, meta *ExtractionMetadata) (segs []Segment, err error) {
	defer func() {
		if r := recover(); r != nil {
			segs, err = nil, ErrCorrupt
		}
	}()
	text, err := mdPrepareWithMetadata(ctx, data, meta)
	if err != nil {
		return nil, err
	}
	text = strings.Trim(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmpty
	}
	return []Segment{{Text: text}}, nil
}

// mdPrepare checks ctx and size, drops the BOM, repairs invalid UTF-8 and NUL
// bytes, and normalises line endings to "\n".
func mdPrepare(ctx context.Context, data []byte) (string, error) {
	return mdPrepareWithMetadata(ctx, data, nil)
}

func mdPrepareWithMetadata(ctx context.Context, data []byte, meta *ExtractionMetadata) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	// Cheap upper bound before any copying: a rune is at most 4 bytes.
	if len(data) > MaxExtractedChars*4 {
		return "", ErrTooLarge
	}
	if meta != nil && (!utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0) {
		meta.warn("text_normalized")
	}
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if utf8.RuneCountInString(text) > MaxExtractedChars {
		return "", ErrTooLarge
	}
	return text, nil
}

// mdStripFrontMatter removes a leading YAML block delimited by "---" lines. An
// unterminated block is left alone.
func mdStripFrontMatter(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	rest := text[4:]
	offset := 0
	for offset <= len(rest) {
		end := strings.IndexByte(rest[offset:], '\n')
		var line string
		if end < 0 {
			line = rest[offset:]
		} else {
			line = rest[offset : offset+end]
		}
		if t := strings.TrimRight(line, " \t"); t == "---" || t == "..." {
			if end < 0 {
				return ""
			}
			return rest[offset+end+1:]
		}
		if end < 0 {
			break
		}
		offset += end + 1
	}
	return text
}

// mdFenceMarker reports a code fence opener/closer (``` or ~~~, length >= 3,
// indented up to 3 spaces) and returns the fence run.
func mdFenceMarker(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return "", false
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return "", false
	}
	// A backtick fence's info string may not contain backticks.
	if c == '`' && strings.Contains(trimmed[n:], "`") {
		return "", false
	}
	return trimmed[:n], true
}

// mdParseHeading parses an ATX heading line.
func mdParseHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest := trimmed[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	rest = strings.TrimSpace(rest)
	// Drop an optional closing run of '#', which must follow a space.
	if stripped := strings.TrimRight(rest, "#"); stripped != rest {
		if stripped == "" {
			rest = ""
		} else if last := stripped[len(stripped)-1]; last == ' ' || last == '\t' {
			rest = strings.TrimSpace(stripped)
		}
	}
	return level, rest, true
}
