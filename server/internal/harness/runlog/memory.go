package runlog

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"hsg/internal/harness/services"
)

// Memory is an in-process RunLog. It is useful for tests and deterministic
// replay fixtures; production uses the Postgres repository.
type Memory struct {
	mu     sync.Mutex
	runs   map[string]services.RunRecord
	events map[string][]services.RunLogEvent
}

func NewMemory() *Memory {
	return &Memory{runs: map[string]services.RunRecord{}, events: map[string][]services.RunLogEvent{}}
}

func (m *Memory) CreateRun(_ context.Context, run services.RunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.runs[run.ID]; exists {
		return fmt.Errorf("run %q already exists", run.ID)
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = "running"
	}
	m.runs[run.ID] = run
	m.events[run.ID] = nil
	return nil
}

func (m *Memory) AppendEvent(_ context.Context, event services.RunLogEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[event.RunID]; !ok {
		return fmt.Errorf("unknown run %q", event.RunID)
	}
	seq := int64(len(m.events[event.RunID]) + 1)
	event.Seq = seq
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	m.events[event.RunID] = append(m.events[event.RunID], event)
	return nil
}

func (m *Memory) FinishRun(_ context.Context, runID string, completion services.RunCompletion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return fmt.Errorf("unknown run %q", runID)
	}
	run.Status = completion.Status
	run.PromptTokens = completion.PromptTokens
	run.CompletionTokens = completion.CompletionTokens
	run.CostUSD = completion.CostUSD
	run.EndedAt = completion.EndedAt
	if run.EndedAt.IsZero() {
		run.EndedAt = time.Now().UTC()
	}
	m.runs[runID] = run
	return nil
}

func (m *Memory) Events(_ context.Context, runID string) ([]services.RunLogEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return nil, fmt.Errorf("unknown run %q", runID)
	}
	out := append([]services.RunLogEvent(nil), m.events[runID]...)
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (m *Memory) Run(runID string) (services.RunRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	return run, ok
}
