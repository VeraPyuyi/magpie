package dotbridge

import (
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

const (
	maxArtifactLabel = 256
	maxArtifactURL   = 2048
)

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
	if !validID(job.JobID) || job.RequestID == "" || job.RequestDigest == "" || job.Mode == "" {
		return errReceipt
	}
	// DotControl echoes optional session_id verbatim, including the empty string.
	// Compare it to the Attempt identity; emptiness alone is not a bad receipt.
	if job.RequestID != requestID || job.RequestDigest != digest || job.Mode != mode || job.SessionID != sessionID {
		return errReceipt
	}
	if !utf8.ValidString(job.Summary) || len(job.Summary) > 2048 {
		return errReceipt
	}
	if !knownJobStates[job.State] {
		return errReceipt
	}
	if job.Result != nil {
		if !utf8.ValidString(job.Result.Text) || len(job.Result.Text) > maxResultText {
			return errReceipt
		}
		if err := ValidateResult(job.Result); err != nil {
			return errReceipt
		}
	}
	return nil
}

func ValidateResult(res *Result) error {
	if res == nil {
		return errReceipt
	}
	if !utf8.ValidString(res.Text) || len(res.Text) > maxResultText {
		return errReceipt
	}
	if res.ErrorCode != "" && !validID(res.ErrorCode) || res.Cancelled && res.ErrorCode != "" {
		return errReceipt
	}
	return validateArtifacts(res.Text+res.ErrorCode, res.Artifacts)
}

func validateArtifacts(text string, artifacts []Artifact) error {
	if len(artifacts) > maxArtifacts {
		return errReceipt
	}
	bytes := len(text)
	for _, a := range artifacts {
		if !utf8.ValidString(a.Label) || len(a.Label) > maxArtifactLabel || len(a.URL) > maxArtifactURL {
			return errReceipt
		}
		bytes += len(a.Label) + len(a.URL)
		if validateArtifact(a) != nil {
			return errReceipt
		}
	}
	if bytes > maxResultText {
		return errReceipt
	}
	return nil
}

var hostPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)

// endsInIPv4Number implements the numeric final-label distinction used by
// WHATWG hosts without parsing into a bounded integer (overflow must not turn
// an alias into a DNS name). The caller already removed one trailing dot and
// lowercased the host. All-decimal labels include octal-looking/invalid octal
// forms; a 0x prefix with only hex digits includes the empty-digit zero form.
func endsInIPv4Number(host string) bool {
	last := host[strings.LastIndexByte(host, '.')+1:]
	if last == "" {
		return false
	}
	digits, alphabet := last, "0123456789"
	if strings.HasPrefix(last, "0x") {
		digits, alphabet = last[2:], "0123456789abcdef"
	}
	for _, c := range digits {
		if !strings.ContainsRune(alphabet, c) {
			return false
		}
	}
	return true
}

func validateArtifact(a Artifact) error {
	if len(a.Label) > maxArtifactLabel || !utf8.ValidString(a.Label) || len(a.URL) > maxArtifactURL || !utf8.ValidString(a.URL) || strings.Contains(a.URL, "\\") {
		return errReceipt
	}
	u, e := url.Parse(a.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return errReceipt
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errReceipt
		}
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, e := netip.ParseAddr(h); e == nil {
		// Reject mapped and scoped forms outright rather than letting consumers
		// interpret an IPv4 destination through an IPv6 spelling.
		if ip.Is4In6() || ip.Zone() != "" {
			return errReceipt
		}
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.Zone() != "" || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
			return errReceipt
		}
	} else {
		// WHATWG treats a domain ending in a number as IPv4 input, including
		// hex, octal, shortened and integer spellings. Only canonical IPs may
		// reach the address-policy branch above; never accept aliases as DNS.
		if endsInIPv4Number(h) {
			return errReceipt
		}
		if !hostPattern.MatchString(h) || !strings.Contains(h, ".") || strings.HasSuffix(h, ".localhost") || h == "localhost" || strings.HasSuffix(h, ".local") || strings.Contains(h, "..") {
			return errReceipt
		}
		for _, label := range strings.Split(h, ".") {
			if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return errReceipt
			}
		}
	}
	return nil
}
