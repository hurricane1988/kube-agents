# kube-agents

AI-powered Kubernetes operations assistant built on [tRPC-Agent-Go](https://github.com/trpc-group/trpc-agent-go) and DeepSeek.

Describe K8s resources in natural language — the agent understands intent, calls the right tools, and executes via the Kubernetes API.

```
> 列出 default 命名空间中 Running 状态的 pod

default 命名空间 Running 状态的 Pod：
1.  ba-0223-app-ccp-r5t67    IP: 10.240.147.181  节点: worker07
2.  ks-web-z5t2q             IP: 10.240.179.59   节点: worker04
3.  nacos-register-hlbgb     IP: 10.240.179.52   节点: worker04
4.  txc-go-gin-grckt         IP: 10.240.50.180   节点: worker02

共 4 个 Pod 处于 Running 状态。
```

## Features

| Category | Capability |
|----------|------------|
| **K8s Operations** | 24 built-in tools covering pods, deployments, services, ingresses, HPAs, ConfigMaps, Secrets, namespaces, events, generic resources |
| **LLM Backend** | DeepSeek (default), any OpenAI-compatible model |
| **AuthN/AuthZ** | K8s ServiceAccount TokenReview + SubjectAccessReview; JWT extensible |
| **HTTP API** | OpenAI-compatible `POST /v1/chat/completions` (non-streaming + SSE streaming) |
| **A2A Protocol** | Agent-to-Agent `message/send` + `message/stream` JSON-RPC 2.0 endpoints |
| **CLI** | `chat` (interactive), `serve` (API server), `version` (table output) via cobra |
| **Plugins** | Built-in audit, rate-limit, logging; custom via `plugin.Plugin` interface |
| **MCP** | External tool servers via stdio / SSE / streamable transports |
| **Skills** | Reusable SKILL.md workflows: diagnose, deploy, security audit |
| **RAG Knowledge** | Semantic search over K8s docs; runtime file upload API |
| **Session** | Conversation state persistence (memory / Redis) |
| **Memory** | Long-term user preference tracking (memory / Redis) |
| **Logging** | `log/slog` structured logging: JSON/text, levels, file rotation (lumberjack) |
| **Graceful Shutdown** | HTTP server drains in-flight requests; configurable timeout |
| **Distroless Image** | Multi-stage Docker build, runs as non-root |

## Quick Start

### Prerequisites

- Go 1.21+
- DeepSeek API key (or OpenAI-compatible endpoint)
- Kubernetes cluster (optional — tools auto-disable if unreachable)

### Install

```bash
git clone https://github.com/codefuture-io/kube-agents.git
cd kube-agents
make build
```

### Run

```bash
# Interactive chat (K8s tools auto-load)
./bin/kube-agents chat --api-key=sk-your-key

# Start HTTP + A2A servers
./bin/kube-agents serve --api-key=sk-your-key

# With config file
./bin/kube-agents serve --config=config/kube-agents.yaml --api-key=sk-your-key
```

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DEEPSEEK_API_KEY` | Yes | DeepSeek API key |
| `OPENAI_API_KEY` | No | Fallback API key |
| `OPENAI_BASE_URL` | No | Custom base URL (default: `https://api.deepseek.com`) |
| `KUBECONFIG` | No | Kubeconfig path (default: `~/.kube/config`) |

---

## CLI Reference

### Commands

| Command | Description |
|---------|-------------|
| `kube-agents chat` | Interactive terminal chat with K8s agent |
| `kube-agents serve` | Start HTTP + gRPC + A2A servers |
| `kube-agents version` | Print version info in table format |

### Flags

| Flag | Default | Env Fallback | Description |
|------|---------|-------------|-------------|
| `--api-key` | — | `DEEPSEEK_API_KEY`→`OPENAI_API_KEY` | API key |
| `--base-url` | — | `OPENAI_BASE_URL` | API base URL |
| `--model` | `deepseek-chat` | — | Model name |
| `--config` | `config/kube-agents.yaml` | — | Config file path |
| `--log-level` | `info` | — | `debug`, `info`, `warn`, `error` |
| `--log-format` | `text` | — | `text`, `json` |
| `--log-add-source` | `false` | — | Include source file:line in logs |
| `--log-file-output` | `false` | — | Write logs to file instead of stderr |
| `--log-file-path` | — | — | Log file path |
| `--log-max-size` | `100` | — | Max MB before rotation |
| `--log-max-backups` | `10` | — | Max old log files to keep |
| `--log-max-age` | `30` | — | Max days to keep old logs |

### Logging

```bash
# JSON format, debug level
./bin/kube-agents serve --log-format=json --log-level=debug

# Write to file with rotation
./bin/kube-agents serve \
  --log-file-output --log-file-path=/var/log/kube-agents.log \
  --log-max-size=500 --log-max-backups=30
```

---

## HTTP API

### Endpoint

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | Chat completion (non-streaming + SSE streaming) |
| `POST` | `/v1/knowledge/upload` | Upload a file to the RAG knowledge base |

### Chat Completion

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "List all namespaces"}],
    "stream": false
  }'
