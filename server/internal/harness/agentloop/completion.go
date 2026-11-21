package agentloop

import (
	"regexp"
	"strings"
)

// This file ports the anti-stall continuation heuristics from the OpenClaude
// CLI (src/utils/continuation.ts). The runner uses them after a model stops
// with text instead of tool calls: if the turn already executed tools but the
// closing text signals unfinished work (or is empty), the harness re-prompts
// the model instead of ending the turn prematurely.

const continuationWindow = 120

var actionVerbs = []string{
	"do", "create", "write", "edit", "update", "fix", "implement", "add", "run",
	"check", "make", "build", "set up", "start", "begin", "go", "proceed", "apply",
	"identify", "inspect", "analyze", "review", "search", "process", "download",
	"upload", "convert", "compile", "train", "evaluate", "test", "continue",
	"generate", "extract", "merge", "deploy", "install", "configure", "refactor",
	"optimize", "summarize",
}

var indonesianVerbs = []string{
	"membuat", "menulis", "mengedit", "memperbarui", "memperbaiki", "menambahkan",
	"menjalankan", "memeriksa", "mengecek", "memproses", "menguji",
	"mengimplementasikan", "mengerjakan", "memverifikasi", "membaca", "mencari",
	"mengunduh", "mengunggah", "mengonversi", "menggabungkan", "menerapkan",
	"menginstal", "mengonfigurasi", "merefaktor", "mengoptimalkan",
	"menganalisis", "meninjau", "merangkum", "memastikan", "menyiapkan",
	"melanjutkan", "lanjut", "mulai",
}

var verbIng = []string{
	"doing", "creating", "writing", "editing", "updating", "fixing",
	"implementing", "adding", "running", "checking", "making", "building",
	"setting up", "starting", "beginning", "going", "proceeding", "applying",
	"identifying", "inspecting", "analyzing", "reviewing", "searching",
	"processing", "downloading", "uploading", "converting", "compiling",
	"training", "evaluating", "testing", "continuing", "generating",
	"extracting", "merging", "deploying", "installing", "configuring",
	"refactoring", "optimizing", "summarizing",
}

var (
	verbAlt           = strings.Join(actionVerbs, "|")
	verbAltWithoutDo  = strings.Join(without(actionVerbs, "do"), "|")
	verbIngAlt        = strings.Join(verbIng, "|")
	indonesianVerbAlt = strings.Join(indonesianVerbs, "|")
)

var continuationSignals = []*regexp.Regexp{
	// English: action-transition phrases (intent + action).
	regexp.MustCompile(`\bso now (i|let me|we) (need to|have to|should|must|will) (` + verbAlt + `)\b`),
	regexp.MustCompile(`\bnow i('ll| will) (` + verbAlt + `)\b`),
	regexp.MustCompile(`\bi (will|shall|now|need to|have to|must|should) (now )?(` + verbAlt + `)\b`),
	regexp.MustCompile(`\blet me (go ahead and |now )?(` + verbAlt + `)\b`),
	regexp.MustCompile(`\btime to (do|` + verbAltWithoutDo + `|get started|begin|start)\b`),
	regexp.MustCompile(`\b(moving on to|next step is to|starting to|proceeding to|continuing with|applying (the|these) changes|` + verbIngAlt + `)\b`),
	// Universal: a trailing colon announces an upcoming list or action.
	regexp.MustCompile(`:\s*$`),
	// Universal: an open-task checkbox marks pending work.
	regexp.MustCompile(`◻`),
	// Indonesian: strong first-person intent.
	regexp.MustCompile(`\bsaya (akan|ingin|harus|perlu|mau|sedang|berencana) (segera |dulu |sekarang )?(` + indonesianVerbAlt + `)\b`),
	regexp.MustCompile(`\bsaya (cek|periksa|lihat|baca|tanyakan)\b`),
	// Indonesian: transition + optional subject/aux + verb.
	regexp.MustCompile(`\b(lalu|kemudian|selanjutnya|setelah (itu|ini)|sesudah itu|sekarang|berikutnya) (saya |kita )?(akan |ingin |harus |perlu |mau )?(` + indonesianVerbAlt + `)\b`),
	regexp.MustCompile(`\bmari (kita )?(` + indonesianVerbAlt + `)\b`),
	regexp.MustCompile(`\blangkah (berikutnya|selanjutnya) (adalah|yakni|:)\b`),
	regexp.MustCompile(`\bwaktunya (untuk )?(` + indonesianVerbAlt + `|melanjutkan)\b`),
	regexp.MustCompile(`\bsedang (` + indonesianVerbAlt + `)\b`),
}

