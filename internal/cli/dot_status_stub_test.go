package cli

import (
	"encoding/json"
	"net/http"

	"github.com/yetone/magpie/internal/dotbridge"
)

// dotStatusPaired is the reply a valid pairing import is authenticated against.
func dotStatusPaired(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Paired: true, Ready: true})
}

func dotStatusUnpaired(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Paired: false})
}

func dotStatusBadVersion(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(dotbridge.Status{Version: 2, Paired: true})
}
