package dotbridge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validID(id string) bool {
	return idPattern.MatchString(id)
}

func validateSubmission(sub Submission) error {
	if !validID(sub.RequestID) || (sub.SessionID != "" && !validID(sub.SessionID)) {
		return errInvalidID
	}
	if sub.Mode != ModeAnswer && sub.Mode != ModeTask {
		return errInvalidBody
	}
	if len(sub.Messages) > maxMessages {
		return errInvalidBody
	}
	if !utf8.ValidString(sub.Task) || (sub.Mode == ModeAnswer && len(sub.Messages) == 0) || (sub.Mode == ModeTask && strings.TrimSpace(sub.Task) == "") {
		return errInvalidBody
	}
	bytes := len(sub.Task)
	for _, m := range sub.Messages {
		if !allowedRoles[m.Role] || !utf8.ValidString(m.Content) {
			return errInvalidBody
		}
		bytes += len(m.Content)
	}
	if bytes > maxMessageBytes {
		return errInvalidBody
	}
	return nil
}

func validateJobReceipt(job Job, digest, requestID, mode, sessionID string) error {
	if job.Version != 1 {
		return errReceipt
	}
	if !validID(job.JobID) || job.RequestID == "" || job.RequestDigest == "" || job.Mode == "" || job.SessionID == "" {
		return errReceipt
	}
	if job.RequestID != requestID || job.RequestDigest != digest || job.Mode != mode || job.SessionID != sessionID {
		return errReceipt
	}
	if !knownJobStates[job.State] {
		return errReceipt
	}
	if job.Result != nil {
		if !utf8.ValidString(job.Result.Text) || len(job.Result.Text) > maxResultText {
			return errReceipt
		}
		if len(job.Result.Artifacts) > maxArtifacts {
			return errReceipt
		}
	}
	return nil
}
