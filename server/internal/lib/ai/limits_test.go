package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTokenAndRefusalLimits(t *testing.T) {
	good := provider(`{"label":"blue","evidence_id":"synthetic-1","quote":"The fictional parcel is blue."}`)
	cases := []string{strings.Replace(good, `"completion_tokens":10`, `"completion_tokens":101`, 1), strings.Replace(good, `"finish_reason":"stop"`, `"finish_reason":"length"`, 1), strings.Replace(good, `"message":{`, `"message":{"refusal":"no",`, 1), strings.Replace(good, `"message":{`, `"message":{"tool_calls":[{"name":"sql"}],`, 1)}
	for _, body := range cases {
		g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		out, err := g.SyntheticDemo(context.Background())
		s.Close()
		if err != ErrOutput || out.Label != "abstain" {
			t.Fatalf("%v %s", err, body)
		}
	}
}
func TestBodyDeadline(t *testing.T) {
	g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	defer s.Close()
	g.config.Timeout = 30 * time.Millisecond
	_, err := g.SyntheticDemo(context.Background())
	if err != context.DeadlineExceeded {
		t.Fatal(err)
	}
}