```

### Streaming (SSE)

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Get deployment nginx details"}],
    "stream": true
  }'
```

### Response Format

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "model": "gpt-3.5-turbo",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "default namespace has 7 pods:\n..."},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 3019, "completion_tokens": 47, "total_tokens": 3066}
}
```

---

### Knowledge Upload

Upload files to the RAG knowledge base at runtime. Accepted formats: `.md`, `.txt`, `.yaml`, `.yml`, `.json`, `.html`, `.pdf` (max 10MB).

```bash
curl -X POST http://localhost:8080/v1/knowledge/upload \
  -F "file=@docs/kubernetes-guide.md"
```

Success: `{"message":"file uploaded and indexed","filename":"kubernetes-guide.md","size":2048}`

> RAG indexing requires an OpenAI-compatible `/embeddings` endpoint. DeepSeek does not currently support embeddings — use an OpenAI API key via `OPENAI_API_KEY`.

---

## A2A Protocol

kube-agents implements the [Agent-to-Agent](https://github.com/trpc-group/trpc-a2a-go) protocol. Other AI agents can discover and invoke kube-agents as a tool.

### Enabling

```yaml
server:
  a2a:
    enabled: true
    host: "kube-agents.default.svc.cluster.local:18080"
```

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/.well-known/agent-card.json` | Agent metadata discovery |
| `POST` | `/` | JSON-RPC 2.0 endpoint |

### Agent Card Discovery

```bash
curl http://localhost:18080/.well-known/agent-card.json
```

Response:
```json
{
  "name": "kube-agents",
  "description": "Kubernetes AI operations assistant with 24 built-in K8s tools",
  "url": "http://localhost:18080"
}
```

### Non-Streaming (`message/send`)

```bash
curl -X POST http://localhost:18080/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "message/send",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"kind": "text", "text": "How many namespaces are in this cluster?"}]
      }
    }
  }'
```

Response:
```json
{
  "jsonrpc": "2.0",
  "result": {
    "artifacts": [{
      "parts": [{"kind": "text", "text": "There are 30 namespaces in this cluster."}]
    }]
  }
}
```

### Streaming (`message/stream`)

```bash
curl -N -X POST http://localhost:18080/ \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "method": "message/stream",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"kind": "text", "text": "List all pods in default namespace"}]
      }
    }
  }'
```

---

## K8s Tools

24 built-in tools + `knowledge_search` (RAG), verified against K8s v1.31.12.

### Core Resources

| Tool | Verb | Returns |
|------|------|---------|
| `pod_list` | list | Name, status, node, IP, restarts |
| `pod_get` | get | Containers, conditions, events, labels |
| `pod_logs` | get | Log output (configurable tail + container) |
| `pod_delete` | delete | Confirmation message |
| `deployment_list` | list | Ready/available replicas |
| `deployment_get` | get | Replicas, image, strategy, conditions |
| `deployment_scale` | update | Scale confirmation |
| `service_list` | list | Type, cluster IP, ports |
| `service_get` | get | Ports, selector, external IP |
| `namespace_list` | list | Name, status, age |
| `namespace_get` | get | Labels |
| `namespace_set` | — | Switch active namespace |
| `event_list` | list | Filter by type (Normal/Warning) |

### Networking & Autoscaling

