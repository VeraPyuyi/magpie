package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

const dotUsage = `magpie dot pair <pairing-file>   import DotControl's private version 1 file into the OS keyring
magpie dot status                show authenticated bridge readiness and capabilities
magpie dot off                   disable locally (keeps pairing and pending jobs)
magpie dot submit --file task.json   enqueue a task once, only with verified task capability
magpie dot requests              list this CLI's local request journal
magpie dot request <requestID>   recover a journaled request with GET, without resubmission
magpie dot job <id>              fetch job state and results
magpie dot cancel <id>           request cancellation; acknowledgement is intent only

Task files contain request_id (stable unique ID), mode="task", task, optional
session_id and messages [{"role":"user","content":"explicit context"}].
Keep the same request_id and body for recovery. No automatic history is attached.
Task capability false means unsupported/probe_required: run DotControl's task
probe and inspect status before submitting. Text readiness alone is insufficient.
Pairing files are retained: delete the explicit file manually after success.
Neither tokens nor pairing JSON belong in shell arguments. See docs/DOT_BRIDGE_SETUP.md.
`

var dotID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// dotPairingFile mirrors DotControl's version 1 PairingFile wire format.
type dotPairingFile struct {
	Version   int       `json:"version"`
	ClientID  string    `json:"client_id"`
	QueueID   string    `json:"queue_id"`
	Token     string    `json:"token"`
	Listen    string    `json:"listen"`
	CreatedAt time.Time `json:"created_at"`
}

func dotDecode(b []byte, dst any) error {
	if err := dotCanonicalJSON(b, reflect.TypeOf(dst).Elem()); err != nil {
		return fmt.Errorf("dot: invalid or ambiguous JSON file")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("dot: invalid JSON file")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("dot: expected one JSON object")
	}
	return nil
}

func dotRead(path string, private bool, max int64) ([]byte, error) {
	f, closeInput, err := dotOpenInput(path)
	if err != nil {
		return nil, fmt.Errorf("dot: cannot securely open input file")
	}
	defer closeInput()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("dot: input must be a regular file")
	}
	if err := dotCheckFile(f, st, private); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, fmt.Errorf("dot: input exceeds %d bytes or cannot be read", max)
	}
	if err := dotCheckFile(f, st, private); err != nil {
		return nil, err
	}
	return b, nil
}
func dotScope(cfg dotbridge.Config) string { return "cli:" + dotbridge.PairingIdentity(cfg) }

