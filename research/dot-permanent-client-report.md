# Permanent Dot subscription client compatibility

Base: merge 024dbe44. Branch: feat/dot-permanent-subscription.
Contract: DC feat/dot-permanent-subscription a953d55, docs/dot-permanent-subscriptions.md.

## Behavior

Status.ExpiresAt, SubscriptionStatus.ExpiresAt and RefreshBefore are nullable pointers. VerifiedAt stays time.Time. Authorized requires v1, enabled, paired, ready, active subscription, nonzero/nonfuture verification, and every present nonzero expiry still in the future. Legacy year-one optional dates normalize to nil on the HTTP read. DisplayCode checks both expiry bounds. Neither absent dates nor display metadata grants capability or authorization.

CLI keeps JSON output, adding subscription_term="permanent" for active subscriptions with no finite bound. GUI API publishes the same derived label. Routing and Dot page translate it through core i18n (en, zh, zh-TW, ja, de). Missing last_refreshed_at is omitted using omitzero instead of printing a synthetic year-one timestamp. Finite dates remain unchanged. Dot provider, cached readiness, GUI/CLI tasks and gateway admission use the shared authorization predicate; unrelated providers' ExpiresAt fields were inspected and left unchanged.

## RED/GREEN

Before implementation, TestPermanentStatus failed absent, null and zero cases with Authorized=false, want true. TestDotCLIPermanentStatus failed because permanent metadata was missing and last_refreshed_at printed year one. Chromium dot-routing-status failed "en: missing permanent label" (permanent-red.log).

After implementation, focused TestPermanent/TestDot checks pass in dotbridge, cli, gui, provider and gateway. Full dotbridge package passes. Coverage includes null decoding to nil, absent dates, zero dates, inactive/expired state, missing/zero/future proof, disabled/unpaired/wrong version/not ready, independently finite past proof/subscription bounds, GUI handler JSON and task gates, CLI output, and provider text-only/task-only model visibility. Existing finite fixtures were migrated to pointers and still pass focused tests.

Targeted browser checks passed in Chromium and WebKit, five locales, Dot page at 1100/420 pixels; routing status plus core/plugin i18n ownership checks passed (5 tests each). Logs: C:/Users/13678/AppData/Local/Temp/opencode/magpie-context-verification/permanent-chromium.log and permanent-webkit.log. First broader Dot browser invocation hit a 120-second tool timeout and is not counted as a pass; a second invocation has no tool timeout.

Windows go vet ./..., go vet -tags nogui ./..., go build ./..., go build -tags nogui ./... passed. Linux cross-vet/build -tags nogui ./... passed. gofmt and git diff --check passed. Race attempt could not run: cgo disabled, no gcc on PATH.

Full go test -tags nogui ./... first run was cancelled by a harness restart and is not counted as a result. The restarted run writes C:/Users/13678/AppData/Local/Temp/opencode/permanent-full-restart.log. The broader browser rerun was also cancelled by the restart; its replacement uses permanent-all-dot-restart.log.

Supplemental TestBuiltinsTranslated initially failed because the Windows working file had CRLF but its fixture parser requires LF. Writing the touched i18n.js with the repository-specified LF endings resolves it without any additional Git diff. TestBuiltinsTranslated, TestBootSaysBuiltinsOff and TestCoreScriptBudget then passed. Final focused checks across all five affected packages passed again after the restart.

## Final broader verification

The completed browser rerun passed 58/58 tests across eight files, Chromium and WebKit, five locales, in 832.33 seconds (permanent-all-dot-restart.log). Earlier cancelled or timed-out invocations are not counted.

The restarted full Go run failed internal/agent after its default 10-minute timeout, with TestReverseProxyTriedFirst running. Earlier failures in that run: TestAsideOfflineDisconnectRestoresWithoutDaemon (native fixture must report missing CLI), TestCodexSignInInKeychain (sign-in not detected), TestCopilotFolderFromAnIDEPluginIsNotTheCLI, TestQoderSettingsFromAnIDEPluginIsNotTheCLI/qoder-cn and /qoder (PATH fixtures), TestInstallWithoutNodeParses (bash resolves to Windows WSL stub without a distribution). The runner did not produce a complete final package summary; this is a failed, incomplete full-suite run, not full green. Exact output is retained in permanent-full-restart.log. These failures are outside the edited Dot paths; the install/timeout failures are not claimed to match the historical baseline.

## Windows fixture diagnostics

Separate focused reproduction (permanent-platform-fixtures.log) failed TestCodexSignedInInKeychain/keyring and /auto (platform keychain not detected), TestPluginPageServedShutIn and TestSweepBridgeProjectsTakesOnlyTheBridgesFolders (symlink privilege missing), TestClaudeAsRootEnv (Unix root IS_SANDBOX expectation), and TestSQLiteSurvivesMalformedEnv (Windows process creation rejects malformed environment). These same named failures exist in the earlier magpie-sustainable-full.log; this is historical evidence, not a claim that a freshly fetched main was tested. No unrelated fixture implementation was changed.

## Subscription requester / rollout boundary

Repository-wide search found no Magpie events/subscribe request sender or ttlMs request construction (only the documentation added here mentions ttlMs). ChatGPT owns subscription creation/renewal. This client does NOT send ttlMs:null and does not convert existing finite grants. ChatGPT must explicitly request null; omission retains DC's finite default. Actual ChatGPT request behavior remains unverified. No live configuration, credentials or jobs were used; HTTP/browser fixtures and test sandbox/keyring substitutes cover this change.