var (
	completionMarker = regexp.MustCompile(`\b(done|finished|completed|complete|summary|that's all|that is all|all set|hope this helps|let me know if|no issues|lgtm|selesai|lengkap|beres|tuntas)\b`)

	strongContinuationIntent = regexp.MustCompile(`\b(i (will|shall|need to|must|should|now)|let (me|us)|moving on to|continuing with|proceeding to|next step is to)\b`)
	strongIndonesianIntent   = regexp.MustCompile(`\b(saya (akan|ingin|mau|harus|perlu|sedang|cek|periksa|lihat|baca)|mari (kita)?|lalu (saya|kita)|langkah (berikutnya|selanjutnya)|mulai|melanjutkan|lanjut)\b`)

	progressivePattern = regexp.MustCompile(`\b(?:now (?:` + verbIngAlt + `)|sedang (?:` + indonesianVerbAlt + `))\b`)
	needToPattern      = regexp.MustCompile(`\bneed to (` + verbAlt + `)\b`)
	nowVerbPattern     = regexp.MustCompile(`\bnow (` + verbAlt + `)\b`)
	nextPattern        = regexp.MustCompile(`\bnext (i|we)\s+(need to|will|shall|should|must)?\s*(` + verbAlt + `)\b`)
	lanjutPattern      = regexp.MustCompile(`\b(lanjut|lanjutkan|melanjutkan|mulai)\b`)

	waitingMarkers = []*regexp.Regexp{
		regexp.MustCompile(`\bsaya (?:akan )?(?:menunggu|nunggu|tunggu)\b`),
		regexp.MustCompile(`\b(?:menunggu|nunggu|tunggu)\s+(?:hasil|hasilnya|notifikasi|verifik|agent|subagent|proses|verifikasi)\b`),
		regexp.MustCompile(`\b(?:agent|verifier|subagent|sub-agent|proses|verifikasi)\s+(?:masih|sedang)\s+(?:bekerja|berjalan|memproses|menganalisis)\b`),
		regexp.MustCompile(`\bberhenti di sini\b`),
		regexp.MustCompile(`\b(?:wait|waiting|waits)\s+(?:for|on)\s+(?:the )?(?:agent|subagent|sub-agent|verifier|result|notification)\b`),
		regexp.MustCompile(`\b(?:i|we)\s+(?:will|'ll|need to)?\s*wait\s+(?:for|on)\b`),
		regexp.MustCompile(`\bstill (?:waiting|running|working|in progress)\b.*\b(?:agent|subagent|sub-agent|verifier|task)\b`),
	}

	// A strong action intent after a waiting marker means the model still plans
	// to work, so the waiting marker must not suppress the nudge.
	strongWaitOverride = regexp.MustCompile(`\b(saya (akan|ingin|mau|harus|perlu|sedang|cek|periksa|lihat|baca|tanyakan|` + indonesianVerbAlt + `)|` +
		`\b(berikutnya|setelah itu|sesudah itu|lalu|dahulu|sekarang)\s*(saya|kita)?\s*(` + indonesianVerbAlt + `)|` +
		`\b(menyusul|lalu saya|kemudian saya|lanjut|melanjutkan)\b|` +
		`\b(i\s+(will|'ll|need to|must|should)\s+(?:` + verbAlt + `)\b|let me\s+(?:` + verbAlt + `)\b|moving (on to|to)\s+(?:` + verbAlt + `)|next (i|we)\s+(?:` + verbAlt + `)|then (i|we)\s+(?:` + verbAlt + `)))\b`)
)