func dotTo(ctx context.Context, out io.Writer, args []string) error {
	if len(args) == 0 {
		_, err := io.WriteString(out, dotUsage)
		return err
	}
	if args[0] == "pair" {
		if len(args) != 2 {
			return fmt.Errorf("usage: magpie dot pair <pairing-file>")
		}
		return dotPair(ctx, out, args[1])
	}
	cfg, err := dotbridge.LoadConfig()
	if err != nil {
		return fmt.Errorf("dot: cannot load configuration")
	}
	scope := dotScope(cfg)
	if len(args) == 1 && args[0] == "off" {
		cfg.Enabled = false
		if err := dotbridge.SaveConfig(cfg); err != nil {
			return fmt.Errorf("dot: cannot disable bridge")
		}
		_, err := fmt.Fprintln(out, "Dot bridge disabled locally; existing jobs and pairing retained.")
		return err
	}
	if len(args) == 1 && args[0] == "requests" {
		items, err := dotbridge.Requests(scope)
		if err != nil {
			return fmt.Errorf("dot: cannot read request journal")
		}
		return json.NewEncoder(out).Encode(items)
	}
	if len(args) == 1 && args[0] == "status" && !cfg.Enabled {
		_, err := fmt.Fprintln(out, `{"enabled":false,"code":"disabled"}`)
		return err
	}
	valid := len(args) == 1 && args[0] == "status" || len(args) == 2 && (args[0] == "request" || args[0] == "job" || args[0] == "cancel") || len(args) == 3 && args[0] == "submit" && args[1] == "--file"
	if !valid {
		return fmt.Errorf("usage:\n%s", dotUsage)
	}
	if len(args) == 2 && !dotID.MatchString(args[1]) {
		return fmt.Errorf("dot: invalid identifier")
	}
	c, err := dotbridge.PairedConnection()
	if err != nil {
		return fmt.Errorf("dot: paired connection unavailable; inspect configuration and OS keyring")
	}
	switch args[0] {
	case "status":
		st, err := c.Status(ctx)
		if err != nil {
			return fmt.Errorf("dot: status unavailable")
		}
		return json.NewEncoder(out).Encode(st)
	case "submit":
		b, err := dotRead(args[2], false, 256*1024)
		if err != nil {
			return err
		}
		var sub dotbridge.Submission
		if err := dotDecode(b, &sub); err != nil {
			return err
		}
		if sub.Mode != dotbridge.ModeTask {
			return fmt.Errorf("dot: submit requires mode task; answer mode is separate from background tasks")
		}
		attempt, err := dotbridge.NewAttempt(sub)
		if err != nil {
			return fmt.Errorf("dot: invalid task submission")
		}
		st, err := c.Status(ctx)
		if err != nil {
			return fmt.Errorf("dot: status unavailable; no task submitted")
		}
		if !st.Capabilities.Tasks {
			return fmt.Errorf("dot: tasks unsupported/probe_required; run DotControl's task probe; no task submitted")
		}
		if !dotTaskReady(st, time.Now()) {
			return fmt.Errorf("dot: task bridge not ready; no task submitted")
		}
		job, err := c.Enqueue(ctx, sub, scope, attempt)
		if err != nil {
			if attempt.Snapshot().BlockReplay {
				return fmt.Errorf("dot: outcome_unknown requestID=%s; recover with: magpie dot request %s (do not retry with a new ID)", sub.RequestID, sub.RequestID)
			}
			return fmt.Errorf("dot: task rejected before submission; check request journal and input")
		}
		return json.NewEncoder(out).Encode(job)
	case "request":
		job, _, err := c.Resume(ctx, scope, args[1])
		if err != nil {
			return fmt.Errorf("dot: request unresolved; retain requestID=%s; recover with: magpie dot request %s; no POST sent", args[1], args[1])
		}
		return json.NewEncoder(out).Encode(job)
	case "job":
		job, err := dotKnownJob(ctx, c, scope, args[1])
		if err != nil {
			return fmt.Errorf("dot: job lookup failed")
		}
		return json.NewEncoder(out).Encode(job)
	case "cancel":
		before, err := dotKnownJob(ctx, c, scope, args[1])
		if err != nil {
			return fmt.Errorf("dot: cannot validate job before cancellation; inspect with: magpie dot job %s", args[1])
		}
		job, err := c.Cancel(ctx, args[1])
		if err != nil || dotValidateJob(job, args[1]) != nil || !dotSameJob(before, job) || !job.CancelRequested {
			return fmt.Errorf("dot: cancellation outcome unknown; inspect with: magpie dot job %s", args[1])
		}
		return json.NewEncoder(out).Encode(struct {
			Notice string        `json:"notice"`
			Job    dotbridge.Job `json:"job"`
		}{"Cancellation intent acknowledged; inspect job state for completion.", job})
	}
	return nil
}

func dotTaskReady(st dotbridge.Status, now time.Time) bool {
	return st.Version == 1 && st.Enabled && st.Paired && st.Ready && st.Capabilities.Tasks && st.Subscription.State == "active" && !st.VerifiedAt.IsZero() && !st.VerifiedAt.After(now) && now.Before(st.VerifiedAt.Add(10*time.Minute)) && now.Before(st.ExpiresAt) && now.Before(st.Subscription.ExpiresAt)
}

func dotPair(ctx context.Context, out io.Writer, path string) error {
	b, err := dotRead(path, true, 16*1024)
	if err != nil {
		return err
	}
	var file dotPairingFile
	if err := dotDecode(b, &file); err != nil {
		return err
	}
	if file.Version != 1 || !dotID.MatchString(file.ClientID) || !dotID.MatchString(file.QueueID) || strings.TrimSpace(file.Token) == "" || strings.ContainsAny(file.Token, "\r\n\x00") || file.CreatedAt.IsZero() {
		return fmt.Errorf("dot: invalid version 1 pairing file")
	}
	c, err := dotbridge.NewConnection(file.Listen, func() (string, error) { return file.Token, nil })
	if err != nil {
		return fmt.Errorf("dot: pairing listen must be numeric loopback host:port")
	}
	st, err := c.Status(ctx)
	if err != nil || st.Version != 1 || !st.Paired {
		return fmt.Errorf("dot: pairing status validation failed; credentials not stored")
	}
	cfg, err := dotbridge.LoadConfig()
	if err != nil {
		return fmt.Errorf("dot: cannot load configuration; credentials not stored")
	}
	cfg.Address = file.Listen
	cfg.Credential = "dot-cli:" + file.ClientID
	cfg.Enabled = true
	// NewConnection has validated the address; LoadConfig has validated the wait budget.
	if err := dotbridge.SavePairing(cfg, file.Token); err != nil {
		return fmt.Errorf("dot: could not persist pairing in OS keyring/configuration")
	}
	_, err = fmt.Fprintln(out, "Dot bridge paired. Pairing file retained: delete it manually after verifying magpie dot status.")
	return err
}