| Tool | Verb | Returns |
|------|------|---------|
| `ingress_list` | list | Hosts, ingress class |
| `ingress_get` | get | TLS, routing rules, backend services |
| `hpa_list` | list | Scale target, min/max replicas, current CPU% |
| `hpa_get` | get | Metric specs, current load, conditions |

### Configuration & Secrets

| Tool | Verb | Returns |
|------|------|---------|
| `configmap_list` | list | Key count |
| `configmap_get` | get | Keys, data (>256 chars truncated), binary keys |
| `secret_list` | list | Type, key count (**values never exposed**) |
| `secret_get` | get | Key names, type, labels (**values never exposed**) |

### Generic & Meta

| Tool | Verb | Returns |
|------|------|---------|
| `resource_list` | list | Any K8s/CRD resource via dynamic client |
| `resource_get` | get | Full unstructured object |
| `cluster_info` | get | Version, API groups, node/namespace count |

---

## Skills

Reusable workflow modules in `skills/` (SKILL.md format):

| Skill | Purpose | Example Use |
|-------|---------|-------------|
| `k8s-diagnose` | CrashLoopBackOff, OOMKilled, scheduling failures | "Why is my pod crashing?" |
| `k8s-deploy` | Rolling updates, rollbacks, scaling, health checks | "Scale nginx to 5 replicas" |
| `k8s-security` | RBAC audit, privileged containers, exposed services | "Find privileged pods" |

---

## Plugins

| Plugin | Hook Point | Behavior |
|--------|-----------|----------|
| `audit` | `AfterTool` | Logs every tool invocation with timestamp/result/error |
| `ratelimit` | `BeforeAgent` | Per-session request counter (100 req/session) |
| `logging` | `BeforeAgent`+`AfterAgent` | Agent lifecycle events |

Custom plugin example:

```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "my-plugin" }

func (p *MyPlugin) Register(r *plugin.Registry) {
    r.BeforeAgent(func(ctx context.Context, args *agent.BeforeAgentArgs) (*agent.BeforeAgentResult, error) {
        slog.Info("custom logic before agent runs")
        return &agent.BeforeAgentResult{Context: ctx}, nil
    })
}
```

---

## Configuration

Full reference — `config/kube-agents.yaml`:

```yaml
server:
  http:
    enabled: true
    port: 8080
  grpc:
    enabled: false
    port: 9090
  a2a:
    enabled: false
    host: "kube-agents.default.svc.cluster.local:18080"

model:
  provider: deepseek
  name: deepseek-chat
  apiKeyEnv: DEEPSEEK_API_KEY
  baseUrl: ""

auth:
  mode: serviceaccount          # serviceaccount | jwt
  jwt:
    issuer: kube-agents
    secretEnv: JWT_SECRET

session:
  backend: memory               # memory | redis
  redis:
    addr: "redis:6379"
    passwordEnv: REDIS_PASSWORD
    db: 0

memory:
  backend: memory
  redis:
    addr: "redis:6379"
    passwordEnv: REDIS_PASSWORD
    db: 1

knowledge:
  sources:
    - type: file
      path: ./docs/k8s-reference/
    - type: url
      path: https://kubernetes.io/docs/

mcpServers:
  - name: kubectl
    transport: stdio
    command: kubectl
    args: ["mcp"]

plugins:
  - audit
  - ratelimit
  - logging

skillsDir: ./skills/
```

---

## Architecture

```
HTTP/gRPC/CLI/A2A
       │
       ▼
┌──────────────────┐
│   Auth Plugin    │  ← TokenReview + SubjectAccessReview (K8s native)
└──────┬───────────┘
       │
       ▼
┌──────────────────┐
│     Runner       │  ← Session Service + Memory Service + Plugins
└──────┬───────────┘
       │
       ▼
┌──────────────────┐
│  K8s LLMAgent    │  ← System prompt + 24 K8s tools + skills + knowledge
└──────┬───────────┘
       │
       ├──→ K8s tools     (client-go → K8s API Server)
       ├──→ MCP tools     (external stdio/SSE servers)
       ├──→ Skills        (k8s-diagnose / deploy / security)
       └──→ Knowledge     (RAG: semantic search over K8s docs)
```

### Request Lifecycle

