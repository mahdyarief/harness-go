package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// DefaultRepeatErrorLimit is how many times the same failing call may happen in
// one run before the guard ends it. Two matches simgos: the first failure is
// the model's chance to correct itself, the second proves it is not doing so.
const DefaultRepeatErrorLimit = 2

// repeatedErrorAnswer is shown as the final answer when the guard fires. It is
// deliberately generic: no tool name, arguments, or provider text.
const repeatedErrorAnswer = "Saya menghentikan proses karena langkah yang sama terus gagal. " +
	"Silakan coba lagi nanti atau ubah permintaan Anda."

// RepeatedErrorGuard is a circuit breaker for a model that keeps issuing the
// same failing tool call. Without it the run burns through its turn and tool
// budgets repeating one mistake.
//
// A call counts as "the same" when the tool, the failure kind, and the
// arguments all match. Arguments are part of the key because the failure kind
// alone is coarse (every execution failure is recorded as one generic error),
// so keying on it would also stop a model that is legitimately trying
// different inputs. Rejections from a DomainGuard are counted like any other
// failure, so a model that ignores a guard's correction is stopped too.
type RepeatedErrorGuard struct {
	limit int
}

// NewRepeatedErrorGuard returns the guard with the default limit.
func NewRepeatedErrorGuard() *RepeatedErrorGuard {
	return &RepeatedErrorGuard{limit: DefaultRepeatErrorLimit}
}

// WithLimit returns a guard that fires on the nth identical failure. A limit
// below 2 is raised to 2, because stopping on the first failure would leave
// the model no chance to recover.
func (g *RepeatedErrorGuard) WithLimit(n int) *RepeatedErrorGuard {
	if n < DefaultRepeatErrorLimit {
		n = DefaultRepeatErrorLimit
	}
	return &RepeatedErrorGuard{limit: n}
}

func (g *RepeatedErrorGuard) Name() string { return "repeated-error" }

func (g *RepeatedErrorGuard) Inspect(mem Memory, turn LoopTurn) Verdict {
	limit := g.limit
	if limit < DefaultRepeatErrorLimit {
		limit = DefaultRepeatErrorLimit
	}
	for _, record := range turn.Records {
		sig := errorSignature(record)
		if sig == "" {
			continue
		}
		if mem.Incr(sig) >= limit {
			return Stop(repeatedErrorAnswer)
		}
	}
	return Allow()
}

// errorSignature identifies a failed call, or returns "" for one that
// succeeded. Equal signatures mean the same tool failed the same way on the
// same input.
func errorSignature(record ToolRecord) string {
	if record.Err == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(record.Name + "\x00" + record.Err + "\x00" + canonicalArgs(record.Arguments)))
	return hex.EncodeToString(sum[:8])
}

// canonicalArgs makes argument JSON comparable regardless of key order and
// whitespace. Arguments that are not valid JSON fall back to a
// whitespace-collapsed form, so a malformed repeat still matches itself.
func canonicalArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		if out, err := json.Marshal(v); err == nil {
			return string(out)
		}
	}
	return strings.Join(strings.Fields(raw), " ")
}
