# Magpie dot background CLI report

Worktree: `f5ca8c/dot-automatic-routing` @ `285d6b2`

## Scope delivered

- `internal/cli/dot_cli.go` plus tests and small pairing-file helpers
- minimal dispatcher wiring in `internal/cli/main.go` and `help_cli.go`
- operator documentation in `docs/DOT_BRIDGE_SETUP.md`

No edits to `internal/dotbridge` production code, gateway routing, provider
keys, or GUI.

## Behavior

### Pairing

- `magpie dot pair <explicit pairing file>` reads DotControl's version 1
  `PairingFile` (`listen` host:port, not URL).
- Validates private regular files, strict JSON, version 1, 16 KiB cap, loopback
  listen, and authenticated status **before** keyring/config writes.
- Stores the token through existing `dotbridge.SavePairing` / OS keyring
  helpers; stdout never prints the token.
- Retains the pairing file and tells the operator to delete it manually.

### Operator commands

- `magpie dot status`
- `magpie dot off`
- `magpie dot submit --file task.json`
- `magpie dot requests`
- `magpie dot request <requestID>`
- `magpie dot job <id>`
- `magpie dot cancel <id>`

### Task gate

- `submit` requires `mode=task`.
- When `/dot-bridge/v1/status` reports `capabilities.tasks=false`, the CLI
  returns `unsupported/probe_required` and performs zero POSTs.
- When task capability is true and readiness checks pass, accepted `202`
  responses print job id/state through the existing `dotbridge.Enqueue` path.
- Uncertain outcomes surface `request_id` recovery guidance without retrying
  with a new id.
- Cancel output states intent only, not confirmed completion.

## APIs reused

Exported `internal/dotbridge` APIs only:

- config/credentials: `LoadConfig`, `SaveConfig`, `SavePairing`, `PairedConnection`
- client: `NewConnection`, `Status`, `Enqueue`, `Resume`, `Get`, `Cancel`
- journal: `Requests`, `Reserve` via `Enqueue`
- attempt/types: `Submission`, `Job`, `Status`, `NewAttempt`

No duplicate HTTP client was added in the CLI.

## Tests

`internal/cli/dot_cli_test.go` covers:

- help dispatch for `magpie dot --help`
- task capability false => zero POST
- valid task fixture => accepted `202` job output
- resume by request id without a second POST
- cancel intent wording (not confirmed)
- pairing rejects invalid JSON/listen/size before fake keyring writes and never
  prints secrets

Command:

```sh
go test -tags nogui ./internal/cli -run '^TestDotCLI' -count=1
```

Result: pass on Windows with fake keyring + temp journal state.

## Not run here

- full `go test ./...`
- real DotControl pairing or task probe against a live personal cloud
- GUI / gateway integration

## Follow-up

Runtime task probing in DotControl remains separate. The CLI now gates
submission on the actual `Status.capabilities.tasks` field so background tasks
can be enabled safely once the runtime probe flips that capability to true.

## Review corrections (I1¨CI4 and M1)

The initial report overstated Windows private-file validation: the original
Windows helper did not inspect ACLs. That implementation has been replaced.

- I1/I3: pairing and task files now share current-user ownership, one-link and
  private-access checks on the actual read handle. Windows queries owner and
  DACL with GetSecurityInfo, requires SE_DACL_PROTECTED and exclusively explicit
  current-user allow ACEs, and rejects inspection failures. NtCreateFile opens
  each component relative to the previously opened directory handle with
  no-reparse flags. The leaf denies write/delete sharing. Unix uses Openat with
  O_NOFOLLOW and Fstat ownership/mode/link checks. Checks run before and after
  reading. Deterministic ancestor substitution verifies the original file is
  read even if an ancestor rename succeeds; ordinary leaf replacement fails.
- I2: Get/Cancel replies are validated for version, exact job ID, SHA-256 digest,
  request/mode/session, legal state and result consistency/bounds. Known jobs
  use exported Resume for journal identity validation; unknown GETs must match
  the requested ID. Cancel validates the job before POST and requires the
  reply's identity to match and CancelRequested to be true. Invalid outcomes
  print no result/acknowledgement and retain GET recovery guidance.
- I4: valid-shaped pairing fixtures now prove an authenticated status request
  precedes the single fake-keyring write and reference-only config save. 401,
  unpaired/invalid status, invalid addresses, unsafe ACLs and malformed inputs
  prove zero writes and appropriate HTTP counts. Sentinel tokens never appear
  in command output/errors. Pairing file retention is tested.
- M1: canonical JSON validation rejects duplicate keys, case aliases, unknown
  keys, null/nonobject roots and trailing JSON, including nested messages.

Verification (Windows, fake keyring and isolated temporary state):

1. RED: `go test -tags nogui ./internal/cli -run '^TestDotReview' -count=1`
   produced real failures for duplicate/case JSON, empty/wrong/malformed job
   and cancel receipts, Everyone-readable ACL and unprotected DACL acceptance.
   The valid pairing/authentication fixture passed at baseline, establishing
   that later security changes still reach the status/persistence boundary.
2. GREEN: `go test -tags nogui ./internal/cli -run '^TestDot' -count=1` passed.
3. `CGO_ENABLED=1 CC=C:/msys64/mingw64/bin/gcc.exe go test -race -tags nogui
   ./internal/cli -run '^TestDot' -count=20` passed (17.610s).
4. Windows `go vet -tags nogui ./internal/cli` and Linux
   `GOOS=linux go vet -tags nogui ./internal/cli` passed.
5. gofmt and git diff --check passed.

Actual Windows ACL files cover protected current-user success, Everyone grant
rejection and inherited/unprotected DACL rejection. The wrong-owner branch is
injected after a successful real-handle security query; arbitrary owner
assignment was not attempted because it requires privileges. Security-query
failure and closed-handle checks fail closed. No live vault or cloud was used.

Threat model: this binds traversal and checks to handles against ordinary local
path substitution and excludes access grants to other users. It does not prove
isolation from malicious same-user code, administrators, kernel access or
pre-existing writable mappings. Unix owner modification during reading remains
possible. Docs now state those boundaries explicitly. Full GUI/full repository
suite and live DotControl task probing were not run; no gateway/provider or
production dotbridge files were changed.
