package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// CodeReplayMiss is returned when a replayed request has no recorded answer.
// Replay never falls back to the provider: a missing fixture is a hard error
// so a test run cannot silently spend money or hide nondeterminism.
const CodeReplayMiss = "REPLAY_MISS"

// CodeReplayRecord marks a failure to persist a recording.
const CodeReplayRecord = "REPLAY_RECORD"

// ModelNamer lets an inner LLM contribute its model name to the replay key.
// The harness interface predates model naming, so it stays optional; an
// adapter that implements it gets stronger keys for free.
type ModelNamer interface {
	Model() string
}

// Store is a directory of JSONL fixtures, one file per request key. The
// layout is intentionally plain so a fixture can be committed, diffed, and
// archived alongside the test that replays it.
type Store struct {
	dir string
	mu  sync.Mutex
}

// OpenStore creates dir when missing and returns a store rooted there.
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("llm: direktori rekaman wajib")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(key string) string { return filepath.Join(s.dir, key+".jsonl") }

// replayRequest is the exact material hashed into a key. Field order is fixed
// so identical inputs always produce identical bytes.
type replayRequest struct {
	Model       string        `json:"model"`
	Messages    []ai.Message  `json:"messages"`
	Tools       []ai.ToolSpec `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature *float64      `json:"temperature,omitempty"`
	Seed        *int          `json:"seed,omitempty"`
}

type replayEntry struct {
	Key        string           `json:"key"`
	Result     ai.ChatResult    `json:"result"`
	Deltas     []ai.StreamDelta `json:"deltas"`
	ErrCode    string           `json:"error_code,omitempty"`
	ErrMessage string           `json:"error_message,omitempty"`
}

func newReplayRequest(model string, messages []ai.Message, opts ai.ChatOptions) replayRequest {
	return replayRequest{
		Model:       model,
		Messages:    messages,
		Tools:       opts.Tools,
		ToolChoice:  opts.ToolChoice,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		Seed:        opts.Seed,
	}
}

func requestKey(model string, messages []ai.Message, opts ai.ChatOptions) string {
	body, err := json.Marshal(newReplayRequest(model, messages, opts))
	if err != nil {
		// The request types are plain structs; a marshal failure is a
		// programming error, and an unusable key must not collide silently.
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (s *Store) write(key string, entry replayEntry) error {
	if key == "" {
		return fmt.Errorf("llm: kunci rekaman kosong")
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.WriteFile(s.path(key), append(body, '\n'), 0o644)
}

func (s *Store) read(key string) (replayEntry, error) {
	if key == "" {
		return replayEntry{}, os.ErrNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path(key))
	if err != nil {
		return replayEntry{}, err
	}
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return replayEntry{}, fmt.Errorf("llm: rekaman %s kosong", key)
	}
	var entry replayEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return replayEntry{}, err
	}
	return entry, nil
}

// Keys lists every recorded key, sorted. Diagnostic only.
func (s *Store) Keys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		keys = append(keys, strings.TrimSuffix(name, ".jsonl"))
	}
	sort.Strings(keys)
	return keys, nil
}

type recorder struct {
	inner services.LLM
	store *Store
	model string
}

// Record wraps inner so every successful or failed call is persisted. The
// provider remains authoritative while recording; a failed write is reported
// as REPLAY_RECORD only when the call itself succeeded.
func Record(inner services.LLM, store *Store) services.LLM {
	return &recorder{inner: inner, store: store, model: modelOf(inner)}
}

func modelOf(llm services.LLM) string {
	if named, ok := llm.(ModelNamer); ok {
		return named.Model()
	}
	return ""
}

func (r *recorder) MaxTokens() int { return r.inner.MaxTokens() }

func (r *recorder) ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	deltas := make([]ai.StreamDelta, 0, 16)
	result, err := r.inner.ChatStream(ctx, messages, opts, func(delta ai.StreamDelta) error {
		deltas = append(deltas, delta)
		return onDelta(delta)
	})
	key := requestKey(r.model, messages, opts)
	entry := replayEntry{Key: key, Result: result, Deltas: deltas}
	if err != nil {
		entry.ErrCode = FailureCodeOf(err)
		entry.ErrMessage = err.Error()
	}
	if writeErr := r.store.write(key, entry); writeErr != nil && err == nil {
		return result, NewFailure("rekaman tidak dapat disimpan", CodeReplayRecord, writeErr)
	}
	return result, err
}

type replayer struct {
	store     *Store
	model     string
	maxTokens int
}

// Replay answers only from the store. The model name is empty, matching a
// recorder whose inner never implemented ModelNamer; use ReplayAs when the
// recorded adapter names a model.
func Replay(store *Store) services.LLM { return &replayer{store: store} }

// ReplayAs pins the model name and token ceiling used in the key.
func ReplayAs(store *Store, model string, maxTokens int) services.LLM {
	return &replayer{store: store, model: model, maxTokens: maxTokens}
}

func (r *replayer) MaxTokens() int { return r.maxTokens }

func (r *replayer) ChatStream(_ context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	key := requestKey(r.model, messages, opts)
	entry, err := r.store.read(key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ai.ChatResult{}, NewFailure("rekaman untuk permintaan ini tidak ditemukan", CodeReplayMiss, err)
		}
		return ai.ChatResult{}, NewFailure("rekaman tidak dapat dibaca", CodeReplayRecord, err)
	}
	for _, delta := range entry.Deltas {
		if err := onDelta(delta); err != nil {
			return ai.ChatResult{}, err
		}
	}
	if entry.ErrCode != "" {
		return entry.Result, NewFailure(entry.ErrMessage, entry.ErrCode, nil)
	}
	return entry.Result, nil
}
