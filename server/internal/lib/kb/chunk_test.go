package kb

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustChunkSegments(t testing.TB, segments []Segment, opts ChunkOptions) []Chunk {
	t.Helper()
	chunks, err := ChunkSegments(segments, opts)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

func chunkCheckInvariants(t testing.TB, chunks []Chunk, o ChunkOptions) {
	t.Helper()
	o = chunkNormalize(o)
	for i, c := range chunks {
		if c.Position != i {
			t.Fatalf("chunk %d has position %d", i, c.Position)
		}
		if strings.TrimSpace(c.Content) == "" {
			t.Fatalf("chunk %d is blank", i)
		}
		if !utf8.ValidString(c.Content) {
			t.Fatalf("chunk %d is not valid UTF-8", i)
		}
		if n := utf8.RuneCountInString(c.Content); n > o.MaxChars {
			t.Fatalf("chunk %d has %d runes, max %d", i, n, o.MaxChars)
		}
	}
}

func TestChunkSegmentsBasics(t *testing.T) {
	segs := []Segment{
		{Heading: []string{"Bab 1", "Pengantar"}, Locator: "halaman 2", Text: "Paragraf satu.\n\nParagraf dua."},
		{Text: "Tanpa heading."},
	}
	got := mustChunkSegments(t, segs, DefaultChunkOptions())
	if len(got) != 2 {
		t.Fatalf("chunks = %d, want 2: %#v", len(got), got)
	}
	want0 := Chunk{Position: 0, HeadingPath: "Bab 1 > Pengantar", Locator: "halaman 2", Content: "Paragraf satu.\n\nParagraf dua."}
	want1 := Chunk{Position: 1, Content: "Tanpa heading."}
	if got[0] != want0 || got[1] != want1 {
		t.Fatalf("got %#v", got)
	}
}

func TestChunkSegmentsMetadataLimits(t *testing.T) {
	for _, heading := range [][]string{
		{strings.Repeat("a", 1001)},
		{strings.Repeat("界", 600), strings.Repeat("文", 600)},
	} {
		got := mustChunkSegments(t, []Segment{{Heading: heading, Locator: strings.Repeat("頁", 201), Text: "Isi tetap utuh."}}, DefaultChunkOptions())
		if len(got) != 1 || got[0].Content != "Isi tetap utuh." {
			t.Fatalf("konten berubah: %#v", got)
		}
		if utf8.RuneCountInString(got[0].HeadingPath) != 1000 || utf8.RuneCountInString(got[0].Locator) != 200 {
			t.Fatalf("metadata melampaui batas skema: %#v", got[0])
		}
		if !utf8.ValidString(got[0].HeadingPath) || !utf8.ValidString(got[0].Locator) {
			t.Fatal("metadata bukan UTF-8 valid")
		}
	}
	got := mustChunkSegments(t, []Segment{{Heading: []string{"Judul\x00"}, Locator: "baris\x00 1", Text: "Isi"}}, DefaultChunkOptions())
	if got[0].HeadingPath != "Judul" || got[0].Locator != "baris 1" {
		t.Fatalf("NUL metadata tidak dibersihkan: %#v", got[0])
	}
	got = mustChunkSegments(t, []Segment{{Heading: []string{"A\x00\xff", "B"}, Locator: "\xfe\x00", Text: "Isi"}}, DefaultChunkOptions())
	if got[0].HeadingPath != "A� > B" || got[0].Locator != "�" {
		t.Fatalf("metadata UTF-8 repair failed: %#v", got[0])
	}
}

func TestChunkSegmentsEmptyInputs(t *testing.T) {
	if got := mustChunkSegments(t, nil, DefaultChunkOptions()); len(got) != 0 {
		t.Fatalf("nil: %#v", got)
	}
	segs := []Segment{{Text: ""}, {Text: " \n\n\t "}, {Text: "\r\n\r\n"}}
	if got := mustChunkSegments(t, segs, DefaultChunkOptions()); len(got) != 0 {
		t.Fatalf("blank: %#v", got)
	}
}

func TestChunkSegmentsKeepsParagraphsWhole(t *testing.T) {
	o := ChunkOptions{TargetChars: 100, MaxChars: 200, OverlapChars: 0}
	var paras []string
	for i := 0; i < 20; i++ {
		paras = append(paras, strings.Repeat(string(rune('a'+i)), 40))
	}
	got := mustChunkSegments(t, []Segment{{Text: strings.Join(paras, "\n\n")}}, o)
	chunkCheckInvariants(t, got, o)
	if len(got) < 5 {
		t.Fatalf("expected several chunks, got %d", len(got))
	}
	// every paragraph appears intact in exactly one chunk
	for _, p := range paras {
		n := 0
		for _, c := range got {
			if strings.Contains(c.Content, p) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("paragraph %q found in %d chunks", p[:3], n)
		}
	}
}

func TestChunkSegmentsDeterministic(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("Kalimat nomor ini cukup panjang untuk uji. ")
		if i%7 == 0 {
			b.WriteString("\n\n")
		}
	}
	segs := []Segment{{Heading: []string{"A"}, Text: b.String()}}
	o := ChunkOptions{TargetChars: 500, MaxChars: 900, OverlapChars: 80}
	first := mustChunkSegments(t, segs, o)
	for i := 0; i < 3; i++ {
		if !reflect.DeepEqual(first, mustChunkSegments(t, segs, o)) {
			t.Fatal("output differs between runs")
		}
	}
	chunkCheckInvariants(t, first, o)
}

