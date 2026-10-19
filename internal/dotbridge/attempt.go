package dotbridge

import "sync"

// Snapshot is a read-only copy of attempt state.
type Snapshot struct {
	RequestID   string
	Digest      string
	JobID       string
	State       string
	Mode        string
	SessionID   string
	BlockReplay bool
}

// Attempt tracks one Dot submission's acceptance barrier.
type Attempt struct {
	mu          sync.Mutex
	requestID   string
	digest      string
	jobID       string
	state       string
	mode        string
	sessionID   string
	blockReplay bool
}

// NewAttempt validates IDs/body and computes immutable digest.
func NewAttempt(sub Submission) (*Attempt, error) {
	if err := validateSubmission(sub); err != nil {
		return nil, err
	}
	digest, err := Digest(sub)
	if err != nil {
		return nil, err
	}
	return &Attempt{
		requestID:   sub.RequestID,
		digest:      digest,
		state:       attemptNotSubmitted,
		mode:        sub.Mode,
		sessionID:   sub.SessionID,
		blockReplay: false,
	}, nil
}

// Snapshot returns a copy under the attempt mutex.
func (a *Attempt) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshot()
}

func (a *Attempt) snapshot() Snapshot {
	return Snapshot{
		RequestID:   a.requestID,
		Digest:      a.digest,
		JobID:       a.jobID,
		State:       a.state,
		Mode:        a.mode,
		SessionID:   a.sessionID,
		BlockReplay: a.blockReplay,
	}
}

func (a *Attempt) beginSubmit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = attemptAcceptanceUnknown
	a.blockReplay = true
}

func (a *Attempt) accept(job Job) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := validateJobReceipt(job, a.digest, a.requestID, a.mode, a.sessionID); err != nil {
		return err
	}
	a.jobID = job.JobID
	a.state = attemptAccepted
	a.blockReplay = true
	return nil
}

func (a *Attempt) restore(snapshot Snapshot) error {
	if snapshot.RequestID == "" || snapshot.Digest == "" || snapshot.Mode == "" || snapshot.SessionID == "" {
		return errInvalidBody
	}
	if a.requestID != "" && (a.requestID != snapshot.RequestID || a.digest != snapshot.Digest || a.mode != snapshot.Mode || a.sessionID != snapshot.SessionID) {
		return errDigestConflict
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.requestID == "" {
		a.requestID = snapshot.RequestID
		a.digest = snapshot.Digest
		a.mode = snapshot.Mode
		a.sessionID = snapshot.SessionID
	}
	a.jobID = snapshot.JobID
	a.blockReplay = snapshot.BlockReplay
	state := snapshot.State
	if state == attemptAccepted {
		state = attemptAcceptanceUnknown
	}
	a.state = state
	return nil
}
