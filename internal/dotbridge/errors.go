package dotbridge

import "errors"

var (
	errDisabled       = errors.New("dot_bridge_disabled")
	errNotPaired      = errors.New("dot_bridge_not_paired")
	errInvalidConfig  = errors.New("dot_bridge_invalid_config")
	errInvalidID      = errors.New("dot_bridge_invalid_id")
	errInvalidBody    = errors.New("dot_bridge_invalid_body")
	errDigestConflict = errors.New("dot_bridge_digest_conflict")
	errJournal        = errors.New("dot_bridge_journal")
	errAddress        = errors.New("dot_bridge_address")
	errAuth           = errors.New("dot_bridge_auth")
	errReceipt        = errors.New("dot_bridge_receipt")
	errRedirect       = errors.New("dot_bridge_redirect")
	errResponseSize   = errors.New("dot_bridge_response_too_large")
)

func sanitizeTokenErr(err error) error {
	if err == nil {
		return nil
	}
	return errAuth
}
