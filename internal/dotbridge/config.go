package dotbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/edit"
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
	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return err
	}
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
	return NewConnection(cfg.Address, func() (string, error) {
		return loadPairToken(reference)
	})
}

// SavePairing stores the pairing token and enables the bridge configuration.
func SavePairing(cfg Config, token string) error {
	if token == "" {
		return errNotPaired
	}
	if err := savePairToken(cfg.Credential, token); err != nil {
		return err
	}
	cfg.Enabled = true
	return SaveConfig(cfg)
}

// ClearPairing removes stored credentials without enabling the bridge.
func ClearPairing(cfg Config) error {
	if err := deletePairToken(cfg.Credential); err != nil && !errors.Is(err, errNotPaired) {
		return err
	}
	cfg.Enabled = false
	return SaveConfig(cfg)
}

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