// shouldContinue reports whether the harness should re-prompt a model that
// stopped with text instead of tool calls. alreadyNudged suppresses the
// completion-spam loop: once nudged, a plain "done"/"selesai" reply is final.
func shouldContinue(text string, alreadyNudged bool) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return true
	}
	if strings.HasSuffix(lower, "?") {
		return false
	}
	if isWaitingForAgent(lower) {
		return false
	}

	late := tailRunes(lower, continuationWindow)
	terminal := endsWithTerminalPunctuation(lower)
	strongIntent := strongContinuationIntent.MatchString(lower) || strongIndonesianIntent.MatchString(lower)
	progressive := progressivePattern.MatchString(late)

	hasLateSignal := false
	for _, re := range continuationSignals {
		loc := re.FindStringIndex(late)
		if loc == nil {
			continue
		}
		match := late[loc[0]:loc[1]]
		strong := strongContinuationIntent.MatchString(match) || strongIndonesianIntent.MatchString(match)
		if strong || !completionMarker.MatchString(late[loc[1]:]) {
			hasLateSignal = true
			break
		}
	}

	if hasLateSignal {
		if terminal {
			// A punctuated sentence with a transition word only counts when the
			// intent is strong, the text ends with a colon, or a progressive /
			// imperative phrase signals work in flight.
			if strongIntent || strings.HasSuffix(lower, ":") || progressive ||
				needToPattern.MatchString(late) || nowVerbPattern.MatchString(late) ||
				nextPattern.MatchString(late) || lanjutPattern.MatchString(late) {
				return true
			}
		} else if !(completionMarker.MatchString(late) && !progressive && !strongIntent) {
			return true
		}
	}

	// Completion-insistence guard: after a nudge, a reply that again declares the
	// task done without a strong intent to continue is the model's final answer.
	if alreadyNudged && completionMarker.MatchString(lower) && !strongIntent && !progressive {
		return false
	}

	if completionMarker.MatchString(lower) && !hasLateSignal && !anyContinuationSignal(lower) {
		return false
	}

	if anyContinuationSignal(lower) && !terminal {
		if !completionMarker.MatchString(lower) || strongIntent {
			return true
		}
	}

	// Pragmatic fallback for Indonesian chat: a strong first-person intent in the
	// final clause ("Saya akan cek lagi.") is a stall even when the verb is not
	// in the fixed list. The clause scope avoids flagging intent mentioned early
	// in an otherwise complete answer.
	final := finalClause(lower)
	if !completionMarker.MatchString(final) &&
		(strongContinuationIntent.MatchString(final) || strongIndonesianIntent.MatchString(final)) {
		return true
	}
	return false
}

func isWaitingForAgent(lower string) bool {
	matched := false
	for _, re := range waitingMarkers {
		if re.MatchString(lower) {
			matched = true
			break
		}
	}
	return matched && !strongWaitOverride.MatchString(lower)
}

func anyContinuationSignal(lower string) bool {
	for _, re := range continuationSignals {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}

func endsWithTerminalPunctuation(s string) bool {
	trimmed := strings.TrimRight(s, " \t\r\n")
	if trimmed == "" {
		return false
	}
	last := trimmed[len(trimmed)-1]
	switch last {
	case '.', '!', '?', '"', '\'', '`', ')', ']':
		return true
	}
	return false
}

// finalClause returns the text after the last sentence terminator, ignoring
// trailing punctuation, so intent mentioned only at the very end is detected.
func finalClause(s string) string {
	trimmed := strings.TrimRight(s, " \t\r\n.!?\"'`)]")
	if idx := strings.LastIndexAny(trimmed, ".!?"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return trimmed
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func without(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}
