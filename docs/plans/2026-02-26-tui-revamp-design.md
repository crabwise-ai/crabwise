# Crabwise TUI Revamp — Design Plan

## Context

Crabwise currently uses plain `fmt.Printf` output for all CLI commands except `watch`, which has a basic Bubble Tea TUI. This plan revamps every command with a cohesive Bubble Tea + Bubbles + Lipgloss treatment — modular per-command TUIs connected through a shared crustacean-futurist theme system.

**Goal:** Each `crabwise` subcommand feels like part of the same polished product. Interactive commands get full TUI treatment (tables, live updates, keyboard nav). Non-interactive commands get styled output with animation. Everything shares the same visual DNA.

---

## Brand & Theme

### Color Palette

| Role | Hex | Usage |
|------|-----|-------|
| Crab Orange (primary) | `#E05A3A` | Headings, borders, active elements, crab ASCII |
| Warm Gold (secondary) | `#E8C785` | Accents, highlights, selected rows, wave animation crest |
| Deep Ocean | `#1A2B3D` | Backgrounds on dark terminals (optional adaptive) |
| Seafoam | `#5FBFAD` | Success states, healthy indicators |
| Coral Red | `#D94F4F` | Blocked/error states |
| Drift Gray | `#6B7B8D` | Muted text, disabled states, timestamps |
| Shell White | `#F0EDE6` | Primary text on dark backgrounds |

### Animated ASCII Banner

The existing crab art gets a wave animation — a color sweep that washes across the characters on startup:

```
▄█▀      ▀█▄   Crabwise AI v0.x.x
█▄█ ▄  ▄ █▄█   Local-first AI agent governance
█▀ ▄█▄▄█▄ ▀█   ─═══════════════════════════─
▀██████████▀    https://crabwise.ai
```

**Wave effect:** A 3-character-wide gradient sweeps left-to-right across the crab art over ~0.8s:
- Leading edge: Drift Gray (dim, approaching)
- Crest: Warm Gold `#E8C785` (bright peak)
- Trailing edge: Crab Orange `#E05A3A` (settles to final color)

Runs once per command invocation. Banner settles to static Crab Orange after the wave completes. Skipped when stdout is not a TTY or `--plain` is set.

### Nautical Motifs

**Spinners** (custom frame sets for `bubbles/spinner`):

| Name | Frames | Usage |
|------|--------|-------|
| Tide | `░ ▒ ▓ █ ▓ ▒ ░` | General loading (init, start, stop) |
| Bubbles | `○ ◎ ◉ ● ◉ ◎ ○` | Connecting, processing |
| Drift | `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏` | Fallback (standard braille, widely supported) |

**Status indicators:**

| State | Icon | Color |
|-------|------|-------|
| Running / Active | `◉` | Seafoam |
| Stopped / Inactive | `○` | Drift Gray |
| Warning / Triggered | `⚠` | Crab Orange |
| Blocked | `✖` | Coral Red |
| Success | `✓` | Seafoam |
| Connecting | `≋` | Warm Gold |

**Section dividers:** `─═─═─═─═─` in Drift Gray. Clean, nautical rope feel.

**Table chrome:** Rounded lipgloss borders in Drift Gray, header row in Crab Orange bold, selected row background highlighted with Warm Gold.

---

## Architecture

### Package Structure

```
internal/
  tui/
    theme.go          # Color constants, lipgloss style presets, adaptive dark/light
    banner.go         # Animated ASCII crab banner (tea.Model)
    spinner.go        # Custom spinner frame sets (Tide, Bubbles, Drift)
    table.go          # Styled table wrapper (bubbles/table + theme)
    statusbar.go      # Reusable bottom bar: key hints + status info
    panel.go          # Bordered output panel for non-interactive results
    format.go         # Shared formatters: timestamps, durations, costs, truncation
```

All commands import `internal/tui` for styling. Each command file in `internal/cli/` continues to own its own Bubble Tea model (if interactive) or rendering logic (if non-interactive). No god-model — each command is self-contained but visually consistent.

### TTY Detection & `--plain` Flag

Every command that produces styled output respects:

1. **Auto-detect:** If stdout is not a TTY, output plain text (pipe-safe). Animations and colors disabled.
2. **`--plain` flag:** Explicit override on root command, inherited by all subcommands. Forces plain text regardless of TTY.
3. **`NO_COLOR` env var:** Standard `NO_COLOR` convention strips colors but keeps layout.

