package dotbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
)

type memStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemStore() *memStore {
	return &memStore{data: map[string]string{}}
}

func (s *memStore) key(service, account string) string {
	return service + "\x00" + account
}

func (s *memStore) Get(service, account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[s.key(service, account)]
	if !ok {
		return "", errNotPaired
	}
	return v, nil
}

func (s *memStore) Set(service, account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[s.key(service, account)] = secret
	return nil
}

func (s *memStore) Delete(service, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, s.key(service, account))
	return nil
}

// queueDigestReference mirrors dotqueue/submissionDigest for fixture checks.
func queueDigestReference(sub Submission) (string, error) {
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
