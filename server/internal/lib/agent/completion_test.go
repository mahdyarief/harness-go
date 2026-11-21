package agent

import "testing"

func TestCompletionHeuristics(t *testing.T) {
	cases := []struct {
		text        string
		nudged      bool
		shouldNudge bool
	}{
		{"", false, true},
		{"", true, true},

		// Replies that conclude the task -> no nudge.
		{"Semua selesai.", false, false},
		{"Semua selesai.", true, false},
		{"Tugas selesai.", false, false},
		{"beres.", false, false},
		{"tuntas", false, false},
		{"lengkap", false, false},
		{"Done.", false, false},
		{"All set.", false, false},
		{"No issues found.", false, false},
		{"LGTM, merged.", false, false},

		// Continuation intent overrides a completion marker.
		{"Tugas selesai, lalu saya akan mengecek lagi.", false, true},
		{"Done. Next I will fix the parser.", false, true},

		// Explicit future intent mid-thought -> nudge.
		{"Saya akan cek lagi", false, true},
		{"Mari lanjutkan implementasinya.", false, true},
		{"Setelah itu saya menjalankan tes.", false, false},
		{"Selanjutnya saya menambahkan validasi.", false, false},
		{"next i will fix it", false, true},
		{"Let me go ahead and check the build.", false, true},
		{"Moving on to the next step.", false, true},
		{"Sedang memeriksa hasil.", false, true},

		// Colon / open task marker signal pending work.
		{"Rencana perbaikan:", false, true},
		{"Todo:", false, true},

		// An actual question -> waiting for the user, no nudge.
		{"Apa yang harus saya lakukan?", false, false},
		{"Should I deploy now?", false, false},

		// Waiting on an external agent -> no nudge.
		{"Menunggu hasil verifikasi dari subagent.", false, false},
		{"Saya akan menunggu agent selesai.", false, false},
		{"Still waiting for the background agent to finish.", false, false},
		{"berhenti di sini", false, false},

		// Weak / conversational text with no intent -> no nudge.
		{"Hmm", false, false},
		{"Hmm", true, false},
		{"Oke", false, false},
		{"oke", true, false},
		{"Baik, akan saya kerjakan besok.", false, false},
		{"Let me know if you need anything else.", false, false},

		// After a nudge, a plain "done" reply is accepted (no spam loop).
		{"Selesai.", true, false},
		{"Done.", true, false},
		{"Tugas beres.", true, false},

		// After a nudge, a reply that still commits to work keeps nudging.
		{"Saya akan mengecek sekali lagi.", true, true},
		{"Lalu saya membuat file baru.", true, true},
	}
	for _, c := range cases {
		got := shouldContinue(c.text, c.nudged)
		if got != c.shouldNudge {
			t.Errorf("text=%q nudged=%v: want %v got %v", c.text, c.nudged, c.shouldNudge, got)
		}
	}
}
