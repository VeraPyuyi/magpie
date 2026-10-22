package gateway

import (
	"context"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

// DotJobObservation is compact execution evidence from a validated receipt.
// Values are replaced, never mutated, so retained trace frames stay historical.
type DotJobObservation struct {
	RequestID       string    `json:"request_id"`
	JobID           string    `json:"job_id"`
	Mode            string    `json:"mode"`
	State           string    `json:"state"`
	Summary         string    `json:"summary,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	ObservedAt      time.Time `json:"observed_at"`
	ResultAvailable bool      `json:"result_available"`
}

type dotObserverKey struct{}

func publishDot(ctx context.Context, job dotbridge.Job) {
	if publish, ok := ctx.Value(dotObserverKey{}).(func(dotbridge.Job)); ok {
		publish(job)
	}
}

func dotObservationError(job dotbridge.Job) string {
	if job.ErrorCode != "" || job.Result != nil && job.Result.ErrorCode != "" {
		return "dot_job_error"
	}
	return ""
}

func compactDotJob(job dotbridge.Job) *DotJobObservation {
	return &DotJobObservation{
		RequestID:       job.RequestID,
		JobID:           job.JobID,
		Mode:            job.Mode,
		State:           job.State,
		Summary:         job.Summary,
		ErrorCode:       dotObservationError(job),
		UpdatedAt:       job.UpdatedAt,
		ObservedAt:      time.Now().UTC(),
		ResultAvailable: job.State == dotbridge.StateCompleted && job.Result != nil && dotbridge.ValidateResult(job.Result) == nil,
	}
}
