package dotbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type canonicalSubmission struct {
	RequestID string             `json:"request_id"`
	Mode      string             `json:"mode"`
	SessionID string             `json:"session_id"`
	Messages  []canonicalMessage `json:"messages"`
	Task      string             `json:"task"`
}

type canonicalMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Digest reproduces dotqueue/submissionDigest canonical JSON and SHA-256 hex.
func Digest(sub Submission) (string, error) {
	msgs := make([]canonicalMessage, len(sub.Messages))
	for i, m := range sub.Messages {
		msgs[i] = canonicalMessage{Role: m.Role, Content: m.Content}
	}
	body, err := json.Marshal(canonicalSubmission{
		RequestID: sub.RequestID,
		Mode:      sub.Mode,
		SessionID: sub.SessionID,
		Messages:  msgs,
		Task:      sub.Task,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
