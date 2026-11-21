package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"hsg/internal/harness/services"
)

const DefaultRepeatErrorLimit = 2

const repeatedErrorAnswer = "Saya menghentikan proses karena langkah yang sama terus gagal. " +
	"Silakan coba lagi nanti atau ubah permintaan Anda."

// RepeatedErrorGuard ends a run when the same tool keeps failing on the same
// arguments. It is a circuit breaker, not a security boundary.
type RepeatedErrorGuard struct{ limit int }

func NewRepeatedErrorGuard() *RepeatedErrorGuard {
	return &RepeatedErrorGuard{limit: DefaultRepeatErrorLimit}
}

func (g *RepeatedErrorGuard) WithLimit(n int) *RepeatedErrorGuard {
	if n < DefaultRepeatErrorLimit {
		n = DefaultRepeatErrorLimit
	}
	return &RepeatedErrorGuard{limit: n}
}

func (g *RepeatedErrorGuard) Name() string { return "repeated-error" }

func (g *RepeatedErrorGuard) Inspect(mem services.GuardMemory, turn services.GuardTurn) services.GuardVerdict {
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
			return services.Stop(repeatedErrorAnswer)
		}
	}
	return services.Allow()
}

func errorSignature(record services.ToolRecord) string {
	if record.Err == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(record.Name + "\x00" + record.Err + "\x00" + canonicalArgs(record.Arguments)))
	return hex.EncodeToString(sum[:8])
}

func canonicalArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		if out, err := json.Marshal(v); err == nil {
			return string(out)
		}
	}
	return strings.Join(strings.Fields(raw), " ")
}
