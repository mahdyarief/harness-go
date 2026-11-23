package llm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type fakeLLM struct {
	result  ai.ChatResult
	err     error
	deltas  []string
	calls   int
	lastMsg []ai.Message
	lastOpt ai.ChatOptions
}

func (f *fakeLLM) MaxTokens() int { return 100 }

func (f *fakeLLM) ChatStream(_ context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	f.calls++
	f.lastMsg, f.lastOpt = messages, opts
	for _, text := range f.deltas {
		if err := onDelta(ai.StreamDelta{Content: text}); err != nil {
			return ai.ChatResult{}, err
		}
	}
	if err := onDelta(ai.StreamDelta{Done: true}); err != nil {
		return ai.ChatResult{}, err
	}
	return f.result, f.err
}

func TestRecordThenReplayIsIdentical(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeLLM{
		result: ai.ChatResult{Content: "halo", FinishReason: "stop", PromptTokens: 7, CompletionTokens: 3},
		deltas: []string{"ha", "lo"},
	}
	messages := []ai.Message{{Role: "user", Content: "sapa"}}
	opts := ai.ChatOptions{MaxTokens: 42}

	var liveDeltas []string
	if _, err := Record(inner, store).ChatStream(context.Background(), messages, opts, func(d ai.StreamDelta) error {
		liveDeltas = append(liveDeltas, d.Content)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("record called the provider %d times", inner.calls)
	}

	replayed := Replay(store)
	var gotDeltas []string
	result, err := replayed.ChatStream(context.Background(), messages, opts, func(d ai.StreamDelta) error {
		gotDeltas = append(gotDeltas, d.Content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != inner.result.Content || result.FinishReason != inner.result.FinishReason ||
		result.PromptTokens != inner.result.PromptTokens || result.CompletionTokens != inner.result.CompletionTokens {
		t.Fatalf("result = %+v; want %+v", result, inner.result)
	}
	if len(gotDeltas) != len(liveDeltas) || gotDeltas[0] != "ha" || gotDeltas[1] != "lo" {
		t.Fatalf("deltas = %v; want %v", gotDeltas, liveDeltas)
	}
}

func TestReplayNeverFallsBackToProvider(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Replay(store).ChatStream(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil })
	if err == nil {
		t.Fatal("missing fixture must fail")
	}
	if code := FailureCodeOf(err); code != CodeReplayMiss {
		t.Fatalf("code = %q; want %q", code, CodeReplayMiss)
	}
}

func TestRecordPersistsFailedAttempt(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeLLM{err: NewFailure("gagal", string(CodeTimeout), errors.New("timeout"))}
	messages := []ai.Message{{Role: "user", Content: "x"}}
	if _, err := Record(inner, store).ChatStream(context.Background(), messages, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil }); err == nil {
		t.Fatal("recording must forward the provider error")
	}
	_, err = Replay(store).ChatStream(context.Background(), messages, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil })
	if code := FailureCodeOf(err); code != string(CodeTimeout) {
		t.Fatalf("replayed error code = %q; want %q", code, CodeTimeout)
	}
}

func TestKeyCoversTemperatureAndSeed(t *testing.T) {
	base := []ai.Message{{Role: "user", Content: "x"}}
	plain := requestKey("m", base, ai.ChatOptions{})
	temp := 0.2
	withTemp := requestKey("m", base, ai.ChatOptions{Temperature: &temp})
	if plain == withTemp {
		t.Fatal("temperature must change the key")
	}
	seed := 7
	withSeed := requestKey("m", base, ai.ChatOptions{Seed: &seed})
	if plain == withSeed {
		t.Fatal("seed must change the key")
	}
	if requestKey("a", base, ai.ChatOptions{}) == requestKey("b", base, ai.ChatOptions{}) {
		t.Fatal("model must change the key")
	}
}

func TestChatOptionsJSONOmitsNilSampling(t *testing.T) {
	body, err := json.Marshal(ai.ChatOptions{MaxTokens: 5})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["temperature"]; ok {
		t.Fatalf("nil temperature leaked: %s", body)
	}
	if _, ok := decoded["seed"]; ok {
		t.Fatalf("nil seed leaked: %s", body)
	}
	temp := 0.0
	seed := 0
	body, err = json.Marshal(ai.ChatOptions{Temperature: &temp, Seed: &seed})
	if err != nil {
		t.Fatal(err)
	}
	decoded = map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["temperature"]; !ok {
		t.Fatalf("explicit zero temperature must be sent: %s", body)
	}
	if _, ok := decoded["seed"]; !ok {
		t.Fatalf("explicit zero seed must be sent: %s", body)
	}
}

func TestRecordedFixtureIsReplayableAfterReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeLLM{result: ai.ChatResult{Content: "ok", PromptTokens: 1, CompletionTokens: 2}}
	messages := []ai.Message{{Role: "user", Content: "persist"}}
	if _, err := Record(inner, store).ChatStream(context.Background(), messages, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Replay(reopened).ChatStream(context.Background(), messages, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" || result.PromptTokens != 1 || result.CompletionTokens != 2 {
		t.Fatalf("result = %+v", result)
	}
}

var _ services.LLM = (*fakeLLM)(nil)
