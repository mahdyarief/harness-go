package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const gentleReminder = "You are repeating the exact same tool call with identical arguments. " +
	"Carefully analyze the previous result before calling again: if the task is " +
	"not complete, try a different approach or different arguments instead of " +
	"repeating the call."

type ReminderConfig struct {
	Thresholds            []int
	Include               []string
	Exclude               []string
	ArgumentsPreviewChars int
}

type Reminder struct {
	Text    string
	Source  string
	Form    string
	Summary string
}

type ReminderTool struct {
	thresholds   []int
	thresholdSet map[int]bool
	include      []*regexp.Regexp
	exclude      []*regexp.Regexp
	preview      int
	key          string
	count        int
}

func NewReminderTool(cfg ReminderConfig) (ReminderTool, error) {
	thresholds, err := validateThresholds(cfg.Thresholds)
	if err != nil {
		return ReminderTool{}, err
	}
	include, err := compilePatterns(cfg.Include)
	if err != nil {
		return ReminderTool{}, fmt.Errorf("invalid include pattern: %w", err)
	}
	exclude, err := compilePatterns(cfg.Exclude)
	if err != nil {
		return ReminderTool{}, fmt.Errorf("invalid exclude pattern: %w", err)
	}
	preview := cfg.ArgumentsPreviewChars
	if preview == 0 {
		preview = 500
	}
	if preview < 1 {
		return ReminderTool{}, fmt.Errorf("invalid ArgumentsPreviewChars %d", preview)
	}
	set := make(map[int]bool, len(thresholds))
	for _, v := range thresholds {
		set[v] = true
	}
	return ReminderTool{
		thresholds:   thresholds,
		thresholdSet: set,
		include:      include,
		exclude:      exclude,
		preview:      preview,
	}, nil
}

func validateThresholds(values []int) ([]int, error) {
	if len(values) == 0 {
		return nil, errors.New("repeat-tool-reminder: `thresholds` must not be empty")
	}
	for _, v := range values {
		if v < 2 {
			return nil, errors.New("repeat-tool-reminder: thresholds must be at least 2")
		}
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			return nil, errors.New("repeat-tool-reminder: `thresholds` must not contain duplicates")
		}
	}
	for i := 1; i < len(values); i++ {
		if values[i] <= values[i-1] {
			return nil, errors.New("repeat-tool-reminder: thresholds must be strictly increasing")
		}
	}
	return sorted, nil
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, pattern := range patterns {
		var b strings.Builder
		b.WriteString("^")
		for _, r := range pattern {
			if r == '*' {
				b.WriteString(".*")
				continue
			}
			if strings.ContainsRune(`|\{}()[]^$+?.`, r) {
				b.WriteString(`\`)
			}
			b.WriteRune(r)
		}
		b.WriteString("$")
		re, err := regexp.Compile(b.String())
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

func (r *ReminderTool) tracked(tool string) bool {
	if len(r.include) > 0 {
		matched := false
		for _, re := range r.include {
			if re.MatchString(tool) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, re := range r.exclude {
		if re.MatchString(tool) {
			return false
		}
	}
	return true
}

func (r *ReminderTool) Observe(tool, arguments string) *Reminder {
	if !r.tracked(tool) {
		return nil
	}
	canonical := canonicalJSON(arguments)
	key := tool + "\x00" + canonical
	if key == r.key {
		r.count++
	} else {
		r.key = key
		r.count = 1
	}
	if !r.thresholdSet[r.count] {
		return nil
	}
	text := gentleReminder
	if r.count != r.thresholds[0] {
		text = detailedReminder(tool, r.count, previewArguments(canonical, r.preview))
	}
	return &Reminder{
		Text:    text,
		Source:  "repeat-tool-reminder",
		Form:    "notice",
		Summary: fmt.Sprintf("%s × %d", tool, r.count),
	}
}

func (r *ReminderTool) Reset() {
	r.key = ""
	r.count = 0
}

func canonicalJSON(raw string) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&value); err != nil {
		return raw
	}
	encoded, err := json.Marshal(sortJSON(value))
	if err != nil {
		return raw
	}
	return string(encoded)
}

func sortJSON(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sortJSON(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = sortJSON(item)
		}
		return out
	default:
		return value
	}
}

func previewArguments(canonical string, cap int) string {
	if len(canonical) <= cap {
		return canonical
	}
	return fmt.Sprintf("%s… (+%d more chars)", canonical[:cap], len(canonical)-cap)
}

func detailedReminder(tool string, count int, canonical string) string {
	return "Repeated tool call detected:\n" +
		"- tool: " + tool + "\n" +
		fmt.Sprintf("- consecutive_calls: %d\n", count) +
		"- arguments: " + canonical + "\n" +
		"The repeated calls are not making progress. Do not call this tool with " +
		"these exact arguments again. Inspect the latest result and choose a " +
		"different action, different arguments, or finish the task if enough " +
		"evidence has been gathered."
}