Interactive TUI commands (`watch`, `audit`, `agents`, `status`, `commandments list`) require a TTY. If no TTY is detected, they fall back to their existing non-interactive output (or a sensible static equivalent).

---

## Command Designs

### Non-Interactive Commands (Styled Output)

These commands run, produce styled output, and exit. They get the shared banner (abbreviated single-line for subcommands), spinners during operations, and styled result panels.

#### `crabwise version`

```
🦀 Crabwise AI v0.4.2 (darwin/arm64)
```

Single-line, Crab Orange `🦀`, version in Shell White. Animated wave on the `🦀` if TTY.

#### `crabwise init`

Animated Tide spinner while writing files and generating CA:

```
▄█▀      ▀█▄   Crabwise AI v0.4.2
█▄█ ▄  ▄ █▄█   Initializing...
█▀ ▄█▄▄█▄ ▀█
▀██████████▀

  ✓ Config written         ~/.config/crabwise/config.yaml
  ✓ Commandments written   ~/.config/crabwise/commandments.yaml
  ✓ Tool registry written  ~/.config/crabwise/tool_registry.yaml
  ✓ OpenAI mapping written ~/.config/crabwise/proxy_mappings/openai.yaml
  ✓ CA certificate         ~/.local/share/crabwise/ca.crt

  Trust the CA:
  ╭──────────────────────────────────────────────────────────────╮
  │ sudo security add-trusted-cert -d -r trustRoot \            │
  │   -k /Library/Keychains/System.keychain ~/.local/.../ca.crt │
  ╰──────────────────────────────────────────────────────────────╯
```

Checkmarks appear sequentially with a brief delay. Existing files show `○ Already exists` in Drift Gray.

#### `crabwise start`

```
▄█▀      ▀█▄   Crabwise AI v0.4.2
█▄█ ▄  ▄ █▄█   Starting daemon...
█▀ ▄█▄▄█▄ ▀█
▀██████████▀

  ◉ Daemon running (pid 48291)
  ◉ Log watcher active
  ◉ Proxy listening on 127.0.0.1:9119
  ○ OTel export disabled

  Watching: ~/.claude/projects/ ~/.codex/sessions/
```

Since `start` runs the daemon in foreground, the banner + status lines render once at startup, then the daemon takes over stdout for log output. The TUI is a transient intro, not persistent.

#### `crabwise stop`

Bubbles spinner while waiting for process exit:

```
  ◎ Stopping daemon (pid 48291)...
  ✓ Daemon stopped
```

#### `crabwise classify <tool>`

```
  ╭─ Classification ──────────────────────╮
  │ Tool:      Bash                       │
  │ Provider:  anthropic                  │
  │ Category:  shell                      │
  │ Effect:    execute                    │
  │ Source:    exact_match                │
  │ Taxonomy:  v1                         │
  ╰───────────────────────────────────────╯
```

Bordered panel in Drift Gray, field names in Crab Orange, values in Shell White.

#### `crabwise commandments test <json>`

```
  Evaluated: 4 commandments

  ⚠ no-destructive-commands (warn)
    "Do not run rm -rf or similar destructive commands"

  No blocks triggered.
```

Warning entries in Crab Orange with `⚠`, blocks in Coral Red with `✖`. Clean result when nothing triggers.

#### `crabwise commandments reload`

```
  ◎ Reloading commandments...
  ✓ Loaded 4 rules
```

#### `crabwise env` / `crabwise wrap`

These remain minimal. `env` prints shell-eval lines (no styling — it's meant to be `eval`'d). `wrap` execs directly. No changes except consistent error formatting with the theme.

---

### Interactive Commands (Full TUI)

These are persistent Bubble Tea programs with keyboard navigation, live updates, and the full theme treatment.

#### `crabwise status` — Daemon Dashboard

Full-screen dashboard with live-updating metrics. Polls daemon via IPC every 3s.

```
▄█▀      ▀█▄  Crabwise AI v0.4.2
█▄█ ▄  ▄ █▄█  ─═─═─═─═─═─═─═─═─
█▀ ▄█▄▄█▄ ▀█
▀██████████▀

  ◉ Daemon        running (pid 48291)    Uptime: 2h 14m
  ◉ Log watcher   active                 Agents: 2
  ◉ Proxy         127.0.0.1:9119         Reqs: 847
  ○ OTel          disabled

 ─═─ Queue ─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─

  Depth:    12 / 10,000            Dropped: 0
  ▓▓░░░░░░░░░░░░░░░░░░░░░░░░░░░░  0.1%

 ─═─ Proxy ─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─

  Total:    847      Blocked: 3      Errors: 0
  Map degraded: 0   Unclassified tools: 1

 ─═─ Commandments ─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─═─

  Rules: 4 loaded    Triggers/min: 2

  r refresh  q quit                                   3s
```

