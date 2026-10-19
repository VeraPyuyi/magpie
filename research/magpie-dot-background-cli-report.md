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
