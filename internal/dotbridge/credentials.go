package dotbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/yetone/magpie/internal/settings"
	"github.com/zalando/go-keyring"
)

const keyringService = "Magpie/DotBridge"

// Store persists pairing secrets for tests and production.
type Store interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

var credentialStore Store = keyringStore{}

// SetCredentialStore replaces the process credential backend (tests only).
func SetCredentialStore(store Store) {
	if store == nil {
		credentialStore = keyringStore{}
		return
	}
	credentialStore = store
}

type keyringStore struct{}

func (keyringStore) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (keyringStore) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}

func (keyringStore) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

func credentialAccount(reference string) string {
	base, err := filepath.Abs(settings.Dir())
	if err != nil {
		base = settings.Dir()
	}
	sum := sha256.Sum256([]byte(base + "\x00" + reference))
	return hex.EncodeToString(sum[:])
}

func loadPairToken(reference string) (string, error) {
	if reference == "" {
		return "", errNotPaired
	}
	secret, err := credentialStore.Get(keyringService, credentialAccount(reference))
	if err != nil {
		if errors.Is(err, errNotPaired) || errors.Is(err, keyring.ErrNotFound) {
			return "", errNotPaired
		}
		return "", sanitizeTokenErr(err)
	}
	if secret == "" {
		return "", errNotPaired
	}
	return secret, nil
}

func savePairToken(reference, token string) error {
	if reference == "" || token == "" {
		return errNotPaired
	}
	return credentialStore.Set(keyringService, credentialAccount(reference), token)
}

func deletePairToken(reference string) error {
	if reference == "" {
		return nil
	}
	return credentialStore.Delete(keyringService, credentialAccount(reference))
}