1. Request arrives via **CLI** / **HTTP** / **A2A**
2. **Auth Plugin** validates token (TokenReview) and injects user identity into session state
3. **Runner** creates/looks up session, attaches memory context, applies plugins
4. **LLMAgent** receives the message with system prompt + available tools
5. LLM decides which tool(s) to call based on user intent
6. **Tool** executes via client-go against K8s API; audit plugin logs the invocation
7. Response streams back through Runner → Session persistence → Client

---

## Project Layout

```
kube-agents/
├── cmd/
│   ├── kube-agents/                   # Main binary
│   │   ├── main.go                    # Entry point
│   │   └── app/
│   │       ├── cmd.go                 # Cobra commands (serve/chat/version)
│   │       └── options/options.go     # CLI flags + validation
│   └── k8s-verify/                    # K8s tools verification utility
├── pkg/
│   ├── agent/                         # Agent factory + calculator tool
│   ├── auth/                          # Auth plugin (TokenReview + SubjectAccessReview)
│   ├── config/                        # YAML config structs + loader
│   ├── k8s/
│   │   ├── client.go                  # client-go factory (in-cluster + kubeconfig)
│   │   └── tools/                     # 24 K8s function tools (7 files)
│   ├── knowledge/                     # RAG knowledge store
│   ├── mcp/                           # MCP tool registry
│   ├── memory/                        # Memory service factory
│   ├── plugin/                        # Built-in plugins (audit/ratelimit/logging)
│   ├── server/                        # HTTP + A2A server setup
│   ├── session/                       # Session service factory
│   └── skill/                         # Skill registry (SKILL.md loader)
├── internal/event/                    # Streaming event processor (reasoning buffer)
├── version/                           # Version info (ldflags injected)
├── utils/log/                         # slog initialization + lumberjack rotation
├── skills/                            # SKILL.md workflow modules (3 skills)
├── config/                            # Default config file
├── deployments/                       # K8s manifests (deployment, svc, configmap, rbac)
├── build/                             # Multi-stage Dockerfile
└── Makefile                           # Build, test, docker targets
```

---

## Deployment

### Docker

```bash
make docker-build IMG=your-registry/kube-agents:0.1.0
make docker-push  IMG=your-registry/kube-agents:0.1.0
make docker-buildx IMG=your-registry/kube-agents:0.1.0   # Multi-arch
```

### Kubernetes

```bash
kubectl apply -f deployments/
```

Creates: ServiceAccount → ClusterRole → ClusterRoleBinding → ConfigMap → Deployment → Service

### RBAC

| API Group | Resources | Verbs |
|-----------|-----------|-------|
| `""` (core) | pods, pods/log, services, namespaces, events, nodes | get, list, watch |
| `""` (core) | pods, services | create, update, delete |
| `apps` | deployments, deployments/scale, replicasets | get, list, watch, update |
| `authentication.k8s.io` | tokenreviews | create |
| `authorization.k8s.io` | subjectaccessreviews | create |
| `networking.k8s.io`, `batch`, `apiextensions.k8s.io` | `*` | get, list |

---

## Verification

A standalone utility tests all tools against a real cluster:

```bash
KUBECONFIG=$HOME/.kube/config go run ./cmd/k8s-verify/
```

Output:
```
Cluster connected (namespace=default)
Total tools: 24

PASS cluster_info         (version: v1.31.12)
PASS namespace_list       (30 items)
PASS pod_list             (7 items)
PASS deployment_list      (4 items)
PASS service_list         (4 items)
PASS ingress_list         (2 items)
PASS configmap_list       (6 items)
PASS secret_list          (2 items)
PASS pod_get              (name: ba-0223-app-ccp-r5t67)
PASS deployment_get       (name: ba-0223-app-ccp)
PASS service_get          (name: dual-stack-svc)
PASS ingress_get          (name: dual-stack-ing)
...

Results: 17 passed, 0 failed
```

---

## Development

```bash
make fmt          # go fmt ./...
make vet          # go vet ./...
make build        # Build binary → bin/kube-agents
make run          # go run ./cmd/kube-agents/
make test         # go test -race ./...
make test-cover   # go test -cover -coverprofile=coverage.out ./...
```

## License

Apache License 2.0
