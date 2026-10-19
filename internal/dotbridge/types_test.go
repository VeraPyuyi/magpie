package dotbridge

import (
	"encoding/json"
	"testing"
)

func TestDotDigestMatchesQueue(t *testing.T) {
	empty := make([]Message, 0)
	sub := Submission{
		RequestID: "req-a",
		Mode:      ModeAnswer,
		SessionID: "sess-b",
		Messages:  empty,
		Task:      "",
	}
	got, err := Digest(sub)
	if err != nil {
		t.Fatal(err)
	}
	want, err := queueDigestReference(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("digest mismatch: %s vs %s", got, want)
	}
	body, err := json.Marshal(canonicalSubmission{
		RequestID: sub.RequestID,
		Mode:      sub.Mode,
		SessionID: sub.SessionID,
		Messages:  []canonicalMessage{},
		Task:      sub.Task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"request_id":"req-a","mode":"answer","session_id":"sess-b","messages":[],"task":""}` {
		t.Fatalf("empty messages encodes []: %s", body)
	}

	sub.Messages = []Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "yo"}}
	got2, _ := Digest(sub)
	want2, _ := queueDigestReference(sub)
	if got2 != want2 {
		t.Fatal("role order digest mismatch")
	}
	sub.Messages = []Message{{Role: "assistant", Content: "yo"}, {Role: "user", Content: "hi"}}
	got3, _ := Digest(sub)
	if got3 == got2 {
		t.Fatal("role order change must change digest")
	}
}
