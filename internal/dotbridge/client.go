package dotbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Wire budgets include JSON escaping and metadata; raw content limits remain
// separate. Read one extra response byte to reject oversize bodies, not truncate.
const (
	maxWireRequestBytes  = 2 << 20
	maxWireResponseBytes = 2 << 20
)

// Connection talks to the local Dot bridge API.
type Connection struct {
	baseURL           string
	pairingIdentity   string
	readinessRevision string
	token             func() (string, error)
	client            *http.Client
}

// NewConnection validates a numeric loopback address and builds a client.
func NewConnection(address string, token func() (string, error)) (*Connection, error) {
	base, err := parseLoopbackAddress(address)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, errAuth
	}
	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errRedirect
		},
	}
	return &Connection{baseURL: base, token: token, client: client}, nil
}

func parseLoopbackAddress(address string) (string, error) {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return "", errAddress
	}
	if strings.Contains(addr, "://") || strings.ContainsAny(addr, "/?#@") {
		return "", errAddress
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", errAddress
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return "", errAddress
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errAddress
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return "", errAddress
	}
	if ip.Is6() {
		return fmt.Sprintf("http://[%s]:%d", ip.String(), n), nil
	}
	return fmt.Sprintf("http://%s:%d", ip.String(), n), nil
}

func (c *Connection) authHeader() (string, error) {
	tok, err := c.token()
	if err != nil {
		return "", sanitizeTokenErr(err)
	}
	if tok == "" {
		return "", errAuth
	}
	return "Bearer " + tok, nil
}

func (c *Connection) do(ctx context.Context, method, path string, body []byte) (*http.Response, []byte, error) {
	ctx, cancel := callTimeout(ctx)
	defer cancel()
	auth, err := c.authHeader()
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxWireResponseBytes+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return resp, nil, err
	}
	if len(b) > maxWireResponseBytes {
		return resp, nil, errResponseSize
	}
	if _, err := c.authHeader(); err != nil {
		return resp, nil, err
	}
	return resp, b, nil
}

type submitEnvelope struct {
	Job
	Accepted bool `json:"accepted"`
}

// encodeSubmission checks the full encoded envelope before reserving identity or
// setting the acceptance barrier. Valid content may expand sixfold in JSON.
func encodeSubmission(sub Submission) ([]byte, error) {
	if err := validateSubmission(sub); err != nil {
		return nil, err
	}
	if sub.Messages == nil {
		sub.Messages = []Message{}
	}
	b, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}
	if len(b) > maxWireRequestBytes {
		return nil, errInvalidBody
	}
	return b, nil
}

// Submit performs the single POST for a new job attempt.
func (c *Connection) Submit(ctx context.Context, sub Submission, attempt *Attempt) (Job, error) {
	if attempt == nil {
		return Job{}, errInvalidBody
	}
	b, err := encodeSubmission(sub)
	if err != nil {
		return Job{}, err
	}
	digest, err := Digest(sub)
	if err != nil {
		return Job{}, err
	}
	if snap := attempt.Snapshot(); snap.Digest != digest || snap.RequestID != sub.RequestID {
		return Job{}, errDigestConflict
	}
	attempt.beginSubmit()
	resp, body, err := c.do(ctx, http.MethodPost, "/dot-bridge/v1/jobs", b)
	if err != nil {
		return Job{}, err
	}
	if resp.StatusCode != http.StatusAccepted {
		return Job{}, fmt.Errorf("dot_bridge_submit_status:%d", resp.StatusCode)
	}
	var env submitEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Job{}, errReceipt
	}
	if !env.Accepted {
		return Job{}, errReceipt
	}
	job := env.Job
	if err := attempt.accept(job); err != nil {
		return Job{}, err
	}
	return job, nil
}

