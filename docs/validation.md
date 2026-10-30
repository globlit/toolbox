# Validation

Local verification date: 2026-10-29. Native host: Linux amd64. Build toolchain: Go 1.26.10. MCP SDK: v1.8.0. A real kubectl v1.34.1 was used only against an ephemeral local TLS mock API, with synthetic credentials. No real cluster or existing kubeconfig was accessed.

## Verified

- `TOOLBOX_TEST_KUBECTL=/usr/local/bin/kubectl GOMAXPROCS=2 go test -race -p=1 ./...` passes, including a provisioned CLI subprocess, actual MCP stdio subprocess/client sessions, and the real-kubectl mock API tests.
- MCP discovery and calls pass for protocol revisions `2025-06-18`, `2025-11-25`, and `2026-07-28` using the official SDK client. Unknown fields, forbidden resources, out-of-scope namespaces, and command failures are exercised.
- The real kubectl test observes fixed resource/log GET requests. Denied reads generate no requests. An adversarial discovery catalog with an identically named resource in another API group cannot redirect a read; a missing core resource fails at the original core endpoint.
- Policy tests cover mutation commands, exec/proxy/port-forward, Secrets/ConfigMaps, raw paths, output/file/template flags, identity/server/config overrides, verbosity, multi-resource input, traversal, namespace escape, and log bounds.
- Output tests cover sanitized stdout/stderr, malformed JSON withholding, all-output withholding on overflow/timeout, configuration snapshots, isolated environment, private-directory cleanup, and the process concurrency limit.
- Synthetic fixtures exercise OpenSearch, RabbitMQ, Kvrocks, CNPG/Postgres, Python, controller, observability, and networking log formats. Tests preserve diagnostic markers while removing injected canaries, including nested JSON, PEM, indented multiline values, and encoded/control-character cases.
- RBAC tests compare every granted resource to the catalog, reject wildcard/non-read permissions, and check that default bindings remain namespaced.
- The [security re-audit](security-audit.md) maps all five disclosure findings and the caller-controlled config bypass to passing regressions. It covers per-line withholding, preserved neighboring diagnostics, numeric rules, tail/since context loss, and config/executable integrity rejection.
- Five-second, single-worker fuzz runs pass: `FuzzSensitiveSuffix` (31,436 executions), `FuzzSensitiveJSON` (21,063), and `FuzzPolicy` (37,868). These are bounded regression checks, not exhaustive security proofs.
- `go vet ./...` and `go mod verify` pass.
- `govulncheck v1.9.0` reports **No vulnerabilities found** after pinning Go 1.26.10 and `golang.org/x/sys v0.44.0`. This reflects the vulnerability database available during this run, not a permanent guarantee.

All six v0.2.0 builds completed with Go 1.26.10: Linux, macOS, and Windows, each for amd64 and arm64. Embedded build metadata and every `dist/SHA256SUMS` entry were verified. Only the Linux amd64 executable was run locally. These portable builds are intentionally unprovisioned: version output succeeds and operational requests fail closed. Operator-provisioned binaries are exercised in the CLI/MCP tests.

Local evidence: `dist/audit/race-tests.txt`, `wire-and-policy.txt`, `fuzz-suffix.txt`, `fuzz-json.txt`, `fuzz-policy.txt`, `vulnerabilities.txt`, `builds.txt`, and `build-verification.txt`. These are ignored generated artifacts. Reproduce the targeted wire and policy checks with:

```sh
TOOLBOX_TEST_KUBECTL=/absolute/path/kubectl go test -count=1 -v ./internal/integrations/kubectl -run 'TestAuditPolicyInvariants|TestRealKubectl|TestSecurityRegressionsOverCLIAndMCP'
```

## Deployment checks still required

- Native macOS/Windows execution, ACL behavior, cancellation, and integration into the actual Codex/Claude Code installations. CI defines tests for those operating systems; no CI run has occurred here.
- Actual cluster RBAC, all other grants to the dedicated identity, API versions, network isolation, and prevention of direct agent access to privileged credentials or unsanitized outputs.
- Representative approved production logs. Fixtures are synthetic and sanitization cannot guarantee removal of arbitrary secrets or PII.
- A separately maintained, supported kubectl binary. Toolbox verifies its operator-approved digest; it does not establish provenance or update it automatically.

The executable does not provision these boundaries, register itself with clients, apply RBAC, or contact a cluster during installation.
