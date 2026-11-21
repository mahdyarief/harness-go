package kb

import (
	"iter"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ChunkOptions sizes are in characters (runes), a rough stand-in for tokens
// since no tokenizer is available: the defaults are about 300-800 tokens.
type ChunkOptions struct {
	// TargetChars is the size at which a chunk is closed and a new one begins.
	TargetChars int
	// MaxChars is the hard ceiling for a chunk, overlap included.
	MaxChars int
	// OverlapChars is how much of the previous chunk is repeated at the start
	// of the next one within the same segment.
	OverlapChars int
}

func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{TargetChars: 1800, MaxChars: 3200, OverlapChars: 200}
}

const (
	chunkParaSep = "\n\n"
	chunkContSep = " "
)

// chunkUnit is a piece of text that is never split further when assembling
// chunks: a whole paragraph, or one piece of an oversized paragraph.
type chunkUnit struct {
	text  string
	n     int    // rune count of text
	sep   string // separator placed before this unit when it follows another
	split bool   // came from splitting an oversized paragraph
}

// ChunkSegments turns segments into indexable chunks. It is pure and
// deterministic. Limits are per document; failures never return partial chunks.
func ChunkSegments(segments []Segment, opts ChunkOptions) ([]Chunk, error) {
	if len(segments) > MaxSegments {
		return nil, ErrTooLarge
	}
	textLeft, metadataLeft := MaxExtractedChars, MaxIndexedBytes
	for _, seg := range segments {
		if len(seg.Text) > textLeft*utf8.UTFMax {
			return nil, ErrTooLarge
		}
		textLeft -= utf8.RuneCountInString(seg.Text)
		if textLeft < 0 {
			return nil, ErrTooLarge
		}
		if len(seg.Locator) > metadataLeft {
			return nil, ErrTooLarge
		}
		metadataLeft -= len(seg.Locator)
		for i, heading := range seg.Heading {
			if i > 0 {
				metadataLeft -= len(" > ")
			}
			if len(heading) > metadataLeft {
				return nil, ErrTooLarge
			}
			metadataLeft -= len(heading)
		}
	}
	opts = chunkNormalize(opts)
	var out []Chunk
	bytesLeft := MaxIndexedBytes
	for _, seg := range segments {
		path := chunkMetadataParts(seg.Heading, " > ", 1000)
		locator := chunkMetadata(seg.Locator, 200)
		err := chunkSegment(seg.Text, opts, func(content string) error {
			cost := len(path) + len(locator) + len(content)
			if len(out) >= MaxChunks || cost > bytesLeft {
				return ErrTooLarge
			}
			bytesLeft -= cost
			out = append(out, Chunk{
				Position: len(out), HeadingPath: path, Locator: locator, Content: content,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func chunkMetadata(text string, limit int) string {
	return chunkMetadataParts([]string{text}, "", limit)
}

// Stream only the bounded prefix, repairing UTF-8 and dropping NUL without
// allocating a joined path or a rune slice proportional to hostile metadata.
func chunkMetadataParts(parts []string, sep string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := make([]rune, 0, limit)
	write := func(text string) bool {
		for _, r := range text {
			if r == 0 {
				continue
			}
			if len(runes) == limit {
				runes[limit-1] = '…'
				return false
			}
			runes = append(runes, r)
		}
		return true
	}
	for i, part := range parts {
		if i > 0 && !write(sep) {
			break
		}
		if !write(part) {
			break
		}
	}
	return string(runes)
}

func chunkNormalize(o ChunkOptions) ChunkOptions {
	d := DefaultChunkOptions()
	if o.TargetChars <= 0 {
		o.TargetChars = d.TargetChars
	}
	if o.MaxChars <= 0 {
		o.MaxChars = d.MaxChars
	}
	if o.MaxChars < o.TargetChars {
		o.MaxChars = o.TargetChars
	}
	if o.OverlapChars < 0 {
		o.OverlapChars = 0
	}
	if o.OverlapChars > o.TargetChars/2 {
		o.OverlapChars = o.TargetChars / 2
	}
	return o
}

func chunkSegment(text string, o ChunkOptions, consume func(string) error) error {
	units := chunkUnits(text, o)
	var (
		cur   []chunkUnit
		curN  int
		fresh int // units in cur that are new rather than overlap
	)
	render := func() string {
		var b strings.Builder
		for i, u := range cur {
			if i > 0 {
				b.WriteString(u.sep)
			}
			b.WriteString(u.text)
		}
		return strings.TrimSpace(b.String())
	}
	emit := func() error {
		if fresh == 0 {
			return nil
		}
		if s := render(); s != "" {
			return consume(s)
		}
		return nil
	}
	for u := range units {
		add := u.n
		if len(cur) > 0 {
			add += utf8.RuneCountInString(u.sep)
		}
		if fresh > 0 && curN+add > o.TargetChars {
			if err := emit(); err != nil {
				return err
			}
			cur = chunkOverlap(cur, o.OverlapChars)
			curN = chunkLen(cur)
			fresh = 0
			add = u.n
			if len(cur) > 0 {
				add += utf8.RuneCountInString(u.sep)
			}
		}
		if curN+add > o.MaxChars {
			// Only overlap is present and it does not fit beside u.
			cur, curN = nil, 0
			add = u.n
		}
		cur = append(cur, u)
		curN += add
		fresh++
	}
	return emit()
}

func chunkLen(us []chunkUnit) int {
	n := 0
	for i, u := range us {
		n += u.n
		if i > 0 {
			n += utf8.RuneCountInString(u.sep)
		}
	}
	return n
}

// chunkOverlap picks the trailing units of a closed chunk to repeat: whole
// units that fit in limit, or, when the last unit is a piece of a split
// paragraph that is too big, its tail cut at a word boundary.
func chunkOverlap(cur []chunkUnit, limit int) []chunkUnit {
	if limit <= 0 || len(cur) == 0 {
		return nil
	}
	total := 0
	start := len(cur)
	for i := len(cur) - 1; i >= 0; i-- {
		cost := cur[i].n
		if i < len(cur)-1 {
			cost += utf8.RuneCountInString(cur[i+1].sep)
		}
		if total+cost > limit {
			break
		}
		total += cost
		start = i
	}
	if start < len(cur) {
		return append([]chunkUnit(nil), cur[start:]...)
	}
	last := cur[len(cur)-1]
	if !last.split {
		return nil
	}
	r := []rune(last.text)
	if len(r) <= limit {
		return nil
	}
	tail := r[len(r)-limit:]
	for i, c := range tail {
		if unicode.IsSpace(c) {
			t := strings.TrimSpace(string(tail[i:]))
			if t == "" {
				return nil
			}
			return []chunkUnit{{text: t, n: utf8.RuneCountInString(t), sep: chunkContSep, split: true}}
		}
	}
	return nil
}

// chunkUnits splits text into paragraphs and breaks any paragraph longer than
// MaxChars into pieces no longer than TargetChars.
func chunkUnits(text string, o ChunkOptions) iter.Seq[chunkUnit] {
	return func(yield func(chunkUnit) bool) {
		text = strings.ToValidUTF8(text, "\uFFFD")
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		addPara := func(lines []string) bool {
			p := strings.TrimRight(strings.Join(lines, "\n"), " \t\n")
			if strings.TrimSpace(p) == "" {
				return true
			}
			n := utf8.RuneCountInString(p)
			if n <= o.MaxChars {
				return yield(chunkUnit{text: p, n: n, sep: chunkParaSep})
			}
			first := true
			for piece := range chunkSplitLong([]rune(p), o.TargetChars) {
				sep := chunkContSep
				if first {
					sep, first = chunkParaSep, false
				}
				if !yield(chunkUnit{text: piece, n: utf8.RuneCountInString(piece), sep: sep, split: true}) {
					return false
				}
			}
			return true
		}
		var lines []string
		for line := range strings.Lines(text) {
			line = strings.TrimSuffix(line, "\n")
			if strings.TrimSpace(line) == "" {
				if !addPara(lines) {
					return
				}
				lines = lines[:0]
				continue
			}
			lines = append(lines, line)
		}
		addPara(lines)
	}
}

// chunkSplitLong cuts r into pieces of at most limit runes, preferring
// sentence boundaries, then word boundaries, and only then raw rune cuts.
func chunkSplitLong(r []rune, limit int) iter.Seq[string] {
	return func(yield func(string) bool) {
		var cur []rune
		flush := func() bool {
			s := strings.TrimSpace(string(cur))
			cur = cur[:0]
			return s == "" || yield(s)
		}
		var add func([]rune, int) bool
		add = func(part []rune, level int) bool {
			if len(part) > limit {
				if !flush() {
					return false
				}
				if level == 0 {
					for w := range chunkWords(part) {
						if !add(w, 1) {
							return false
						}
					}
					return true
				}
				for len(part) > 0 {
					k := min(limit, len(part))
					cur = append(cur, part[:k]...)
					if !flush() {
						return false
					}
					part = part[k:]
				}
				return true
			}
			if len(cur)+len(part) > limit && !flush() {
				return false
			}
			cur = append(cur, part...)
			return true
		}
		for s := range chunkSentences(r) {
			if !add(s, 0) {
				return
			}
		}
		flush()
	}
}

// chunkSentences splits after sentence-ending punctuation followed by
// whitespace, and after every newline. Trailing whitespace stays attached.
func chunkSentences(r []rune) iter.Seq[[]rune] {
	return func(yield func([]rune) bool) {
		start := 0
		for i := 0; i < len(r); i++ {
			c := r[i]
			boundary := c == '\n' || ((c == '.' || c == '!' || c == '?' || c == '…') && (i+1 == len(r) || unicode.IsSpace(r[i+1])))
			if !boundary {
				continue
			}
			j := i + 1
			for j < len(r) && unicode.IsSpace(r[j]) && r[j-1] != '\n' {
				j++
			}
			if !yield(r[start:j]) {
				return
			}
			start, i = j, j-1
		}
		if start < len(r) {
			yield(r[start:])
		}
	}
}

// chunkWords splits into runs of non-space followed by their trailing space.
func chunkWords(r []rune) iter.Seq[[]rune] {
	return func(yield func([]rune) bool) {
		start := 0
		for i := 0; i < len(r); i++ {
			if unicode.IsSpace(r[i]) && (i+1 == len(r) || !unicode.IsSpace(r[i+1])) {
				if !yield(r[start : i+1]) {
					return
				}
				start = i + 1
			}
		}
		if start < len(r) {
			yield(r[start:])
		}
	}
}
