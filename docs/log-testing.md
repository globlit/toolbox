# Log sanitizer tests

Run the deterministic corpus:

```sh
go test ./internal/sanitize
```

The [complex log corpus](../internal/sanitize/testdata/complex_logs.json) adds 127 synthetic fixtures across 36 families. Each fixture uses a synthetic secret that the format tests do not configure as a literal. The tests check secret removal, redaction counts, physical newline counts, and independent neighboring records.

| Cases | Formats |
| --- | --- |
| Structured records | Nested JSON, arrays, duplicate keys, escaped keys, serialized JSON, multiline exception strings |
| Logfmt | Quoted and unquoted values, dotted keys, quoted keys with spaces, duplicate assignments, embedded JSON |
| Malformed records | Missing braces, trailing commas, invalid escapes, concatenated JSON, incomplete arrays, truncated parents, unfinished quotes |
| Services | OpenSearch, RabbitMQ, Kvrocks, CNPG/Postgres, cert-manager, external-dns, Prometheus, Alertmanager, Fluent Bit, Cilium, CoreDNS, Nginx, Envoy, kubeseak |
| Source snippets | Go, Python, JavaScript, TypeScript, Java, C#, Ruby, PHP, Rust, C, C++, shell, PowerShell, templates |
| SQL | PostgreSQL, MySQL, SQLite, SQL Server, bind parameters, transactions, slow-query logs |
| Stack traces | Python, Go, Java, Kotlin, JavaScript, TypeScript, Rust, Ruby, PHP, .NET, C++, Swift, Erlang, Elixir, PowerShell, Bash |
| Adversarial inputs | Fake record boundaries, prompt-injection text, terminal escapes, Unicode formatting characters, NUL bytes, private keys, multiline sensitive blocks |

The suite checks LF and CRLF endings, 15,625 ordered pairs of closed records, three JSON-envelope depths, mixed dumps, configured-credential tail cuts, and concurrent requests. Unterminated quotes and private keys continue masking until their closing delimiter. The pair tests exclude those two unfinished fixtures.

Two fixtures test unknown values at a log-tail boundary. They skip the single-record JSON-envelope check because a complete JSON field supplies context that the original tail fragment lacks. Unknown secrets in ordinary prose still require explicit rules. These synthetic fixtures do not establish production format coverage.

The generated tests add 400 mixed records and 96 deterministic random-byte inputs. Size tests cover empty and one-byte inputs, a 1 MiB mixed dump, and malformed lines up to 8 MiB. Code-snippet tests check that sanitization causes no file changes, file-content disclosure, or HTTP requests.

## Fuzzing

Run one target at a time. Replace the target name to select another row.

```sh
GOMAXPROCS=2 go test ./internal/sanitize -run '^$' \
  -fuzz '^FuzzComplexLogCombinations$' -fuzztime=60s -parallel=2
```

| Target | Checks |
| --- | --- |
| `FuzzComplexLogCombinations` | Mixed records, line endings, JSON transport envelopes |
| `FuzzSensitiveLogfmtValues` | Generated values, sensitive key spellings, quoting, separators |
| `FuzzMalformedJSONRecovery` | Malformed parents cannot expose valid inner objects |
| `FuzzKnownCredentialInArbitraryLogs` | Generated credentials in arbitrary bytes, normalized credentials, JSON envelopes |
| `FuzzComplexStructuredLogs` | Nested objects, arrays, serialized JSON, recursion limits |
| `FuzzEmbeddedSource` | Generated Go, Python, JavaScript, SQL, shell, and PHP snippets in log streams and JSON envelopes |
| `FuzzConfigMapData` | Readable ConfigMap settings with generated JSON, YAML, properties, and sensitive values |
| `FuzzArbitraryLogBytes` | Arbitrary bytes, UTF-8 validity, terminal controls, newline counts, report consistency |

Linux continuous integration runs each target for 15 seconds. Ordinary `go test` runs their seed cases on every supported operating system. Retain any failing Go fuzz inputs under `internal/sanitize/testdata/fuzz/` as regression cases.

`FuzzConfigMapNamespacePolicy` also checks namespace exclusions, global disable, wildcard selections, aliases, and raw-path equivalence in `internal/integrations/kubectl`. `FuzzWildcardRawPath` mutates API paths under wildcard namespace policies.

## CLI and MCP

Set `TOOLBOX_TEST_KUBECTL` to a local kubectl executable:

```sh
TOOLBOX_TEST_KUBECTL=/usr/local/bin/kubectl GOMAXPROCS=2 \
  go test -race -p=1 ./internal/integrations/kubectl \
  -run '^TestSecurityRegressionsOverCLIAndMCP$' -count=1
```

The test uses a local TLS mock Kubernetes API. It sends the 125 closed fixtures through the command-line interface (CLI) and Model Context Protocol (MCP) server. Both paths must remove the synthetic secret and preserve all independent checkpoints. No production cluster is contacted.

## Kind end-to-end tests

Install Docker, Go, Python 3, kubectl, and kind v0.33.0. Run:

```sh
bash scripts/e2e/kind.sh
```

The script creates a separate kind cluster with a temporary kubeconfig. It builds a local log-emitter image, provisions synthetic ConfigMaps, and tests CLI/MCP requests with cluster-wide and namespace-bound service accounts. It checks ConfigMap defaults, exclusions, global disable, wildcard reads, Kubernetes authorization failures, mutation rejection, and the mixed log corpus. Cleanup deletes only the test cluster, its fixture image, and temporary files.

GitHub Actions runs this test on Ubuntu before release publication. The workflow uses kind v0.33.0 and a Kubernetes v1.34.11 node image fixed by digest. [Kind release images](https://github.com/kubernetes-sigs/kind/releases/tag/v0.33.0).