**Components:** `bubbles/progress` for queue gauge, `bubbles/help` for key hints, custom status rows, `tea.Tick` for polling.

**Keys:** `r` manual refresh, `q` quit.

#### `crabwise agents` — Agent Explorer

Interactive table of discovered agents with live updates.

```
▄█▀      ▀█▄  Agents
█▄█ ▄  ▄ █▄█
█▀ ▄█▄▄█▄ ▀█
▀██████████▀

  ┌─────────────────────────────────────────────────────┐
  │ STATUS  ID                TYPE          PID         │
  ├─────────────────────────────────────────────────────┤
  │ ◉       claude-a8f2       claude_code   48312       │
  │ ◉       codex-1bc9        codex_cli     48456       │
  │ ○       claude-f012       claude_code   —           │
  └─────────────────────────────────────────────────────┘

  2 active, 1 inactive

  ↑↓ navigate  enter detail  r refresh  q quit
```

**Components:** `bubbles/table` with theme styling, status icons in the first column. Detail view shows recent events for selected agent (viewport overlay).

**Keys:** `↑/↓` navigate, `enter` detail view, `esc` back, `r` refresh, `q` quit.

#### `crabwise audit` — Audit Explorer

The most complex TUI. Interactive table with filtering, sorting, pagination, detail view, and cost mode.

```
▄█▀      ▀█▄  Audit Trail                    847 events
█▄█ ▄  ▄ █▄█
█▀ ▄█▄▄█▄ ▀█  Filter: ▏                       Page 1/17
▀██████████▀

  ┌──────────────────────────────────────────────────────────────────────────┐
  │ TIME     AGENT          ACTION TYPE         ACTION      OUTCOME   COST  │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ 14:32:01 claude-a8f2    tool_call           Bash        ✓ success       │
  │ 14:31:58 claude-a8f2    ai_request          chat        ✓ success $0.02 │
  │ 14:31:45 codex-1bc9     tool_call           Write       ✓ success       │
  │ 14:31:30 claude-a8f2    command_execution   rm -rf /tmp ✖ blocked       │
  │ 14:31:12 claude-a8f2    file_access         .env        ⚠ warned        │
  │ ...                                                                     │
  └──────────────────────────────────────────────────────────────────────────┘

  ↑↓ navigate  / filter  enter detail  c cost view  v verify  q quit
```

**Filter mode:** `/` opens `bubbles/textinput` overlay. Filter applies across agent, action, outcome fields. Supports quick filters: `o:blocked` `o:warned` `a:tool_call` `agent:claude`.

**Detail view:** `enter` opens a bordered panel showing full event JSON with syntax-highlighted fields. `esc` returns to table.

**Cost view:** `c` toggles to cost summary mode (grouped by day/agent/model with totals). Same table component, different data source.

**Integrity verification:** `v` triggers `audit.verify` via IPC, shows progress spinner then result in a panel overlay.

**Pagination:** `n/p` or `pgup/pgdn` for pages. Page size based on terminal height.

**Components:** `bubbles/table`, `bubbles/textinput`, `bubbles/viewport` (detail view), `bubbles/paginator`, `bubbles/help`.

#### `crabwise watch` — Live Event Stream (Revamp)

Upgrade the existing `watch_tui.go` with the full theme system. The current basic implementation becomes a polished dashboard.

```
▄█▀      ▀█▄  Watch                          ◉ connected
█▄█ ▄  ▄ █▄█  ─═─═─═─═─═─═─═─═─
█▀ ▄█▄▄█▄ ▀█  Queue: 12  Dropped: 0  Triggers/min: 2
▀██████████▀   Uptime: 2h 14m

  14:32:01 [claude-a8f2] tool_call           Bash       {"command":"ls -la"}
  14:31:58 [claude-a8f2] ai_request          chat       gpt-4o ($0.023)
  14:31:45 [codex-1bc9]  tool_call           Write      src/main.go
⚠ 14:31:30 [claude-a8f2] command_execution   rm -rf     WARNED: no-destructive-cmds
✖ 14:31:12 [claude-a8f2] file_access         .env       BLOCKED: no-credential-access
  14:31:00 [codex-1bc9]  ai_request          chat       gpt-4o-mini ($0.001)
  14:30:45 [claude-a8f2] tool_call           Read       internal/cli/root.go

  / filter  esc clear  q quit
```

