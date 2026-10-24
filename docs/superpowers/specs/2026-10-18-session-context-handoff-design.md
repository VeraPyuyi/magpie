# Session context handoff

The first phase helps a reader select existing sessions in Magpie, preview bounded excerpts, and copy them into an official Dot conversation with a new task. It does not connect remote computers or submit tasks to Dot automatically.

## User flow

Sessions are grouped by proven provenance and working folder. A separate context selection supports at most 20 readable sessions without changing the existing delete selection. Preparing context reads the selected conversations and presents literal excerpts, per-session errors, and omission notices. Copy requires an explicit action after preview. Nothing is written back to native session files.

## Provenance

Local, WSL and gateway records describe how Magpie obtained a session. They do not identify the computer that created it. A gateway record is not evidence of a Codex cloud task; WSL is a subsystem on this computer. Source categories and folders use structured tuple keys so labels and delimiters cannot merge distinct groups. Remote integration will require a separate stable device identity.

## Export contract

`POST /api/sessions/context_export` takes an ordered JSON array of up to 20 unique `agent:id` references. It resolves native sessions first, then recorded gateway conversations, without accepting caller-supplied paths. Only user and assistant text is eligible; reasoning, tool output, attachments, system text and compaction content are excluded.

Exports retain at most the last 20 eligible text parts available to the reader and at most 8,000 Unicode codepoints per session, with a total content bound of 160,000. Native reader truncation and excerpt omissions remain explicit. This is literal extraction, not model summarization or a guarantee of complete history. Metadata is bounded and newline-safe, and the export identifies its text as untrusted reference material.

The response contains `text`, per-session status in `sessions`, and `omitted`. Missing, unreadable or textless histories are reported individually using sanitized errors. Partial results can be reviewed; an all-error response cannot be copied as a successful context package. Responses are not cached or persisted by the feature.

## Interaction and failure handling

Selection is held in page memory and survives filtering and pagination. Agent changes and the trash view clear it. Successful reloads remove unavailable selections. Selection changes, reloads, dialog close and agent changes invalidate pending previews. A late result cannot become the text copied for another selection. The preview renders plain text and uses the app's existing modal and clipboard fallback paths.

## Deferred integration

DotControl currently exposes target-local history and execution tools through a private MCP connection; its protected listener is loopback-only. OpenAI Secure MCP Tunnel is documented for supported OpenAI callers and does not establish a generic third-party client path to a personal Dot. Personal Dot task submission therefore remains a manual handoff in this phase.

Cross-device session discovery, on-demand remote reads, revocation, cache expiry and task result collection require a later bilateral protocol. Existing configuration/usage synchronization and inference sharing do not authorize conversation sharing. Sidebar Organizer informs evidence-based source grouping and stable logical keys; this change does not modify Codex's native sidebar groups.

## Verification and delivery

API tests use isolated native/gateway fixtures for filtering, ordering, bounds and partial failures. Browser checks cover independent selections, explicit copy, stale responses, grouping, literal rendering, keyboard controls and scroll preservation in Chromium and WebKit and the app's five languages. Repository build, vet and full-suite results must name the tested revision and any limitations.

The upstream README currently declines external pull requests. The implementation is prepared on a fork with reviewable commits and verification evidence; upstream acceptance is not assumed.
