package agentloop

import (
	"strings"
)

const continuationPrompt = "Lanjutkan pekerjaan yang belum selesai dari pesan Anda sebelumnya. Jangan ulangi bagian yang sudah Anda sampaikan."

func shouldContinue(text string, nudges int) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return true
	}
	if strings.HasSuffix(lower, "?") && nudges < 2 {
		return true
	}
	for _, phrase := range []string{
		"masih", "belum selesai", "lanjut", "akan saya", "saya akan", "selanjutnya",
		"mohon tunggu", "please wait", "let me", "i will", "i'll", "continuing", "still working",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
