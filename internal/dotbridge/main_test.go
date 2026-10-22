package dotbridge

import (
	"testing"

	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) {
	credentialStore = newMemStore()
	testenv.Main(m)
}
