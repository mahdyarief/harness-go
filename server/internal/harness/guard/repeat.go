package guard

import "sync"

type RepeatDecision struct{ Flagged bool }

type RepeatTool struct {
	mu      sync.Mutex
	window  int
	history []string
}

func NewRepeatTool(window int) *RepeatTool {
	if window < 1 {
		window = 1
	}
	return &RepeatTool{window: window}
}

func (r *RepeatTool) Check(tool, args string) RepeatDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tool + "\x00" + args
	for _, h := range r.history {
		if h == key {
			return RepeatDecision{Flagged: true}
		}
	}
	r.history = append(r.history, key)
	if len(r.history) > r.window {
		r.history = r.history[len(r.history)-r.window:]
	}
	return RepeatDecision{}
}
