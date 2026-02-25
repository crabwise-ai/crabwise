# Commandments

Commandments are policy rules that run against every audit event. When a rule’s **match** conditions all succeed, the rule is **triggered** and its **enforcement** and **message** are recorded (and optionally **redact** sensitive content).

The rules file is YAML. By default it lives at `~/.config/crabwise/commandments.yaml` (see main [Config](../../README.md#config)); you can override the path in `config.yaml` under `commandments.file`.

## Current default behavior

- Preventative blocking is enabled by default in shipped config (`adapters.proxy.enabled: true`).
- The default commandments file includes an enabled block rule:
  - `no-destructive-commands` (`enforcement: block`)
- Blocking is preventative only for requests that pass through the proxy listener (`127.0.0.1:9119` by default).
- On observe-only adapters (log watcher), `block` is recorded as `warned` because the action already happened.

## File layout

```yaml
version: "1"
commandments:
  - name: my-rule
    description: Optional short description
    enforcement: warn          # or block
    priority: 100             # higher = evaluated first
    enabled: true             # optional, default true
    match:                    # all conditions must match
      action_type: command_execution
      arguments:
        type: regex
        pattern: "git push.*main"
    redact: false             # optional, default false
    message: "Direct push to main"
```

- **`version`** — Must be `"1"`.
- **`commandments`** — List of rules. Order in the file does not affect evaluation order; that is determined by **priority** (highest first). Up to 100 rules; total compiled patterns (regex/glob/list entries) capped at 200.

## Rule fields

| Field | Required | Description |
|-------|----------|-------------|
| **name** | Yes | Unique rule id (used in audit output and `commandments list`). |
| **description** | No | Human-readable summary. |
| **enforcement** | Yes | `warn` or `block`. Stored with the event; on enforce-capable adapters (proxy), `block` prevents the action before it reaches upstream. On observe-only adapters (log watcher), `block` downgrades to `warn`. |
| **priority** | No | Integer; higher runs first. Default 0. Ties broken by name. |
| **enabled** | No | If `false`, rule is skipped. Default `true`. |
| **match** | Yes | Map of event field → condition. **All** conditions must match for the rule to trigger. |
| **redact** | No | If `true`, event payload may be redacted when this rule triggers. Default `false`. |
| **message** | No | Short message stored when the rule triggers. |

## Match fields (event attributes)

You can match on any of these audit event fields. The value is compared with the matcher you specify (exact, regex, glob, etc.).

| Field | Description |
|-------|-------------|
| `id` | Event id |
| `agent_id` | Agent identifier |
| `action_type` | e.g. `command_execution`, `file_access`, `ai_request` |
| `action` | Action name (e.g. tool or command name) |
| `arguments` | Arguments payload (often command line or JSON) |
| `session_id` | Session id |
| `working_dir` | Working directory |
| `provider` | AI provider (e.g. openai) |
| `model` | Model name |
| `tool_category` | Tool category from registry (e.g. `shell`, `file.read`) |
| `tool_effect` | e.g. `execute`, `read` |
| `tool_name` | Tool name |
| `adapter_type` | Adapter type |
| `adapter_id` | Adapter id |
| `outcome` | Event outcome |
| `input_tokens` | Token count (numeric) |
| `output_tokens` | Token count (numeric) |
| `cost_usd` | Cost (numeric) |
| `agent_pid` | Agent process id (numeric) |

If an event has no value for a matched field (e.g. empty string or missing), that condition fails and the rule does not trigger.

## Match condition types

Each **match** entry is a field name plus a **condition**. A condition can be:

1. **Scalar (exact)** — a plain string: the field must equal that value.
2. **Object** — `type` plus type-specific keys (see below).

### `exact`

String must equal the given value.

```yaml
match:
  action_type: command_execution   # shorthand: exact
  action_type:                     # explicit
    type: exact
    pattern: command_execution
```

- **pattern** (required): exact string.

### `regex`

String must match the regex (full string or substring per Go `regexp`).

```yaml
arguments:
  type: regex
  pattern: "rm\\s+-rf|dd\\s+if="
```

- **pattern** (required): valid Go regex. Max length 1024 chars.

### `glob`

String is matched with [doublestar](https://github.com/bmatcuk/doublestar) globs (`**` for any path segment).

```yaml
arguments:
  type: glob
  patterns:
    - "**/.env**"
    - "**/*.pem**"
```

- **pattern** — single pattern, or
- **patterns** — list of patterns (any match succeeds).

### `numeric`

Value is parsed as a number and compared with **op** and **value**.

```yaml
input_tokens:
  type: numeric
  op: gte
  value: 1000
```

- **op** (required): `gt`, `lt`, `eq`, `gte`, `lte`
- **value** (required): number (int or float)

### `list`

Value is checked for membership (or non-membership) in a list.

```yaml
model:
  type: list
  op: not_in
  values:
    - gpt-4o
    - claude-sonnet-4-5-20250929
```

- **op** (required): `in` or `not_in`
- **values** (required): list of strings

## Example rules

**Destructive shell commands (regex on arguments):**

```yaml
- name: no-destructive-commands
  description: Block destructive shell commands
  enforcement: block
  priority: 100
  match:
    tool_category: shell
    tool_effect: execute
    arguments:
      type: regex
      pattern: "rm\\s+-rf|mkfs|dd\\s+if="
  message: "Destructive command detected"
```

**Credential file access (glob on arguments):**

```yaml
- name: protect-credentials
  enforcement: warn
  match:
    tool_category:
      type: list
      op: in
      values:
        - file.read
        - file.search
    arguments:
      type: glob
      patterns:
        - "**/.env**"
        - "**/*.pem**"
  redact: true
  message: "Credential file access detected"
```

**Approved models only (list not_in):**

```yaml
- name: approved-models
  enforcement: warn
  match:
    action_type: ai_request
    model:
      type: list
      op: not_in
      values:
        - claude-sonnet-4-5-20250929
        - gpt-4o
        - gpt-4o-mini
  message: "Model is not in approved list"
```

**High token usage (numeric):**

```yaml
- name: high-input-tokens
  enforcement: warn
  match:
    action_type: ai_request
    input_tokens:
      type: numeric
      op: gte
      value: 10000
  message: "Large input token usage"
```

### Block commandments

Use `enforcement: block` to prevent actions before they execute. On the proxy adapter, a triggered block rule immediately returns HTTP 403 to the client and the request is never forwarded upstream. The blocked event is recorded in the audit trail with `outcome=blocked`.

On observe-only adapters (log watcher), `block` downgrades to `warn` because the action has already occurred by the time the log is parsed. The event is still recorded with the triggered rule.

For block enforcement to take effect preventatively, traffic must go through the proxy and at least one enabled commandment must use `enforcement: block`. The default install already satisfies this (`adapters.proxy.enabled: true` plus `no-destructive-commands` with `enforcement: block`).

**Block direct push to main/master:**

```yaml
- name: block-push-main
  description: Block pushing directly to main or master
  enforcement: block
  priority: 200
  match:
    tool_category: shell
    tool_effect: execute
    arguments:
      type: regex
      pattern: "git push.*(?:origin\\s+)?(?:main|master)(?:\\s|$)"
  message: "Blocked: direct push to main/master"
```

**Block unapproved AI models:**

```yaml
- name: block-unapproved-models
  description: Block AI requests for models not on the approved list
  enforcement: block
  priority: 150
  match:
    action_type: ai_request
    model:
      type: list
      op: not_in
      values:
        - gpt-4o
        - gpt-4o-mini
        - claude-sonnet-4-5-20250929
  message: "Blocked: model not in approved list"
```

**Block credential file reads (with redaction):**

```yaml
- name: block-credential-access
  description: Block reads of credential files
  enforcement: block
  priority: 180
  match:
    tool_category:
      type: list
      op: in
      values:
        - file.read
        - file.search
    arguments:
      type: glob
      patterns:
        - "**/.env"
        - "**/.env.*"
        - "**/*.pem"
        - "**/*credentials*"
  redact: true
  message: "Blocked: credential file access"
```

You can query blocked events in the audit trail with `crabwise audit --triggered --outcome blocked` (when outcome is set from enforcement).

## Reloading

After editing the YAML file, reload rules without restarting the daemon:

```bash
crabwise commandments reload
```

## Testing a rule

You can test how rules evaluate against a sample event (JSON):

```bash
crabwise commandments test '{"action_type":"command_execution","action":"Bash","arguments":"git push origin main"}'
```

This runs the current rules against that event and prints which rules were evaluated and which triggered.