func TestChunkSegmentsLongSingleParagraph(t *testing.T) {
	o := DefaultChunkOptions()
	text := strings.Repeat("Ini kalimat Indonesia yang agak panjang, 😀 dengan emoji. ", 3000)
	got := mustChunkSegments(t, []Segment{{Text: text}}, o)
	chunkCheckInvariants(t, got, o)
	if len(got) < 10 {
		t.Fatalf("chunks = %d", len(got))
	}
	// pieces split at sentence boundaries: each non-final chunk ends with '.'
	for i, c := range got[:len(got)-1] {
		if !strings.HasSuffix(c.Content, ".") {
			t.Fatalf("chunk %d does not end at a sentence: %q", i, c.Content[len(c.Content)-20:])
		}
	}
}

func TestChunkSegmentsNoSpacesSplitsOnRunes(t *testing.T) {
	o := ChunkOptions{TargetChars: 100, MaxChars: 150, OverlapChars: 10}
	text := strings.Repeat("日本語😀é", 2000)
	got := mustChunkSegments(t, []Segment{{Text: text}}, o)
	chunkCheckInvariants(t, got, o)
	if len(got) < 10 {
		t.Fatalf("chunks = %d", len(got))
	}
	// no content lost, no rune corrupted
	var joined strings.Builder
	for _, c := range got {
		joined.WriteString(c.Content)
	}
	if !strings.Contains(joined.String(), "日本語😀é") {
		t.Fatal("content corrupted")
	}
}

func TestChunkSegmentsWordBoundary(t *testing.T) {
	o := ChunkOptions{TargetChars: 50, MaxChars: 60, OverlapChars: 0}
	text := strings.Repeat("kata ", 200) // one long paragraph, no sentence ends
	got := mustChunkSegments(t, []Segment{{Text: text}}, o)
	chunkCheckInvariants(t, got, o)
	for i, c := range got {
		for _, w := range strings.Fields(c.Content) {
			if w != "kata" {
				t.Fatalf("chunk %d has split word %q", i, w)
			}
		}
	}
}

func TestChunkSegmentsOverlapStaysInSegment(t *testing.T) {
	o := ChunkOptions{TargetChars: 120, MaxChars: 300, OverlapChars: 60}
	mk := func(tag string) string {
		var ps []string
		for i := 0; i < 12; i++ {
			ps = append(ps, strings.Repeat(tag, 30))
		}
		return strings.Join(ps, "\n\n")
	}
	got := mustChunkSegments(t, []Segment{{Heading: []string{"A"}, Text: mk("x")}, {Heading: []string{"B"}, Text: mk("y")}}, o)
	chunkCheckInvariants(t, got, o)
	overlapped := false
	for i, c := range got {
		if c.HeadingPath == "A" && strings.Contains(c.Content, "y") || c.HeadingPath == "B" && strings.Contains(c.Content, "x") {
			t.Fatalf("chunk %d crosses segment boundary: %q", i, c.HeadingPath)
		}
		if i > 0 && got[i-1].HeadingPath == c.HeadingPath {
			prevParas := strings.Split(got[i-1].Content, "\n\n")
			if strings.HasPrefix(c.Content, prevParas[len(prevParas)-1]) {
				overlapped = true
			}
		}
	}
	if !overlapped {
		t.Fatal("expected overlap between consecutive chunks of one segment")
	}
}

func TestChunkSegmentsNormalizesOptions(t *testing.T) {
	got := mustChunkSegments(t, []Segment{{Text: "halo dunia"}}, ChunkOptions{})
	if len(got) != 1 || got[0].Content != "halo dunia" {
		t.Fatalf("got %#v", got)
	}
	weird := ChunkOptions{TargetChars: 10, MaxChars: 5, OverlapChars: 999}
	chunkCheckInvariants(t, mustChunkSegments(t, []Segment{{Text: strings.Repeat("abc def. ", 100)}}, weird), weird)
}

