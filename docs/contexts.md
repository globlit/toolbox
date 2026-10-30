# Context selection

Add `--config /etc/toolbox/toolbox.json` to each CLI command below, or use an operator-built default. Configure the same file in your [MCP client](clients.md).

Use one Toolbox executable with several approved contexts from the same kubeconfig. Follow [configuration setup](setup.md#multiple-contexts) first. The examples use `prod-a` with `apps` and `observability`, and `prod-b` with `billing`.

## List contexts

```sh
toolbox kubectl contexts
```

Illustrative output:

```json
{
  "contexts": [
    {"name":"prod-a","default_namespace":"apps","namespaces":["apps","observability"],"allow_cluster_scoped":false,"allow_port_forward":false},
    {"name":"prod-b","default_namespace":"billing","namespaces":["billing"],"allow_cluster_scoped":false,"allow_port_forward":false}
  ]
}
```

MCP tool call:

```json
{"name":"kubectl_contexts","arguments":{}}
```

The MCP result exposes the same object in `result.structuredContent`. The catalog lists Toolbox's approved contexts, not every context in the kubeconfig. It does not test connectivity or Kubernetes permissions.

## Select a context

Supply `--context NAME` or `--context=NAME` before or after the kubectl subcommand:

```sh
toolbox kubectl --context prod-a get pods -n apps -o json
toolbox kubectl get deployments --context=prod-b -n billing
toolbox kubectl logs api-0 --context prod-a -n apps --tail 20
toolbox kubectl resources --context prod-b
toolbox kubectl get --raw /api/v1/namespaces/billing/pods --context prod-b
```

Use the `context` field for MCP reads and resource catalogs:

```json
{"name":"kubectl_get","arguments":{"context":"prod-b","resource":"pods","namespace":"billing"}}
```

```json
{"name":"kubectl_logs","arguments":{"context":"prod-a","pod":"api-0","namespace":"apps","tail":20}}
```

One approved context is the default. Multiple approved contexts require explicit selection on every read or tunnel-start request. Omit `namespace` to use that context's `default_namespace`. Unknown contexts and unapproved namespaces fail before kubectl starts.

Toolbox does not change the kubeconfig or use `kubectl config use-context`. Concurrent requests can select different contexts. Read results report `context`. Existing tunnels continue using their original context.

## Query approved namespaces

```sh
toolbox kubectl get pods --context prod-a -A -o json
```

Equivalent MCP tool call:

```json
{"name":"kubectl_get","arguments":{"context":"prod-a","resource":"pods","all_namespaces":true}}
```

Illustrative output when both approved namespaces are empty:

```json
{
  "context": "prod-a",
  "namespaces": ["apps", "observability"],
  "data": {"apiVersion":"v1","kind":"List","items":[]},
  "redactions": 0,
  "untrusted": true
}
```

`-A` and `--all-namespaces` query approved namespaces in the selected context. With `namespaces: ["*"]`, unrestricted reads use the cluster-wide list endpoint. Read [wildcard behavior and ConfigMap exclusions](configmaps.md). The options reject an object name, `-n`, raw paths, and cluster-scoped resources. Each query must succeed with a complete list. Failure, cancellation, timeout, or excess output discards the combined data.

`-o json` and `--output=json` preserve the sanitized result envelope. Resource JSON appears under `data`. MCP already returns JSON.

## Query several contexts

Send one request per context. This shell example returns one JSON result per line:

```sh
for context in prod-a prod-b; do
  toolbox kubectl get pods --context "$context" -A -o json || break
done
```

In MCP, call `kubectl_get` separately with each context name. Toolbox does not provide a combined cross-context snapshot. All contexts share the server's two subprocess slots, including active tunnels.

## Port forwarding

Enable forwarding for the selected context, then specify it when starting a tunnel:

```sh
toolbox kubectl port-forward service/api 18080:80 --context prod-a -n apps --duration 1m
```

Read [port-forward configuration, outputs, and lifecycle](raw-and-port-forward.md#enable-tunnels). Status and stop calls use the tunnel ID, without another context selection.
