# Security re-audit — 2026-10-29

The five previously reproduced disclosure cases (F1–F5) now pass regression tests. The caller-controlled configuration path (D1) has been removed from the runtime CLI. No mutation bypass was reproduced through the exposed CLI/MCP API. This conclusion covers the tested cases and deployment assumptions below; it is not a guarantee that arbitrary application logs are secret-free.

## Findings and verification

| Finding | Remediation | Verification |
| --- | --- | --- |
| F1, P1: a JSON suffix detached a `request_body` or `password` label from its value | Classify sensitive prefixes and continuation state before accepting a JSON suffix; withhold the affected physical line | `TestAuditF1F4FragmentsFailClosedPerLine`; actual CLI and MCP subprocesses against real kubectl/local TLS mock, including a Forbidden response leaking through stderr in the original audit |
| F2, P1: `tail=1` removed a multiline secret's label | Withhold recognizably orphaned lines: indentation, bare/scalar values, unframed plaintext, arrays and malformed structured fragments | `TestAuditF2OrphanTailLinesFailClosed`; CLI/MCP tail reproduction; additional CLI `since=1s` boundary reproduction |
| F3, P2: quoted multiline values leaked continuation lines | Track sensitive single/double/triple quotes, YAML-style blocks and PEM spans; handle recognized timestamp prefixes and retain state when an opening line is indented | `TestAuditF3MultilineStatesAndRecovery` and `TestUnclosedSecretSpansDoNotTrustFakeRecordPrefixes`; CLI/MCP quoted-value reproduction |
| F4, P2: Python repr and embedded escaped JSON bypassed filtering | Withhold unsupported structured fragments without evaluating them; recursively sanitize embedded JSON strings and decoded keys | `TestAuditF1F4FragmentsFailClosedPerLine`; Python repr reproduction through CLI/MCP |
| F5, P2: configured literal/pattern rules skipped JSON numbers | Apply configured rules to numeric, boolean and null representations, including bounded equivalent exponent spellings; preserve unmatched number precision | `TestAuditF5RulesApplyToAllScalarTypes`; CLI/MCP numeric literal and regex canaries |
| D1, conditional deployment bypass: `--config` selected an arbitrary executable | Remove runtime config selection and fallbacks. An operator build pins the approved config path, exact config digest, and kubectl digest. Verify the same config bytes that are parsed, and check kubectl digest/native format before execution | `TestD1RuntimeCannotChooseConfigOrExecutable`, `TestD1UnprovisionedBuildHasNoEnvironmentFallback`, `TestD1PinnedConfigRejectsReplacement`, `TestD1ExecutableReplacementRejectedBeforeExecution`, `TestD1ScriptsAreNotNativeExecutables`, and built-process `TestStdioEndToEnd` |

The original synthetic canaries were `TOPSECRET42`, `654321`, and `987654`. The regression tests require their absence, valid sanitized result envelopes, expected success/error behavior, and preservation of independent neighboring records. A failed or empty request alone cannot satisfy the wire tests. Existing benign diagnostic markers, ordinary sensitive assignments, structured password fields, credential literals, and unmatched high-precision numbers provide controls.

Re-audit also exposed a variant where an indented opening quote was withheld without retaining quote state. A following `INFO TOPSECRET42` was reproduced as a leak before correction; both ordinary and timestamp-prefixed indented openings now have regression coverage. Fake severity prefixes inside an open quoted secret or PEM span cannot terminate withholding. Cross-line configured rules and rules matching sensitive labels also have regressions in `TestConfiguredRulesCannotEraseContextAndCanSpanLines`.

## Per-line failure behavior

An ambiguous log line becomes `[WITHHELD: ambiguous log fragment]`; newline positions and independent neighboring records remain. `withheld_lines` reports how many lines were withheld. There is no raw-output override.

For quoted/PEM values, the entire sensitive span remains ambiguous until its closing delimiter. If no closing delimiter arrives, subsequent lines remain withheld through the end of that span; preceding records survive. Treating a plausible `INFO` prefix as proof of a new record would reopen the verified leak. This can hide harmless lines after a malformed opener, but does not replace a successful dump with one error.

Existing whole-response withholding on timeout, cancellation, oversized output, or malformed resource JSON remains unchanged. Those conditions cannot safely return a complete sanitized result.

## Configuration boundary and migration

Version 0.2 intentionally rejects the former `toolbox --config PATH ...` interface. Runtime commands are now `toolbox kubectl ...` and `toolbox mcp` with no config arguments. Config/environment/home/current-directory selection cannot change the compiled trust anchors. Plain builds are unprovisioned and reject operational requests.

An operator uses `go run ./scripts/provision -config /absolute/operator.json -out /absolute/toolbox` from the reviewed source checkout. Configuration or kubectl changes require reprovisioning. Credential contents may rotate at the approved kubeconfig path; restart MCP to refresh its static snapshot. The provisioning package is absent from `go list -deps ./cmd/toolbox`; the runtime contains no provisioning endpoint.

These are trust anchors, not an OS sandbox. Protect the gateway, approved kubectl, config, kubeconfig and their parent directories from the agent. Concurrent replacement of writable executables can race a hash check. Native-format checks do not establish provenance; an operator must approve a trustworthy kubectl. A same-user agent able to replace the gateway or directly read credentials remains outside this boundary. Enforce least-privilege RBAC and isolate credential/network access as described in [the security model](security.md).

## Read-only controls retained

- The catalog/path policy, strict credential loader, and both RBAC manifests have identical SHA-256 hashes to the pre-remediation audit baseline.
- All 39 catalog routes retain their exact API group/version/resource path. All 56 additional adversarial resource/name/pod/container field cases are rejected.
- Tests retain denial of mutations, exec/attach/proxy/port-forward, Secrets/ConfigMaps, raw paths, identity/server/config overrides, namespace escape, unsupported flags and unknown MCP fields.
- Local TLS wire observations contain GET requests only. Denied reads issue no requests, and an API-group name collision cannot redirect a read.
- Credential snapshot validation, isolated subprocess environment, bounded concurrency, cancellation/overflow handling, failed stdout withholding, and RBAC/catalog consistency tests pass.

## Evidence and limits

The final test/build results and reproducible commands are recorded in [validation.md](validation.md). Local evidence is retained under `dist/audit/` (ignored build artifacts). The original read-only audit and reproduction source remain at `/tmp/toolbox-security-audit-va4ckl4t/`; that temporary directory is not a durable project dependency.

Verification used Linux amd64, Go 1.26.10, real kubectl v1.34.1 against an ephemeral local TLS mock, synthetic credentials and synthetic logs. No production cluster, production logs or existing kubeconfig was used. macOS/Windows were cross-compiled, not natively executed. Actual cluster permissions, OS ACLs, client isolation and production format coverage still require deployment validation.

F2 is fixed for the recognizable fragments in the regression matrix. Upstream truncation can still remove sensitivity context from a complete JSON object or an ordinary-looking timestamped/severity-prefixed line. Arbitrary unlabeled secrets, unsupported encodings and application-specific semantics can also survive heuristic sanitization. These documented limitations remain; use source-level secret suppression and explicit application rules rather than treating this gateway as complete DLP.
