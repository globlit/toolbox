# Security re-audit — 2026-10-29

Regression tests pass for the five reproduced disclosure cases, F1–F5. The runtime no longer accepts caller-selected configuration, addressing D1. No mutation bypass was reproduced through the command-line interface (CLI) or Model Context Protocol (MCP) server.

The conclusions apply to the tested inputs and [deployment assumptions](security.md). Arbitrary application logs can still disclose secrets.

## Findings and verification

P1 denotes high priority. P2 denotes medium priority. D1 depended on caller control of runtime configuration.

| Finding | Correction | Regression coverage |
| --- | --- | --- |
| F1, P1: JSON suffix parsing detached sensitive labels | Classify prefixes and continuation context before suffix parsing | `TestAuditF1F4FragmentsFailClosedPerLine`. CLI/MCP checks include a Forbidden response through standard error. |
| F2, P1: `tail=1` removed a sensitive label | Replace recognizable orphan fragments per line | `TestAuditF2OrphanTailLinesFailClosed`. CLI/MCP tail checks and a CLI `since=1s` check. |
| F3, P2: multiline values leaked continuation lines | Track quoted values, YAML blocks, certificate/key blocks, timestamps, and indented openers | `TestAuditF3MultilineStatesAndRecovery`, `TestUnclosedSecretSpansDoNotTrustFakeRecordPrefixes`. CLI/MCP quoted-value checks. |
| F4, P2: Python representations and embedded JSON escaped filtering | Reject ambiguous structured fragments. Sanitize embedded JSON and decoded keys. | `TestAuditF1F4FragmentsFailClosedPerLine`. CLI/MCP Python representation checks. |
| F5, P2: explicit rules skipped numeric values | Apply rules to scalar representations without changing unmatched numeric precision | `TestAuditF5RulesApplyToAllScalarTypes`. CLI/MCP numeric literal and regular-expression checks. |
| D1: caller configuration selected an arbitrary executable | Remove runtime configuration selection. Compile the approved path and configuration/kubectl digests into the executable. | `TestD1RuntimeCannotChooseConfigOrExecutable`, `TestD1UnprovisionedBuildHasNoEnvironmentFallback`, `TestD1PinnedConfigRejectsReplacement`, `TestD1ExecutableReplacementRejectedBeforeExecution`, `TestD1ScriptsAreNotNativeExecutables`, `TestStdioEndToEnd`. |

Tests require removal of synthetic markers `TOPSECRET42`, `654321`, and `987654`. They also require valid result envelopes, expected success/error behavior, and independent neighboring records. Empty output or request failure alone cannot satisfy the wire checks. Benign diagnostics, ordinary sensitive assignments, structured password fields, credential literals, and unmatched high-precision numbers provide controls.

An additional reproduction exposed an indented opening quote that lost continuation context. The following `INFO TOPSECRET42` escaped filtering before correction. Regressions cover ordinary and timestamp-prefixed indented openers. `TestConfiguredRulesCannotEraseContextAndCanSpanLines` covers cross-line rules and rules matching sensitive labels.

## Output behavior

Ambiguous physical lines become `[WITHHELD: ambiguous log fragment]`. Independent neighboring records remain. `withheld_lines` counts replaced lines.

Quoted values and certificate/key blocks remain suppressed until their closing delimiter. If that delimiter is missing, later lines remain suppressed through the end of the response. Earlier records remain visible. A plausible `INFO` prefix cannot terminate the sensitive span.

Timeout, cancellation, oversized output, and malformed resource JSON still suppress the whole response. No raw-output override exists.

## Configuration migration

Version 0.2 rejects runtime `--config`. Plain builds reject operational requests. Use the operator [setup procedure](setup.md) to build an executable with approved configuration and kubectl digests. The provisioning package is absent from the runtime dependency graph.

Filesystem protection remains necessary. Hash verification cannot prevent concurrent replacement of writable executables. Native headers do not establish provenance. Same-user agents with direct credential access or permission to replace the gateway remain outside the security boundary.

## Read-only verification

- The catalog policy, credential loader, and both permission manifests match the pre-correction SHA-256 baseline.
- All 39 catalog routes use the expected API group, version, and resource path.
- All 56 additional adversarial resource/name/pod/container fields are rejected.
- Tests reject mutations, unsupported subresources, Secrets, ConfigMaps, raw paths, identity/configuration overrides, namespace escapes, unsupported flags, and unknown MCP fields.
- Local wire checks observed only GET requests. Denied reads made no requests. Resource-name collisions could not redirect reads to another API group.
- Credential, environment, concurrency, cancellation, output-limit, and permission-manifest checks passed.

## Evidence and limits

Verification used Linux amd64, Go 1.26.10, and kubectl v1.34.1 against a temporary local mock API with Transport Layer Security (TLS). Credentials and logs were synthetic. No production cluster or existing kubeconfig was accessed. macOS and Windows were cross-compiled but not executed natively.

Read [validation](validation.md) for commands and results. Local generated evidence is under `dist/audit/`. The original report and reproduction source remain under `/tmp/toolbox-security-audit-va4ckl4t/`. That temporary directory is not a project dependency.

F2 is corrected for recognizable fragments in the regression matrix. Upstream truncation can still remove sensitivity context from a complete JSON object or an ordinary-looking log line. Unlabeled secrets, unsupported encodings, and application-specific semantics remain limitations. Actual cluster permissions, filesystem access controls, client isolation, and production log coverage require deployment checks.