**Changes from current:**
- Themed banner + status strip replaces plain header
- Connection status indicator (◉ connected / ○ reconnecting / ✖ disconnected)
- Event lines use lipgloss styling instead of raw ANSI
- Warn/block lines get full-width background highlight, not just prefix color
- Cost inline on ai_request events
- Smoother reconnection UX with Bubbles spinner during reconnect

**Components:** Reuse existing `watchModel` structure, replace `View()` with themed rendering, swap text filter for `bubbles/textinput` (already partially done).

#### `crabwise commandments list` — Rules Explorer

```
▄█▀      ▀█▄  Commandments
█▄█ ▄  ▄ █▄█
█▀ ▄█▄▄█▄ ▀█  4 rules loaded
▀██████████▀

  ┌──────────────────────────────────────────────────────────────┐
  │ NAME                             ENFORCEMENT  PRI  ENABLED  │
  ├──────────────────────────────────────────────────────────────┤
  │ no-destructive-commands          block        100  ✓        │
  │ no-credential-access             warn         90   ✓        │
  │ approved-models-only             block        80   ✓        │
  │ no-git-push-main                 warn         70   ✓        │
  └──────────────────────────────────────────────────────────────┘

  ↑↓ navigate  enter detail  r reload  q quit
```

**Detail view:** Shows full commandment YAML definition, match patterns, and recent trigger history.

**Reload:** `r` triggers `commandments.reload` via IPC, shows spinner then updated table.

---

## Milestones

### T0 — Theme Foundation & Shared Components

**Deliverable:** `internal/tui/` package with all shared components. A `tui-demo` build tag test that renders each component for visual verification.

| Item | Detail |
|------|--------|
| `theme.go` | Color palette constants, lipgloss presets (heading, body, muted, success, warning, error, selected), adaptive rendering |
| `banner.go` | `BannerModel` (tea.Model) — animated wave sweep, static fallback, compact single-line variant |
| `spinner.go` | Tide, Bubbles, Drift spinner frame sets as `spinner.Spinner` values |
| `table.go` | `NewStyledTable()` wrapping `bubbles/table` with themed header, borders, row highlight, status icon column helper |
| `statusbar.go` | `StatusBar` — bottom bar with left-aligned key hints + right-aligned status text |
| `panel.go` | `RenderPanel(title, body)` — bordered lipgloss box for non-interactive results |
| `format.go` | `FormatTimestamp`, `FormatDuration`, `FormatCost`, `Truncate`, `StatusIcon` |
| Root `--plain` flag | Global flag on root command, TTY auto-detection, `NO_COLOR` support |

**Exit gates:**
- All components render correctly in 80-col and 120-col terminals
- Plain mode produces no ANSI escape codes
- Wave animation completes in < 1s, no visual artifacts
- Components are independently testable via `tea.Model` unit tests

### T1 — Non-Interactive Command Styling

**Deliverable:** `version`, `init`, `start`, `stop`, `classify`, `commandments test`, `commandments reload` use themed output.

| Item | Detail |
|------|--------|
| `version` | Single-line with crab emoji + orange styling |
| `init` | Banner + sequential checkmarks with Tide spinner during CA generation |
| `start` | Banner + status lines at daemon launch |
| `stop` | Bubbles spinner while waiting, checkmark on success |
| `classify` | Bordered panel output |
| `commandments test` | Themed trigger results with warn/block icons |
| `commandments reload` | Spinner + checkmark |
| Error formatting | All error output uses consistent red panel styling |

