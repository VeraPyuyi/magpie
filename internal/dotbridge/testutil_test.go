package dotbridge

import (
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
