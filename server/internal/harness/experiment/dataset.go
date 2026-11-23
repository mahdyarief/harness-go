// Package experiment runs arms over one dataset and reports generic metrics.
// It carries no domain knowledge: the consumer defines the arms and the label
// field path; the harness only measures.
package experiment

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Item is one dataset row. Input is the arm's request; Label is the expected
// answer used by the metrics. Split lets a fixture mix dev and heldout rows.
type Item struct {
	ID    string          `json:"id"`
	Split string          `json:"split,omitempty"`
	Input json.RawMessage `json:"input"`
	Label json.RawMessage `json:"label,omitempty"`
}

// Dataset is a validated set of items. The order of Items is the order arms
// run and the order results are reported in.
type Dataset struct {
	Items []Item
}

// LoadDataset reads a JSONL dataset from path.
func LoadDataset(path string) (Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataset{}, err
	}
	defer f.Close()
	return DecodeDataset(f)
}

// DecodeDataset reads one JSON object per line. Blank lines are ignored and
// every row is checked before an arm sees it.
func DecodeDataset(r io.Reader) (Dataset, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var ds Dataset
	seen := map[string]bool{}
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var item Item
		if err := json.Unmarshal([]byte(text), &item); err != nil {
			return Dataset{}, fmt.Errorf("baris %d: %w", line, err)
		}
		if err := validateItem(item); err != nil {
			return Dataset{}, fmt.Errorf("baris %d: %w", line, err)
		}
		if seen[item.ID] {
			return Dataset{}, fmt.Errorf("baris %d: id %q duplikat", line, item.ID)
		}
		seen[item.ID] = true
		ds.Items = append(ds.Items, item)
	}
	if err := scanner.Err(); err != nil {
		return Dataset{}, err
	}
	if len(ds.Items) == 0 {
		return Dataset{}, fmt.Errorf("dataset kosong")
	}
	return ds, nil
}

func validateItem(item Item) error {
	if item.ID == "" || strings.TrimSpace(item.ID) != item.ID {
		return fmt.Errorf("id wajib dan tanpa spasi tepi")
	}
	if len(item.Input) == 0 || !json.Valid(item.Input) {
		return fmt.Errorf("item %s: input harus JSON valid", item.ID)
	}
	if len(item.Label) > 0 && !json.Valid(item.Label) {
		return fmt.Errorf("item %s: label harus JSON valid", item.ID)
	}
	return nil
}
