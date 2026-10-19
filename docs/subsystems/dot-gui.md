# Dot background task window

The optional `plugins/dot` built-in requires the existing `window` service and
registers a `nav.page` contribution. Its scripts use the existing kernel page
slot. Its registration imports only `internal/pluginrt`; turning the page off
does not disable the bridge or cancel cloud work.

`internal/gui/dot.go` registers `/api/dot/` under the existing `Handler`.
Browser mode retains the web session guard. Every Dot route additionally
requires a loopback peer and Host, rejects forwarded headers and bearer
credentials, and validates any Origin against the request's scheme and Host.
The native window is recognized only by `ownWindow`'s context marker, supporting
its Wails origin. A network caller cannot set that marker. Gateway caller keys
never authorize this surface. No route accepts pairing credentials, file paths,
addresses, or configuration from a browser.

Pairing remains the secure terminal boundary: explicitly import DotControl's
private local pairing file using `magpie dot pair <private-pairing-file>`, verify
`magpie dot status`, then refresh this page. The file is retained under the CLI's
existing policy. The GUI does not upload pairing files or return credentials.

- `GET status` reports authenticated bridge status and `task_available`, computed
  from actual task capability, readiness, verification freshness, pairing and
  subscription expiration. Text readiness cannot enable tasks.
- `POST off` disables locally, retaining pairing and running work.
- `POST jobs` accepts a task submission with explicit context messages and stable
  request/session IDs. It checks fresh task capability before `Enqueue` and
  returns 202 for acceptance, without waiting for execution.
- `GET requests` returns at most the latest 100 identity snapshots in this GUI's
  journal scope, distinct from CLI and gateway scopes. It contains no saved task
  or conversation text and provides no selected-history integration.
- `GET requests/{request_id}` uses `Resume` to validate immutable journal identity
  and query the original request without POST. Unknown acceptance never causes
  automatic resubmission. The browser retains pending identity across reloads.
- `POST requests/{request_id}/cancel` first resumes that GUI-owned request, then
  checks the cancel receipt against the recovered identity. Its acknowledgement
  is cancellation intent, never completion or a task result.

The page renders result text literally with `textContent`; artifact links require
HTTPS without user info and are never fetched automatically. State, placeholders
and operator instructions are translated in English, Chinese, Traditional Chinese,
Japanese and German. There is no production ready setter.

Focused checks use the existing isolated runners and fixture-only paired bridge:

```text
go test -tags nogui ./internal/gui ./plugins/dot ./internal/pluginrt -run '^(TestDotGUI|TestBuiltin|TestPluginsImportOnlyCore)' -count=1
go test -race -tags nogui ./internal/gui -run '^TestDotGUI' -count=1
go vet -tags nogui ./internal/gui ./plugins/dot
node --test --test-concurrency=1 internal/gui/tests/dot-background.test.cjs internal/gui/tests/i18n-keys.test.cjs internal/gui/tests/plugin-kernel.test.cjs
```

The browser fixture runs Chromium and WebKit, all five languages, and narrow and
wide windows. It covers task gating, explicit context, reload recovery, literal
malicious result text, safe artifact links and cancellation intent. No live
account, personal configuration or OS credential store is used.