func TestChunkSegmentsResourceLimits(t *testing.T) {
	tiny := ChunkOptions{TargetChars: 1, MaxChars: 1}
	for _, n := range []int{MaxChunks, MaxChunks + 1} {
		chunks, err := ChunkSegments([]Segment{{Text: strings.Repeat("x", n)}}, tiny)
		if n == MaxChunks {
			if err != nil || len(chunks) != MaxChunks {
				t.Fatalf("at chunk limit: count=%d err=%v", len(chunks), err)
			}
		} else if !errors.Is(err, ErrTooLarge) || chunks != nil {
			t.Fatalf("chunk overflow returned partial output: %d, %v", len(chunks), err)
		}
	}
	for _, n := range []int{MaxExtractedChars, MaxExtractedChars + 1} {
		chunks, err := ChunkSegments([]Segment{{Text: strings.Repeat("界", n/2)}, {Text: strings.Repeat("界", n-n/2)}}, ChunkOptions{TargetChars: MaxExtractedChars, MaxChars: MaxExtractedChars})
		if n == MaxExtractedChars {
			if err != nil || len(chunks) != 2 {
				t.Fatalf("at text limit: count=%d err=%v", len(chunks), err)
			}
		} else if !errors.Is(err, ErrTooLarge) || chunks != nil {
			t.Fatalf("text overflow returned partial output: %d, %v", len(chunks), err)
		}
	}
	segments := make([]Segment, MaxSegments)
	for i := range segments {
		segments[i].Text = "x"
	}
	if chunks, err := ChunkSegments(segments, DefaultChunkOptions()); err != nil || len(chunks) != MaxChunks {
		t.Fatalf("at segment limit: count=%d err=%v", len(chunks), err)
	}
	chunks, err := ChunkSegments(make([]Segment, MaxSegments+1), DefaultChunkOptions())
	if !errors.Is(err, ErrTooLarge) || chunks != nil {
		t.Fatalf("segment overflow: %#v, %v", chunks, err)
	}
}

func TestChunkSegmentsMetadataBudgets(t *testing.T) {
	// Raw inherited metadata has its own aggregate byte budget, before truncation.
	heading := strings.Repeat("😀", MaxIndexedBytes/8)
	segments := []Segment{{Heading: []string{heading}, Text: "a"}, {Heading: []string{heading}, Text: "b"}}
	if chunks, err := ChunkSegments(segments, DefaultChunkOptions()); err != nil || len(chunks) != 2 {
		t.Fatalf("at metadata limit: count=%d err=%v", len(chunks), err)
	}
	segments[1].Locator = "x"
	if chunks, err := ChunkSegments(segments, DefaultChunkOptions()); !errors.Is(err, ErrTooLarge) || chunks != nil {
		t.Fatalf("metadata byte overflow: count=%d err=%v", len(chunks), err)
	}
	// A heading path must charge join separators too.
	segments[1].Locator = ""
	segments[1].Heading = []string{heading, ""}
	if chunks, err := ChunkSegments(segments, DefaultChunkOptions()); !errors.Is(err, ErrTooLarge) || chunks != nil {
		t.Fatalf("metadata separator overflow: count=%d err=%v", len(chunks), err)
	}
	// Exactly 4096 bytes per chunk: four-byte heading runes + locator + content.
	segments = []Segment{{Heading: []string{strings.Repeat("😀", 1000)}, Locator: strings.Repeat("l", 95), Text: strings.Repeat("x", MaxChunks)}}
	tiny := ChunkOptions{TargetChars: 1, MaxChars: 1}
	chunks, err := ChunkSegments(segments, tiny)
	if err != nil || len(chunks) != MaxChunks {
		t.Fatalf("at indexed byte limit: count=%d err=%v", len(chunks), err)
	}
	bytes := 0
	for _, c := range chunks {
		bytes += len(c.HeadingPath) + len(c.Locator) + len(c.Content)
	}
	if bytes != MaxIndexedBytes {
		t.Fatalf("indexed bytes=%d want=%d", bytes, MaxIndexedBytes)
	}
	segments[len(segments)-1].Locator += "l"
	if chunks, err = ChunkSegments(segments, tiny); !errors.Is(err, ErrTooLarge) || chunks != nil {
		t.Fatalf("indexed overflow returned partial output: count=%d err=%v", len(chunks), err)
	}
}

func TestChunkSegmentStopsEmission(t *testing.T) {
	calls := 0
	err := chunkSegment(strings.Repeat("x", MaxExtractedChars), ChunkOptions{TargetChars: 1, MaxChars: 1}, func(string) error {
		calls++
		return ErrTooLarge
	})
	if !errors.Is(err, ErrTooLarge) || calls != 1 {
		t.Fatalf("emission did not stop: calls=%d err=%v", calls, err)
	}
}

func FuzzChunkSegments(f *testing.F) {
	f.Add("Halo dunia.\n\nIni paragraf kedua 😀.", 50, 80, 10)
	f.Add(strings.Repeat("x", 500), 20, 30, 5)
	f.Fuzz(func(t *testing.T, text string, target, max, overlap int) {
		if target > 5000 || max > 10000 || overlap > 5000 {
			t.Skip()
		}
		o := ChunkOptions{TargetChars: target, MaxChars: max, OverlapChars: overlap}
		chunks, err := ChunkSegments([]Segment{{Heading: []string{"h"}, Text: text}}, o)
		if err != nil {
			if !errors.Is(err, ErrTooLarge) || chunks != nil {
				t.Fatalf("unexpected failure: %v, %#v", err, chunks)
			}
			return
		}
		chunkCheckInvariants(t, chunks, o)
	})
}
