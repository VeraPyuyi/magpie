package dotbridge_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/backup"
	"github.com/yetone/magpie/internal/settings"
)

func TestDotBackupExcludesMachineLocalState(t *testing.T) {
	const sentinel = "DOT_MACHINE_LOCAL_DO_NOT_SYNC_7125"
	if err := os.MkdirAll(settings.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dot-bridge.json", "dot-bridge-requests.json"} {
		path := filepath.Join(settings.Dir(), name)
		old, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if old == nil {
				_ = os.Remove(path)
			} else {
				_ = os.WriteFile(path, old, 0600)
			}
		})
		if err := os.WriteFile(path, []byte(`{"credential":"`+sentinel+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, keys := range []bool{false, true} {
		bundle, err := backup.Collect(keys, "dot-client-test")
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{sentinel, "dot-bridge.json", "dot-bridge-requests.json"} {
			if strings.Contains(string(data), secret) {
				t.Fatalf("machine-local state carried with keys=%v", keys)
			}
		}
	}
}
