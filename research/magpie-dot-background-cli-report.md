# Magpie dot background CLI report

Worktree: `f5ca8c/dot-automatic-routing` @ `285d6b2`

## Rereview 1 (R1) scoped fix

Scope: tests, docs and this report only. No production code was changed, and no
broadening of I1–I3 or M1 was attempted.

The broad-acl case in `TestDotReviewPairInvalidBoundary` wrote the fixture with
0600 and skipped the platform access preparation. On Unix that file was private,
current-user owned, regular and single-linked, so the production predicate
correctly accepted it and the case failed on its own "must be rejected"
assertions after a real fake-store write and config save. The case is replaced
by platform-appropriate fixtures whose access is applied and then verified.

- `internal/cli/dot_pair_fixture_test.go` holds the shared valid-shaped fixture,
  the accepted-import proof (one exact-token store write, reference-only config,
  retained file, no token printed) and the rejected-import proof (zero vault
  writes, no config file, no output, expected status-call count, no token in
  output or error).
- `internal/cli/dot_input_unix_test.go` (`//go:build !windows`) applies a real
  `chmod` and asserts the resulting mode before the CLI runs, for 0600 (the
  accepted case) and 0640, 0644, 0666 (rejected, zero writes). Symlinked leaf,
  hard-linked leaf and symlinked ancestor are rejected through the same
  path. `TestDotOpenInputUnixRefusesLinks` covers the traversal predicate
  directly, and `TestDotCheckFileUnixOwnership` checks effective-user
  ownership, one link and a regular file on the open descriptor.
- `internal/cli/dot_input_windows_test.go` (`//go:build windows`) applies real
  security descriptors to real temporary files: protected current-user only
  (accepted), an explicit Everyone read grant, and an unprotected DACL that
  inherits from the profile directory. Both the input predicate and the full
  pairing command are asserted. `TestDotInputWindowsAccess` also covers an
  empty DACL.
- `internal/cli/dot_fixture_red_unix_test.go` is the R1 control: the same
  fixture at 0600 imports and writes once, while the 0600→0644 variant is
  rejected with zero writes, so the difference is the fixture's mode rather
  than any change to the predicate.

Verification, Windows 10 host, `f5ca8c` at this revision:

1. RED for the new boundary: before the platform fixtures, the broad-acl branch
   reached the success path. The control above states the mode dependency
   explicitly rather than relying on a cross-platform run I could not execute.
2. `go test -tags nogui ./internal/cli -run '^TestDot' -count=1` — ok
   (3.460s).
3. `CGO_ENABLED=1 CC=C:/msys64/mingw64/bin/gcc.exe go test -race -tags nogui
   ./internal/cli -run '^TestDot' -count=1` — ok (10.443s), and
   `-count=20` — ok (19.579s).
4. `go vet -tags nogui ./internal/cli` on Windows — ok.
5. `GOOS=linux go vet -tags nogui ./internal/cli` and
   `GOOS=darwin go vet -tags nogui ./internal/cli` — exit 0.

Cross-platform execution limits, stated plainly: no Linux or macOS runtime was
available on this machine. WSL2 has no distro installed, Docker is unavailable,
and no Linux Go toolchain exists. The Linux and Darwin runs are cross-compiled
vet only: they prove the `!windows` test file and the `unix` syscalls compile
there, not that the Unix fixtures pass. The Unix mode, symlink and hardlink
fixtures were therefore not executed. Every runtime claim above is Windows.
The `!windows` build tag is broader than Unix, so no all-GOOS claim is made.

Nothing outside `internal/cli` and `research/` was touched, and the other
session's `internal/gateway` and `internal/dotbridge` working changes were left
alone. Full repository suite, GUI suite, live DotControl pairing and the task
probe remain unrun.

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

## Review corrections (I1�CI4 and M1)

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
