package dotbridge

import "time"

const (
	ModeAnswer = "answer"
	ModeTask   = "task"

	StateQueued          = "queued"
	StateClaimed         = "claimed"
	StateRunning         = "running"
	StateWaitingUser     = "waiting_user"
	StateCancelRequested = "cancel_requested"
	StateCancelled       = "cancelled"
	StateCompleted       = "completed"
	StateFailed          = "failed"
	StateOutcomeUnknown  = "outcome_unknown"

	attemptNotSubmitted      = "not_submitted"
	attemptAcceptanceUnknown = "acceptance_unknown"
	attemptAccepted          = "accepted"

	maxMessages       = 32
	maxMessageBytes   = 256 * 1024
	maxResultText     = 256 * 1024
	maxArtifacts      = 16
	maxJournalRecords = 10000
)

var knownJobStates = map[string]bool{
	StateQueued:          true,
	StateClaimed:         true,
	StateRunning:         true,
	StateWaitingUser:     true,
	StateCancelRequested: true,
	StateCancelled:       true,
	StateCompleted:       true,
	StateFailed:          true,
	StateOutcomeUnknown:  true,
}

var allowedRoles = map[string]bool{
	"system":    true,
	"user":      true,
	"assistant": true,
}

// Message is one chat turn on the Dot bridge wire.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Submission is the body Magpie posts to DotControl.
type Submission struct {
	RequestID string    `json:"request_id"`
	Mode      string    `json:"mode"`
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
	Task      string    `json:"task"`
}

// Artifact is a structured result attachment.
type Artifact struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

// Result is the terminal payload for a job.
type Result struct {
	Text      string     `json:"text"`
	Artifacts []Artifact `json:"artifacts"`
	ErrorCode string     `json:"error_code"`
	Cancelled bool       `json:"cancelled"`
}

// Job is the bridge job DTO.
type Job struct {
	Version         int       `json:"version"`
	JobID           string    `json:"job_id"`
	RequestID       string    `json:"request_id"`
	RequestDigest   string    `json:"request_digest"`
	Mode            string    `json:"mode"`
	SessionID       string    `json:"session_id"`
	State           string    `json:"state"`
	Summary         string    `json:"summary,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CancelRequested bool      `json:"cancel_requested"`
	Result          *Result   `json:"result,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
}

// Capacity reports global retained job counts for operator visibility.
type Capacity struct {
	Limit     int `json:"limit"`
	Retained  int `json:"retained"`
	Remaining int `json:"remaining"`
}

// Status is the authenticated /dot-bridge/v1/status payload.
type Status struct {
	Version      int                `json:"version"`
	Enabled      bool               `json:"enabled"`
	Paired       bool               `json:"paired"`
	Ready        bool               `json:"ready"`
	Code         string             `json:"code"`
	VerifiedAt   time.Time          `json:"verified_at,omitempty"`
	ExpiresAt    time.Time          `json:"expires_at,omitempty"`
	Subscription SubscriptionStatus `json:"subscription"`
	Capabilities Capabilities       `json:"capabilities"`
	Capacity     *Capacity          `json:"capacity,omitempty"`
}

// SubscriptionStatus describes the current authenticated subscription.
type SubscriptionStatus struct {
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// Capabilities describes abilities verified by the bridge policy.
type Capabilities struct {
	Text  bool `json:"text"`
	Tasks bool `json:"tasks"`
}