**Exit gates:**
- All commands produce valid plain-text output when piped (`crabwise status | cat`)
- Spinner animation is smooth (no flicker, proper cursor handling)
- Existing test coverage continues to pass
- `env` and `wrap` remain unstyled (they're machine-consumed)

### T2 — Status Dashboard

**Deliverable:** `crabwise status` is a full interactive TUI with live-updating metrics.

| Item | Detail |
|------|--------|
| Status model | `statusModel` (tea.Model) with IPC polling, gauge rendering, section layout |
| Queue gauge | `bubbles/progress` with Crab Orange fill |
| Proxy section | Request count, blocked count, error count, mapping degraded count |
| Commandments section | Rule count, triggers/min counter |
| Key bindings | `r` refresh, `q` quit via `bubbles/help` |
| Plain fallback | When no TTY, print current `status.go` output with theme colors stripped |

**Exit gates:**
- Dashboard updates every 3s without flicker
- Handles daemon-not-running gracefully (show offline state, retry on `r`)
- Window resize reflows layout
- `q` exits cleanly, no orphaned goroutines

### T3 — Agents Explorer

**Deliverable:** `crabwise agents` is an interactive table with detail view.

| Item | Detail |
|------|--------|
| Agents model | `agentsModel` (tea.Model) with `bubbles/table`, IPC polling |
| Status column | `◉`/`○` icons with color |
| Detail view | `enter` opens viewport with recent events for selected agent |
| Live refresh | Auto-refresh every 10s, `r` for manual |
| Plain fallback | Current tabwriter output |

**Exit gates:**
- Table scrolls correctly with many agents
- Detail view opens/closes without state corruption
- Handles zero agents gracefully

### T4 — Audit Explorer

**Deliverable:** `crabwise audit` is an interactive filterable, paginated event table.

| Item | Detail |
|------|--------|
| Audit model | `auditModel` (tea.Model) with `bubbles/table`, `bubbles/textinput`, `bubbles/paginator` |
| Filter mode | `/` activates filter input, supports field-prefixed queries (`o:blocked`, `a:tool_call`) |
| Detail view | `enter` opens viewport with full event JSON |
| Cost view | `c` toggles cost summary mode |
| Integrity | `v` triggers verification with spinner + result panel |
| Pagination | Terminal-height-aware page size, `n/p` navigation |
| Plain fallback | Current `audit.go` output |
| CLI flags preserved | `--since`, `--until`, `--agent`, `--export json` still work for scripting |

**Exit gates:**
- Table handles 1000+ events without lag (pagination, not full load)
- Filter applies in < 100ms
- Cost view totals match `--cost` plain output
- `--export json` bypasses TUI entirely (machine output)
- Existing audit integration tests pass

### T5 — Watch Revamp

**Deliverable:** `crabwise watch` upgraded with full theme system.

| Item | Detail |
|------|--------|
| Themed `View()` | Replace raw string concatenation with lipgloss-styled sections |
| Connection indicator | `◉ connected` / `○ reconnecting` / `✖ disconnected` in status bar |
| Warn/block highlighting | Full-width row background color on triggered events |
| Inline cost | Show `($0.023)` on ai_request events |
| Reconnect UX | Bubbles spinner during reconnect instead of text message |
| `--text` preserved | Plain text mode unchanged |

**Exit gates:**
- Existing `watch_tui_test.go` tests pass with adapted assertions
- Reconnection still works (1 retry, then fatal)
- Filter mode still works
- No regressions in `--text` mode

### T6 — Commandments Explorer

**Deliverable:** `crabwise commandments list` is an interactive table with detail/reload.

| Item | Detail |
|------|--------|
| Commandments model | `commandmentsModel` (tea.Model) with `bubbles/table` |
| Detail view | `enter` opens viewport with rule YAML definition |
| Reload | `r` triggers `commandments.reload` via IPC, updates table |
| Plain fallback | Current tabwriter output |

**Exit gates:**
- Table renders correctly with varying rule name lengths
- Reload updates table in-place
- Detail view scrolls for long rule definitions

---

## Implementation Ordering

```
T0 (foundation) → T1 (non-interactive) → T5 (watch revamp, since it exists)
                                        → T2 (status dashboard)
                                        → T3 (agents)
                                        → T4 (audit explorer)
                                        → T6 (commandments)
```

T0 and T1 are sequential (T1 depends on T0). After T1, the remaining milestones can be done in any order since they're independent commands. Suggested order above prioritizes upgrading existing TUI code (T5) and the most-used commands (T2, T4).

---

## Testing Strategy

- **Unit tests:** Each `tea.Model` gets table-driven tests via `Update()` + `View()` assertions (same pattern as existing `watch_tui_test.go`)
- **Visual tests:** Build-tag-gated test that renders each component to a string for snapshot comparison. Not pixel-perfect, but catches regressions in layout structure.
- **Plain mode tests:** Every command tested with `--plain` to verify no ANSI escapes in output
- **TTY detection tests:** Verify auto-detection logic with mocked isatty
- **Existing tests:** All current CLI tests must continue to pass unchanged. New TUI rendering is additive — plain output is the fallback, not a new code path.

## Dependencies

Already in `go.mod`, no new dependencies required:
- `charmbracelet/bubbletea v1.3.10`
- `charmbracelet/bubbles v0.20.0`
- `charmbracelet/lipgloss v1.1.0`
