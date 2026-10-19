package dotbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/settings"
)

const (
	defaultAnswerWait = 120
	minAnswerWait     = 1
	maxAnswerWait     = 600
	defaultCallBudget = 10 * time.Second
)

// Config is machine-local Dot bridge settings (credential reference only).
type Config struct {
	Enabled           bool   `json:"enabled"`
	Address           string `json:"address"`
	Credential        string `json:"credential"`
	PairingGeneration string `json:"pairing_generation,omitempty"`
	AnswerWaitSeconds int    `json:"answer_wait_seconds"`
}

func configPath() string {
	return filepath.Join(settings.Dir(), "dot-bridge.json")
}

func normalizeConfig(cfg Config) Config {
	if cfg.AnswerWaitSeconds == 0 {
		cfg.AnswerWaitSeconds = defaultAnswerWait
	}
	return cfg
}

func validateConfig(cfg Config) error {
	if cfg.AnswerWaitSeconds < minAnswerWait || cfg.AnswerWaitSeconds > maxAnswerWait {
		return errInvalidConfig
	}
	if cfg.Enabled {
		if _, err := parseLoopbackAddress(cfg.Address); err != nil {
			return err
		}
		if cfg.Credential == "" {
			return errNotPaired
		}
	}
	return nil
}

// LoadConfig reads dot-bridge.json; a missing file is a disabled default.
func LoadConfig() (Config, error) {
	unlock, held := filelock.Lock(configPath()+".lock", filelock.Wait)
	if !held { return Config{}, errInvalidConfig }
	defer unlock()
	cfg, err := readConfig()
	if err != nil { return cfg, err }
	// Migrate legacy configurations once under the same cross-process lock as
	// writers. Never derive a public identifier from a secret or trust old proof.
	if cfg.Credential != "" && cfg.PairingGeneration == "" {
		cfg.PairingGeneration, err = newPairingGeneration()
		if err != nil { return Config{}, err }
		if err := writeConfig(cfg); err != nil { return Config{}, err }
	}
	return cfg, nil
}

func readConfig() (Config, error) {
	path := configPath()
	b, err := edit.Read(path)
	if err != nil {
		return Config{}, fmt.Errorf("dot bridge config: %w", err)
	}
	if b == nil {
		return normalizeConfig(Config{}), nil
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("dot bridge config: %w", err)
	}
	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// SaveConfig atomically writes dot-bridge.json.
func SaveConfig(cfg Config) error {
	unlock, held := filelock.Lock(configPath()+".lock", filelock.Wait)
	if !held { return errInvalidConfig }
	defer unlock()
	return saveConfig(cfg, false)
}

func saveConfig(cfg Config, replace bool) error {
	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil { return err }
	previous, err := readConfig()
	if err != nil { return err }
	if replace || cfg.PairingGeneration == "" && previous.PairingGeneration == "" || cfg.Enabled != previous.Enabled || cfg.Address != previous.Address || cfg.Credential != previous.Credential {
		cfg.PairingGeneration, err = newPairingGeneration()
		if err != nil { return err }
	} else {
		// A stale settings form cannot resurrect an older pairing generation.
		cfg.PairingGeneration = previous.PairingGeneration
	}
	if PairingIdentity(cfg) != PairingIdentity(previous) || cfg.Enabled != previous.Enabled { invalidateReady() }
	return writeConfig(cfg)
}

func writeConfig(cfg Config) error {
	if err := os.MkdirAll(settings.Dir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return edit.WriteAtomic(configPath(), append(b, '\n'))
}

// PairedConnection returns a client for enabled, paired configuration.
func PairedConnection() (*Connection, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errDisabled
	}
	reference := cfg.Credential
	if _, err := loadPairToken(reference); err != nil {
		return nil, err
	}
	c, err := NewConnection(cfg.Address, func() (string, error) {
		current, err := LoadConfig()
		if err != nil || !current.Enabled || PairingIdentity(current) != PairingIdentity(cfg) { return "", errNotPaired }
		return loadPairToken(reference)
	})
	if err != nil { return nil, err }
	c.pairingIdentity = PairingIdentity(cfg)
	return c, nil
}

// SavePairing stores the pairing token and enables the bridge configuration.
func SavePairing(cfg Config, token string) error {
	if token == "" {
		return errNotPaired
	}
	unlock, held := filelock.Lock(configPath()+".lock", filelock.Wait)
	if !held { return errInvalidConfig }
	defer unlock()
	cfg.Enabled = true
	if err := validateConfig(normalizeConfig(cfg)); err != nil { return err }
	// Persist the new generation before replacing a credential at the same ref.
	// Readers can never authenticate the replacement with the old cached proof.
	if err := saveConfig(cfg, true); err != nil { return err }
	return savePairToken(cfg.Credential, token)
}

// ClearPairing removes stored credentials without enabling the bridge.
func ClearPairing(cfg Config) error {
	unlock, held := filelock.Lock(configPath()+".lock", filelock.Wait)
	if !held { return errInvalidConfig }
	defer unlock()
	cfg.Enabled = false
	if err := saveConfig(cfg, true); err != nil { return err }
	if err := deletePairToken(cfg.Credential); err != nil && !errors.Is(err, errNotPaired) { return err }
	return nil
}

func newPairingGeneration() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { return "", err }
	return hex.EncodeToString(b[:]), nil
}

// PairingIdentity returns the public, stable namespace for a loaded pairing.
// It includes canonical loopback address, credential reference and persisted
// generation, never the token or a token hash. Empty means invalid/unmigrated;
// callers must LoadConfig before deriving scopes. Enabled is deliberately not
// included so disabled pairings' saved records remain inspectable.
func PairingIdentity(cfg Config) string {
	address, err := parseLoopbackAddress(cfg.Address)
	if err != nil || cfg.Credential == "" || cfg.PairingGeneration == "" { return "" }
	b, _ := json.Marshal([]string{address, cfg.Credential, cfg.PairingGeneration})
	sum := sha256.Sum256(b)
	return "pair-" + hex.EncodeToString(sum[:])
}

// PairingIdentity returns the namespace captured when this paired connection
// was constructed. An unbound NewConnection has no production identity.
func (c *Connection) PairingIdentity() string { return c.pairingIdentity }

func callTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := defaultCallBudget
	if deadline, ok := ctx.Deadline(); ok {
		left := time.Until(deadline)
		if left < budget {
			budget = left
		}
	}
	if budget <= 0 {
		return context.WithTimeout(ctx, time.Nanosecond)
	}
	return context.WithTimeout(ctx, budget)
}
