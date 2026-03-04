# Crabwise System Architecture

```
  +-----------------------------------------+
  |           Developer Workstation          |
  |                                          |
  |  +------------+    +------------------+ |
  |  | Claude Code|    |  Other AI Agent  | |
  |  | (agent)    |    |  (OpenAI client) | |
  |  +-----+------+    +--------+---------+ |
  |        |                    |            |
  |        | JSONL logs         | HTTP       |
  |        | ~/.claude/         | (proxied)  |
  |        | projects/.../      |            |
  |        | sessions/          |            |
  |        |           +--------+            |
  |        |           |                     |
  |        v           v                     |
  |  +-----+-----------+------------------+ |
  |  |         crabwise daemon             | |
  |  |                                     | |
  |  |  +---------------+  +-----------+  | |
  |  |  |  Log Watcher  |  |   HTTP    |  | |
  |  |  |  Adapter      |  |   Proxy   |  | |
  |  |  | (JSONL parser)|  | (reverse) |  | |
  |  |  +-------+-------+  +-----+-----+  | |
  |  |          |                |         | |
  |  |          |           +----+----+    | |
  |  |          |           | Buffer  |    | |
  |  |          |           | SSE/JSON|    | |
  |  |          |           +----+----+    | |
  |  |          |                |         | |
  |  |          |     +----------+         | |
  |  |          |     | Extract  |         | |
  |  |          |     | Tool Use |         | |
  |  |          |     | Blocks   |         | |
  |  |          |     +----+-----+         | |
  |  |          |          |               | |
  |  |     +----+----------+               | |
  |  |     |                               | |
  |  |     v                               | |
  |  |  +--+---------------------+         | |
  |  |  |   Commandment Engine   |         | |
  |  |  |   (YAML rules)         |         | |
  |  |  |   Evaluate() -> block/ |         | |
  |  |  |   warn / pass          |         | |
  |  |  +----------+-------------+         | |
  |  |             |                       | |
  |  |     +-------+-------+               | |
  |  |     |               |               | |
  |  |     v               v               | |
  |  |  block/warn      pass               | |
  |  |  403/emit        200 fwd            | |
  |  |     |               |               | |
  |  |     +-------+-------+               | |
  |  |             |                       | |
  |  |             v                       | |
  |  |  +----------+-----------+           | |
  |  |  |   Event Pipeline     |           | |
  |  |  |   bounded queue      |           | |
  |  |  |   hash chain         |           | |
  |  |  |   batch writes       |           | |
  |  |  +----------+-----------+           | |
  |  |             |                       | |
  |  |             v                       | |
  |  |  +----------+-----------+           | |
  |  |  |   SQLite store       |           | |
  |  |  |   (WAL mode)         |           | |
  |  |  |   audit_events table |           | |
  |  |  +----------------------+           | |
  |  |                                     | |
  |  |  +----------------------------------+ |
  |  |  |   IPC Server (Unix socket)       | |
  |  |  |   JSON-RPC 2.0 + SO_PEERCRED    | |
  |  |  |   methods:                       | |
  |  |  |     status, agents, audit        | |
  |  |  |     verify, gate.evaluate        | |
  |  |  +--------+-------------------------+ |
  |  |           |                           |
  |  +-----------+---------------------------+
  |              |                            |
  |    +---------+----------+                 |
  |    |                    |                 |
  |    v                    v                 |
  | +------+         +------------+           |
  | | cwcl |         | hooks /    |           |
  | | CLI  |         | ext. tools |           |
  | | cmds:|         | gate.eval  |           |
  | | status         | pre-tool   |           |
  | | agents         | check      |           |
  | | audit          +------------+           |
  | | verify                                  |
  | +------+                                  |
  +-----------------------------------------+
                    |
                    | HTTPS (forwarded by proxy)
                    v
        +-----------+-----------+
        |   LLM Provider API    |
        |   (OpenAI-compatible) |
        |   /v1/chat/completions|
        +-----------------------+
```

## Component Responsibilities

```
  Component              | Package                        | Role
  -----------------------+--------------------------------+------------------------------
  Log Watcher Adapter    | internal/adapter/logwatcher    | tail JSONL, parse CC events
  HTTP Proxy             | internal/adapter/proxy         | intercept LLM calls
  Commandment Engine     | internal/commandments          | evaluate YAML policy rules
  Event Pipeline         | internal/queue + audit/logger  | bounded queue, hash chain
  SQLite Store           | internal/store                 | durable audit log
  IPC Server             | internal/ipc                   | JSON-RPC 2.0 Unix socket
  Daemon Orchestrator    | internal/daemon                | wire all components
  CLI                    | cmd/cwcl                       | human interface
```

## Data Flow Summary

```
  Observe path (passive):
    CC logs -> LogWatcher -> queue -> hash chain -> SQLite

  Enforce path (active, proxy):
    Agent -> Proxy -> Provider (LLM)
                   <- response buffered
                   <- tool blocks extracted
                   <- commandments evaluated
    blocked -> 403 + AuditEvent(blocked) -> queue -> SQLite
    passed  -> 200 + response forwarded to agent

  Enforce path (active, hook/IPC):
    hook -> gate.evaluate (IPC) -> commandments evaluated
    blocked -> AuditEvent(blocked) -> queue -> SQLite -> "block" returned
    passed  -> "pass" returned (no emit)

  Query path:
    cwcl audit / agents / verify -> IPC -> SQLite -> stdout
```