// ByRequest looks up a job by request ID after uncertain acceptance.
func (c *Connection) ByRequest(ctx context.Context, attempt *Attempt) (Job, error) {
	if attempt == nil {
		return Job{}, errInvalidBody
	}
	snap := attempt.Snapshot()
	if snap.RequestID == "" {
		return Job{}, errInvalidBody
	}
	resp, body, err := c.do(ctx, http.MethodGet, "/dot-bridge/v1/requests/"+snap.RequestID, nil)
	if err != nil {
		return Job{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return Job{}, fmt.Errorf("dot_bridge_not_found")
	}
	if resp.StatusCode != http.StatusOK {
		return Job{}, fmt.Errorf("dot_bridge_lookup_status:%d", resp.StatusCode)
	}
	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return Job{}, errReceipt
	}
	if err := attempt.accept(job); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Get returns one job by ID.
func (c *Connection) Get(ctx context.Context, jobID string) (Job, error) {
	if !validID(jobID) {
		return Job{}, errInvalidID
	}
	resp, body, err := c.do(ctx, http.MethodGet, "/dot-bridge/v1/jobs/"+jobID, nil)
	if err != nil {
		return Job{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Job{}, fmt.Errorf("dot_bridge_get_status:%d", resp.StatusCode)
	}
	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return Job{}, errReceipt
	}
	return job, nil
}

// Cancel requests cancellation for a job.
func (c *Connection) Cancel(ctx context.Context, jobID string) (Job, error) {
	if !validID(jobID) {
		return Job{}, errInvalidID
	}
	resp, body, err := c.do(ctx, http.MethodPost, "/dot-bridge/v1/jobs/"+jobID+"/cancel", nil)
	if err != nil {
		return Job{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Job{}, fmt.Errorf("dot_bridge_cancel_status:%d", resp.StatusCode)
	}
	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return Job{}, errReceipt
	}
	return job, nil
}

// Enqueue reserves identity and either looks up or submits once.
func (c *Connection) Enqueue(ctx context.Context, sub Submission, scope string, attempt *Attempt) (Job, error) {
	if attempt == nil {
		return Job{}, errInvalidBody
	}
	if _, err := encodeSubmission(sub); err != nil {
		return Job{}, err
	}
	digest, err := Digest(sub)
	if err != nil {
		return Job{}, err
	}
	if snap := attempt.Snapshot(); snap.Digest != "" && (snap.Digest != digest || snap.RequestID != sub.RequestID) {
		return Job{}, errDigestConflict
	}
	// Reject a switched connection or foreign scope before any durable reserve.
	if err := c.checkPairingScope(scope); err != nil {
		return Job{}, err
	}
	snapshot, existing, err := Reserve(sub, scope)
	if err != nil {
		return Job{}, err
	}
	if err := attempt.restore(snapshot); err != nil {
		return Job{}, err
	}
	if existing {
		job, err := c.ByRequest(ctx, attempt)
		if err != nil {
			return Job{}, err
		}
		_ = SaveAttempt(scope, attempt.Snapshot())
		return job, nil
	}
	job, err := c.Submit(ctx, sub, attempt)
	if err != nil {
		return Job{}, err
	}
	_ = SaveAttempt(scope, attempt.Snapshot())
	return job, nil
}

// Resume reloads journal identity and looks up without POST.
func (c *Connection) Resume(ctx context.Context, scope, requestID string) (Job, *Attempt, error) {
	if err := c.checkPairingScope(scope); err != nil {
		return Job{}, nil, err
	}
	snapshot, err := lookupRequest(scope, requestID)
	if err != nil {
		return Job{}, nil, err
	}
	attempt := &Attempt{}
	if err := attempt.restore(snapshot); err != nil {
		return Job{}, nil, err
	}
	job, err := c.ByRequest(ctx, attempt)
	if err != nil {
		return Job{}, attempt, err
	}
	_ = SaveAttempt(scope, attempt.Snapshot())
	return job, attempt, nil
}

// checkPairingScope keeps all bound clients on their captured namespace.
// Legacy/unbound transport fixtures retain arbitrary scopes; production clients
// are always constructed by BoundPairingSnapshot/PairedConnection.
func (c *Connection) checkPairingScope(scope string) error {
	if c.pairingIdentity == "" {
		return nil
	}
	if scope != "cli:"+c.pairingIdentity && scope != "gui:"+c.pairingIdentity && !strings.HasPrefix(scope, c.pairingIdentity+":") {
		return errNotPaired
	}
	cfg, err := LoadConfig()
	if err != nil || !cfg.Enabled || PairingIdentity(cfg) != c.pairingIdentity {
		return errNotPaired
	}
	return nil
}

// Status fetches bridge readiness from the local API.
func (c *Connection) Status(ctx context.Context) (Status, error) {
	var generation uint64
	if c.pairingIdentity != "" {
		readyCache.Lock()
		readyCache.status = Status{}
		readyCache.at = time.Time{}
		readyCache.generation++
		generation = readyCache.generation
		readyCache.Unlock()
	}
	resp, body, err := c.do(ctx, http.MethodGet, "/dot-bridge/v1/status", nil)
	if err != nil {
		return Status{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("dot_bridge_status:%d", resp.StatusCode)
	}
	var st Status
	if err := json.Unmarshal(body, &st); err != nil {
		return Status{}, errReceipt
	}
	if st.Version != 1 {
		return Status{}, errReceipt
	}
	cfg, cfgErr := LoadConfig()
	readyCache.Lock()
	if c.pairingIdentity != "" && cfgErr == nil && cfg.Enabled && PairingIdentity(cfg) == c.pairingIdentity && cfg.ReadinessRevision == c.readinessRevision && readyCache.generation == generation {
		readyCache.identity = c.pairingIdentity
		readyCache.revision = c.readinessRevision
		readyCache.status = st
		readyCache.at = time.Now()
	}
	readyCache.Unlock()
	return st, nil
}

// CachedReady reports whether a recently validated status cache is ready.
var readyCache struct {
	sync.Mutex
	identity   string
	revision   string
	status     Status
	at         time.Time
	generation uint64
}

func invalidateReady() {
	readyCache.Lock()
	defer readyCache.Unlock()
	readyCache.identity = ""
	readyCache.status = Status{}
	readyCache.at = time.Time{}
	readyCache.generation++
}

func CachedReady(now time.Time) bool {
	cfg, err := LoadConfig()
	if err != nil || !cfg.Enabled {
		return false
	}
	identity := PairingIdentity(cfg)
	readyCache.Lock()
	defer readyCache.Unlock()
	if identity == "" || identity != readyCache.identity || cfg.ReadinessRevision != readyCache.revision {
		return false
	}
	s := readyCache.status
	age := now.Sub(readyCache.at)
	// "active" is a conservative provisional spelling until Task 2 pins the
	// status endpoint's subscription states. Any other spelling fails closed.
	return age >= 0 && age <= 10*time.Second && s.Version == 1 && s.Enabled && s.Paired && s.Ready && s.Capabilities.Text && s.Subscription.State == "active" && !s.VerifiedAt.IsZero() && !s.VerifiedAt.After(now) && now.Before(s.VerifiedAt.Add(10*time.Minute)) && now.Before(s.ExpiresAt) && now.Before(s.Subscription.ExpiresAt)
}
