package kb

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestMarkdownLongHeadingChunks(t *testing.T) {
	segments, err := NewMarkdownExtractor().Extract(context.Background(), []byte("# "+strings.Repeat("界", 1001)+"\nIsi dokumen."))
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := ChunkSegments(segments, DefaultChunkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || len([]rune(chunks[0].HeadingPath)) != 1000 || chunks[0].Content != "Isi dokumen." {
		t.Fatalf("heading panjang tidak menghasilkan chunk valid: %#v", chunks)
	}
}

func TestMarkdownSegmentResourceLimits(t *testing.T) {
	for _, n := range []int{MaxSegments, MaxSegments + 1} {
		segments, err := NewMarkdownExtractor().Extract(context.Background(), []byte(strings.Repeat("## Section\nBody\n", n)))
		if n == MaxSegments {
			if err != nil || len(segments) != MaxSegments {
				t.Fatalf("at segment limit: count=%d err=%v", len(segments), err)
			}
		} else if !errors.Is(err, ErrTooLarge) || segments != nil {
			t.Fatalf("segment overflow returned partial output: count=%d err=%v", len(segments), err)
		}
	}
}

func TestMarkdownInheritedHugeHeading(t *testing.T) {
	source := "# " + strings.Repeat("界", 500_000) + "\n" + strings.Repeat("## Child\nBody\n", 1000)
	segments, err := NewMarkdownExtractor().Extract(context.Background(), []byte(source))
	if err != nil || len(segments) != 1000 {
		t.Fatalf("extract: count=%d err=%v", len(segments), err)
	}
	for _, seg := range segments {
		if len([]rune(seg.Heading[0])) != 1000 || !strings.HasSuffix(seg.Heading[0], "…") {
			t.Fatal("inherited heading was not bounded during extraction")
		}
	}
	chunks, err := ChunkSegments(segments, DefaultChunkOptions())
	if err != nil || len(chunks) != 1000 {
		t.Fatalf("chunk inherited metadata: count=%d err=%v", len(chunks), err)
	}
}

func TestMarkdownExtractor(t *testing.T) {
	ex := NewMarkdownExtractor()
	if ex.Format() != FormatMarkdown {
		t.Fatalf("format = %q", ex.Format())
	}
	bom := "\uFEFF"
	tests := []struct {
		name string
		in   string
		want []Segment
	}{
		{
			"headings nested",
			"intro\n\n# A\nteks a\n## B\nteks b\n### C\nteks c\n## D\nteks d\n# E\nteks e\n",
			[]Segment{
				{Text: "intro"},
				{Heading: []string{"A"}, Text: "teks a"},
				{Heading: []string{"A", "B"}, Text: "teks b"},
				{Heading: []string{"A", "B", "C"}, Text: "teks c"},
				{Heading: []string{"A", "D"}, Text: "teks d"},
				{Heading: []string{"E"}, Text: "teks e"},
			},
		},
		{
			"heading without body is skipped",
			"# A\n## B\nisi b\n",
			[]Segment{{Heading: []string{"A", "B"}, Text: "isi b"}},
		},
		{
			"crlf",
			"# Judul\r\nbaris satu\r\nbaris dua\r\n",
			[]Segment{{Heading: []string{"Judul"}, Text: "baris satu\nbaris dua"}},
		},
		{
			"bom",
			bom + "# Judul\nisi",
			[]Segment{{Heading: []string{"Judul"}, Text: "isi"}},
		},
		{
			"indonesian and emoji",
			"# Pengantar 😀\nSelamat datang di Yogyakarta, kota pelajar.",
			[]Segment{{Heading: []string{"Pengantar 😀"}, Text: "Selamat datang di Yogyakarta, kota pelajar."}},
		},
		{
			"hash inside fence ignored",
			"# A\n```sh\n# bukan heading\necho hi\n```\nsesudah\n",
			[]Segment{{Heading: []string{"A"}, Text: "```sh\n# bukan heading\necho hi\n```\nsesudah"}},
		},
		{
			"tilde fence ignored",
			"~~~\n# x\n~~~\n# Nyata\nok",
			[]Segment{{Text: "~~~\n# x\n~~~"}, {Heading: []string{"Nyata"}, Text: "ok"}},
		},
		{
			"front matter dropped",
			"---\ntitle: x\ntags: [a]\n---\n# H\nisi",
			[]Segment{{Heading: []string{"H"}, Text: "isi"}},
		},
		{
			"unterminated front matter kept",
			"---\ntitle: x\n",
			[]Segment{{Text: "---\ntitle: x"}},
		},
		{
			"hash without space is not heading",
			"#hashtag\nisi",
			[]Segment{{Text: "#hashtag\nisi"}},
		},
		{
			"closing hashes and markup preserved",
			"## Judul ##\n**tebal** dan `kode`",
			[]Segment{{Heading: []string{"Judul"}, Text: "**tebal** dan `kode`"}},
		},
		{
			"seven hashes is text",
			"####### x",
			[]Segment{{Text: "####### x"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ex.Extract(context.Background(), []byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if !reflect.DeepEqual(append([]string(nil), got[i].Heading...), append([]string(nil), tc.want[i].Heading...)) ||
					got[i].Text != tc.want[i].Text || got[i].Locator != tc.want[i].Locator {
					t.Fatalf("segment %d: got %#v, want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestTextExtractor(t *testing.T) {
	ex := NewTextExtractor()
	if ex.Format() != FormatText {
		t.Fatalf("format = %q", ex.Format())
	}
	got, err := ex.Extract(context.Background(), []byte("\uFEFFhalo\r\ndunia 😀\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "halo\ndunia 😀" || len(got[0].Heading) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestMarkdownTextCommonErrors(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ex := range []Extractor{NewMarkdownExtractor(), NewTextExtractor()} {
		t.Run(string(ex.Format()), func(t *testing.T) {
			empties := []string{"", "   \n\t\r\n", "\uFEFF"}
			if ex.Format() == FormatMarkdown {
				empties = append(empties, "---\na: b\n---\n")
			}
			for _, in := range empties {
				if _, err := ex.Extract(context.Background(), []byte(in)); !errors.Is(err, ErrEmpty) {
					t.Fatalf("input %q: err = %v, want ErrEmpty", in, err)
				}
			}
			big := []byte(strings.Repeat("a", MaxExtractedChars+1))
			if _, err := ex.Extract(context.Background(), big); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
			if _, err := ex.Extract(canceled, []byte("# a\nb")); !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			r := rand.New(rand.NewSource(7))
			for i := 0; i < 500; i++ {
				b := make([]byte, r.Intn(400))
				r.Read(b)
				_, _ = ex.Extract(context.Background(), b)
			}
		})
	}
}
