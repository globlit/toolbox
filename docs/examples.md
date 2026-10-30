# Command examples

Use the `toolbox` executable. Replace `apps`, `api`, `api-0`, and `app` with your namespace, deployment, pod, and container names.

Examples 4–6 show `tools/call` requests after session initialization. Use a configured [MCP client](clients.md) to send them. Successful output blocks show `result.structuredContent`. The error block shows `result.content` and `result.isError`. Transport metadata is omitted.

## 1. List pods with the CLI

```sh
toolbox kubectl get pods -n apps
```

Output:

```json
{
  "data": {
    "apiVersion": "v1",
    "items": [
      {
        "metadata": {
          "labels": {
            "app": "api"
          },
          "name": "api-0",
          "namespace": "apps"
        },
        "status": {
          "phase": "Running"
        }
      }
    ],
    "kind": "PodList"
  },
  "redactions": 0,
  "untrusted": true
}
```

## 2. Read deployment status with the CLI

```sh
toolbox kubectl get deployments api -n apps
```

Output:

```json
{
  "data": {
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {
      "name": "api",
      "namespace": "apps"
    },
    "status": {
      "readyReplicas": 2,
      "replicas": 2
    }
  },
  "redactions": 0,
  "untrusted": true
}
```

## 3. Read sanitized logs with the CLI

```sh
toolbox kubectl logs api-0 -n apps -c app --tail 20 --since 10m
```

Output:

```json
{
  "redactions": 1,
  "text": "INFO ready\n[WITHHELD: ambiguous log fragment]\nINFO request complete\n",
  "untrusted": true,
  "withheld_lines": 1
}
```

The middle input line was `request_body={"customer":"synthetic-customer"}`. Toolbox replaced that line and preserved both neighboring records.

## 4. Filter pods through MCP

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "kubectl_get",
    "arguments": {
      "namespace": "apps",
      "resource": "pods",
      "selector": "app=api"
    }
  }
}
```

Output:

```json
{
  "data": {
    "apiVersion": "v1",
    "items": [
      {
        "metadata": {
          "labels": {
            "app": "api"
          },
          "name": "api-0",
          "namespace": "apps"
        },
        "status": {
          "phase": "Running"
        }
      }
    ],
    "kind": "PodList"
  },
  "redactions": 0,
  "untrusted": true
}
```

## 5. Read previous container logs through MCP

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "kubectl_logs",
    "arguments": {
      "container": "app",
      "namespace": "apps",
      "pod": "api-0",
      "previous": true,
      "since": "10m",
      "tail": 20
    }
  }
}
```

Output:

```json
{
  "redactions": 1,
  "text": "{\"level\":\"error\",\"message\":\"connection refused\",\"password\":\"[REDACTED]\"}\n",
  "untrusted": true
}
```

## 6. Reject a Secret read through MCP

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "kubectl_get",
    "arguments": {
      "namespace": "apps",
      "resource": "secrets"
    }
  }
}
```

Output:

```json
{
  "content": [
    {
      "type": "text",
      "text": "resource is not permitted. Use toolbox kubectl resources or kubectl_resources for the catalog"
    }
  ],
  "isError": true
}
```

Toolbox rejects this request before invoking kubectl.

## CLI results

| Exit code | Meaning |
| --- | --- |
| `0` | Successful request |
| `1` | Denied request, failed tool execution, or transport failure |
| `2` | Invalid command or configuration |

Successful reads return `data` or `text`. Empty log output may omit `text`. Failed tool execution returns `error` and optional sanitized `diagnostics`. Partial standard output is discarded. `redactions` counts replacements. `withheld_lines` counts replaced ambiguous lines. `untrusted` marks cluster output as untrusted data.

Run `toolbox --help` for supported flags. There is no arbitrary flag forwarding.
