# Crabwise AI — Product Overview

> **Audience:** Investors and Board Members
> **Version:** 1.0 — February 2026

---

## 1. Executive Summary

AI agents are getting keys to our digital lives. They read email, schedule meetings, write code, manage files, and transact on our behalf — autonomously. By end of 2026, 40% of enterprise applications will embed AI agents (Gartner). The autonomous agent market will reach $8.5B this year and $52.6B by 2030 (Deloitte).

There is no control plane for any of it.

**Crabwise AI is the oversight layer between AI agents, their providers, and the people who use them.** One interface to monitor what your agents are doing, set rules they cannot break, and maintain a complete audit trail — across every agent, every provider.

We are building:
- **Open-source developer tools** — to establish reputation, community, and credibility in the space
- **A full product for teams and enterprise** — locally hosted with optional secure cloud storage
- **Crabbot** — our own AI agent that serves as the primary interface for interacting with Crabwise, delivering notifications, surfacing insights, and actioning low-risk changes across every channel your team already uses

A company building AI agent oversight should itself be agent-first. Crabbot is that philosophy made product — the simplest, fastest way to interact with your Crabwise system without ever opening a dashboard.

The market opportunity is massive, the timing is right, and nobody is building for the segment we're targeting.

---

## 2. Problem Statement & Market Opportunity

### The Agent Explosion

AI agents are proliferating faster than the infrastructure to manage them:

| Signal | Data Point |
|--------|------------|
| Enterprise agent adoption | 40% of apps by end of 2026, up from <5% in 2025 (Gartner) |
| Market size | $8.5B in 2026 → $52.6B by 2030 at 46.3% CAGR (Deloitte) |
| Consumer demand | 44% of US consumers want a personal AI agent; 70% of Gen Z (Master of Code) |
| Agent sprawl | Avg enterprise uses 12 agents today, growing to 20 by 2027 (CIO Dive) |
| Shadow AI | 78% of AI users bring their own tools to work; 52% won't admit it (McKinsey) |

### The Governance Gap

Despite this explosion, governance hasn't kept pace:

- **Only 6%** of organizations have an advanced AI security strategy (HBR/Palo Alto Networks)
- **Only 1 in 5** companies has a mature model for governing autonomous agents (MIT Sloan)
- **58-59%** of organizations report monitoring capabilities, but **only 37-40%** have containment controls — kill switches, purpose binding (Cloud Security Alliance)
- AI-associated data breaches cost **$650K+** per incident (IBM 2025)

Organizations can see something is wrong, but they can't stop it. Monitoring without containment is awareness without protection.

### Why Existing Tools Fall Short

| Tool Category | Why It Fails for AI Agents |
|--------------|---------------------------|
| API Gateways | "Intent blindness" — can't understand the semantic intent behind agent actions |
| iPaaS (Zapier, etc.) | Built for deterministic workflows; can't handle probabilistic AI loops |
| MLOps/Observability (Arize, LangSmith) | Observe but can't intercept or enforce policies |
| IAM (Okta/Auth0) | Confirms human identity, but can't control what autonomous agents do on their behalf |
| Enterprise governance (Zenity, Arthur AI) | $100-500/seat/month, complex, built for Fortune 500 |

The entire agent management market is optimized for large enterprise. Nobody builds for the developer, the small team, or the growing company that needs governance today without a six-figure budget.

---

## 3. Target Users & Personas

### Developers (Open-Source Tools)

**Who they are:** Individual developers and power users running 2-5 AI agents across coding tools (Claude Code, Cursor, Copilot), local models (Ollama), and personal agents (OpenClaw).

**What they need:**
- See what all their agents are doing in one place
- Set hard rules ("never run `rm -rf`", "never push to main without confirmation")
- Search and export a history of everything their agents did
- Get alerted when agents access sensitive files or exceed cost thresholds

**How we reach them:** Open-source tools, MIT-licensed. One-line install. Build reputation and community through genuine utility.

### Teams & Enterprise (Full Product)

**Who they are:** Engineering teams (5-50+ people), startups scaling AI agent usage, and enterprises that need governance without the overhead of traditional enterprise tools.

**What they need:**
- Shared policies enforced across the entire team's agent fleet
- Unified audit trails with export for compliance and billing
- Cost tracking and budget controls across all agents and providers
- Locally hosted deployment with optional secure cloud for team sync

**How we reach them:** Bottom-up adoption from developers who already use the open-source tools, expanding into team and enterprise features as organizations formalize their agent governance.

---

## 4. Product Vision & Positioning

### The Middleware Layer

Crabwise AI sits in the critical space between three parties:

```
    ┌──────────────┐         ┌──────────────┐
    │   AI Agent   │         │   Provider   │
    │  (OpenClaw,  │◄───────►│ (Anthropic,  │
    │  Claude Code,│         │  OpenAI,     │
    │  custom)     │         │  local LLMs) │
    └──────┬───────┘         └──────────────┘
           │
    ┌──────▼───────┐
    │  CRABWISE AI │  ◄── oversight, rules, audit
    └──────┬───────┘
           │
    ┌──────▼───────┐
    │     YOU      │
    │  (the user)  │
    └──────────────┘
```

The agent has capabilities. The provider gives it intelligence. **Crabwise gives you the admin panel** — visibility, control, and a complete record across all of it.

### Agent-First Experience

We build AI agent oversight — so we should deliver it through an AI agent. **Crabbot** is Crabwise's own AI agent: the omni-present, conversational interface through which administrators and users interact with the entire Crabwise system.

Instead of requiring users to navigate dashboards for every question or action, Crabbot meets them where they already are — Slack, Discord, terminal, desktop notifications, email — and handles the interaction directly. Ask it what your agents did today. Tell it to tighten a commandment. Get a cost summary. Crabbot handles it.

**The security boundary is simple:** Crabbot can read anything, surface any insight, deliver any notification, and action low-risk changes (pause a session, acknowledge an alert, adjust a warning threshold). But high-risk changes — modifying commandments, adjusting kill switch settings, changing team permissions — require the authenticated dashboard. Crabbot links you directly there when needed. No friction, no ambiguity.

This is our UX philosophy: **simple by default, secure when it matters.**

### Where We Sit in the Stack

In the current technology landscape, value is concentrating in three places: the agent layer (execution), the data layer (systems of record), and the infrastructure layer (security, governance, compliance). Crabwise AI is infrastructure — the governance and oversight layer that every organization deploying AI agents will need. This is the category that, as the industry matures, will generate durable, compounding value.

---

## 5. Product Strategy

### Open-Source Developer Tools → Full Product for Teams & Enterprise

Our strategy has two reinforcing components:

**1. Open-source tools for developers**
- Core monitoring, audit, and rule enforcement — MIT-licensed, free forever
- Build credibility and trust in the AI agent community
- Drive adoption through genuine utility: install Crabwalk or other realted Crabwise tools, immediately see value
- Create a community of contributors who extend platform support to more agents
## **Goal:** Become the default tool developers install alongside their first AI coding agent

**2. Full product for teams and enterprise**
- Shared policies, team audit trails, cost controls, admin features
- Locally hosted by default — your agents touch your codebase, your files, your credentials; that data shouldn't leave your machine
- Optional secure cloud storage for team sync, shared configurations, and push notifications
## **Goal:** Convert organic developer adoption into team and enterprise accounts

### Why Local-First

Every competitor is cloud-first SaaS. We are local-first by design.

- AI agents access codebases, credentials, files, and financial accounts. That telemetry data is sensitive by default.
- Local-first is a trust differentiator in a market where developers are already wary of sending data to third parties.
- The core product works fully offline. Cloud features are opt-in, not required.

---

## 6. Product Pillars

### Pillar 1: Crabwalk — Live Agent Monitor

The flagship experience. A real-time, multi-agent dashboard where you see all your agents working in one place.

- **Multi-agent visibility** — every agent, every provider, one view
- **Visual activity graph** — interactive node-based visualization of sessions, actions, tool calls, and spawned processes
- **Aggregate metrics** — tokens consumed, estimated cost, actions per hour, success rates
- **Anomaly detection** — flag unusual behavior (agent running too long, spending too much, touching unexpected files)
- **Timeline replay** — scrub through historical agent activity to understand what happened and when

Crabwalk exists today as a working prototype monitoring OpenClaw agents. The path forward is generalizing it across all agent platforms.

### Pillar 2: Commandments — Infrastructure-Level Agent Controls

**The most novel concept in the space.** Commandments are immutable rules that override all agent behavior regardless of provider. They are enforced at the infrastructure layer, not the prompt layer — meaning they **cannot be jailbroken**.

**Examples:**
```yaml
commandments:
  # Financial
  - NEVER send crypto transactions without confirmation
  - LIMIT total spend across all agents to $50/day

  # Communications
  - NEVER send emails on my behalf without confirmation
  - NEVER post to social media

  # Files & Security
  - NEVER delete files matching "*.env, *.key, *.pem"
  - NEVER execute commands containing "rm -rf"

  # Autonomy
  - ALWAYS require confirmation for "git push" to main
  - LIMIT concurrent agents to 5
```

**Three enforcement levels:**
1. **Hard block** — action intercepted and stopped; agent receives an error
2. **Confirm** — action paused; user must explicitly approve before it proceeds
3. **Warn** — action proceeds but is flagged in the audit log with an alert

**Why this matters:** Existing security tools enforce guardrails at the prompt or model layer — which can be bypassed via prompt injection or jailbreaks. Crabwise commandments operate at the proxy/infrastructure layer. The agent's LLM never sees the rule, so it can't reason around it. The rule is enforced on the wire, not in the conversation. This aligns with OWASP's core principle of **Least Agency** (OWASP Top 10 for Agentic Applications, 2026).

### Pillar 3: Audit — Cross-Agent Event History

Structured, searchable, exportable history of everything your agents do.

- **Unified event log** — who (which agent), what (action + arguments), when (timestamp), where (session/context), outcome (success/failure/blocked)
- **Search & filter** — by agent, time range, action type, commandment triggers
- **Cost tracking** — per agent, per session, per day; know exactly what you're spending
- **Export** — CSV, JSON, PDF for billing clients, proving compliance, debugging failures
- **Configurable retention** — from 7 days to custom retention windows

### Pillar 4: Notifications — Cross-Agent Alert Layer

A notification system that works across all your agents and providers — delivered primarily through Crabbot.

- **Rule-based alerts:** cost threshold reached, commandment triggered, task completed, anomaly detected
- **Multiple channels:** Slack, Discord, desktop notifications, email, mobile push (later) — all delivered by Crabbot with conversational context
- **Digest mode** — batch low-priority notifications into periodic summaries instead of interrupting
- **Actionable delivery** — notifications aren't just informational; Crabbot lets you respond inline (acknowledge, pause agent, escalate) without switching to the dashboard

### Pillar 5: Crabbot — Agent-First Administrative Interface

Crabwise's own AI agent. The omni-present, conversational way to interact with your entire Crabwise system.

- **System health at a glance** — ask Crabbot "how are my agents doing?" and get a plain-language summary of active agents, recent alerts, cost trends, and anomalies
- **Insight on demand** — "what did my agents do yesterday?", "which agent is spending the most?", "show me all blocked actions this week" — answered instantly, no dashboard navigation required
- **Low-risk actions** — Crabbot can pause agent sessions, acknowledge alerts, adjust warning thresholds, and trigger audit exports directly from the conversation
- **Multi-channel presence** — available in Slack, Discord, terminal (CLI), desktop app, and email; meets users wherever they already work
- **Notification delivery** — Crabbot is the primary vehicle for all Crabwise alerts and warnings, delivering them with context and actionable next steps
- **Secure escalation** — high-risk changes (editing commandments, modifying kill switch settings, changing team permissions, adjusting enforcement levels) are never actioned through Crabbot; instead, it provides a direct authenticated link to the relevant dashboard page

**The design principle:** Crabbot should handle 80% of daily Crabwise interactions without the user ever opening a dashboard. The dashboard exists for configuration, deep investigation, and high-risk changes. Crabbot exists for everything else.

### Pillar 6: Multi-Platform Support

Support for many agent platforms, closed and open source.

| Tier | Method | Platforms | Enforcement Level |
|------|--------|-----------|-------------------|
| Native | Direct API/protocol integration | OpenClaw, MCP servers, Ollama | Full |
| Log Watcher | Parse agent log files & process activity | Claude Code, Cursor, Copilot | Read-only (monitor + audit) |
| Proxy | Local HTTP/WS proxy agents route through | Any HTTP-based agent | Full |

**MCP as primary integration vector:** MCP (Model Context Protocol) is the dominant standard for connecting AI agents to tools — 97M+ monthly SDK downloads, adopted by OpenAI, Google, and Microsoft, donated to the Agentic AI Foundation under Linux Foundation. Crabwise positions as MCP-aware middleware. Wrapping MCP servers with commandment enforcement is the most universal integration path.

---

## 7. Requirements Overview

### What the Product Must Do (Functional Requirements)

| Capability | Description |
|-----------|-------------|
| **Agent Discovery** | Automatically detect AI agents running on the user's machine or network; maintain a registry of known agents |
| **Real-Time Monitoring** | Display live activity across all connected agents — actions, tool calls, resource access — in a single dashboard |
| **Rule Enforcement** | Evaluate every agent action against user-defined commandments; block, pause, or flag actions in real time |
| **Audit Logging** | Capture a complete, structured record of every agent action — who, what, when, where, outcome |
| **User Attribution** | Bind every agent action to a human user or service account; maintain chain of custody |
| **Notifications & Alerts** | Deliver timely alerts when rules are triggered, costs are exceeded, or anomalies are detected |
| **Cost Tracking** | Track token usage and estimated spend per agent, per session, per day |
| **Data Export** | Export audit logs and reports in standard formats (CSV, JSON, PDF) |
| **Kill Switch** | Provide immediate stop controls — global hard stop, session pause, and scoped blocks for specific tools or agents |
| **Data Protection** | Detect and redact PII and secrets in agent prompts and responses before they leave the machine |
| **Crabbot Interface** | Provide a conversational AI agent (Crabbot) across Slack, Discord, CLI, desktop, and email that can answer questions about system health, deliver notifications, and action low-risk changes — while routing high-risk changes to authenticated dashboards |

### How It Must Perform (Non-Functional Requirements)

| Requirement | Target |
|------------|--------|
| Latency overhead | < 100ms at the gateway layer; users should not feel the oversight layer |
| Rule evaluation speed | < 10ms per decision |
| Uptime | 99.9% availability for the local daemon |
| Data retention | Configurable from 7 to 365 days |
| Privacy | All data stored locally by default; cloud features are opt-in |
| Cross-platform | macOS, Windows, Linux desktop support |

---

## 8. Architecture Summary

Crabwise AI uses a **local-first architecture** with an optional cloud layer for team features.

```
YOUR MACHINE
├── Crabwise Daemon (background process)
│   ├── Adapter Layer (one per agent platform)
│   ├── Commandment Engine (rule evaluation + enforcement)
│   ├── Audit Logger (structured event storage)
│   ├── Event Bus (internal message routing)
│   └── Notification Router (alert dispatch)
│
├── Crabbot (AI agent interface)
│   ├── Multi-channel presence (Slack, Discord, CLI, desktop, email)
│   ├── Read access to all Crabwise data (agents, audit, metrics)
│   ├── Action layer (low-risk ops: pause, acknowledge, export)
│   └── Secure escalation (links to dashboard for high-risk changes)
│
├── Crabwise Dashboard (browser or desktop app)
│   └── Authenticated UI for configuration & high-risk changes
│
├── SQLite (durable local storage)
│
└── Optional: Crabwise Cloud (opt-in)
    ├── Team Sync (shared policies & audit)
    ├── Crabbot Multi-Channel Routing
    └── Push Notifications
```

**Key architectural principles:**
- **Zero Trust for AI Agents** — no agent is trusted by default; every request is verified
- **Defense in Depth** — multiple layers of protection from network through application
- **Observable by Design** — structured logging at all decision points, OpenTelemetry-compatible

For full architectural detail — including component deep dives, deployment patterns, security controls, identity architecture, and compliance framework alignment — see the [Architecture Document](./architecture.md/THEPRODUCT_AI_Agent_Watchdog_Architecture.md).

---

## 9. Go-to-Market Strategy

### Phase 1: Open-Source Credibility

- Ship Crabwalk 1.0.0 by Crabwise AI as the post-OpenClaw evolution of the existing Crabwalk project _(Operation OpenCrab)_
- Open-source core: monitor + audit + commandments as feature-capped Crabwise tools (MIT license)
- Content marketing: blog posts, demo videos showing multi-agent monitoring and commandment enforcement
- Distribution: Hacker News, Reddit (r/LocalLLaMA, r/ChatGPT), AI Twitter/X, Product Hunt
- Work with and attract Developer interest globally
- **Key metrics:** GitHub stars, Discord community size, weekly installs

### Phase 2: Community & Ecosystem

- Additional agent adapters: Cursor, Copilot, Ollama
- Community-contributed commandment sets for common use cases
- Conference talks at AAIF/MCP summits; contribute to open standards
- Partnerships with agent platform teams (Claude Code, Cursor, Copilot)
- **Goal:** Become the default tool people install alongside their first AI coding agent
- **Key metrics:** Weekly active users, community contributions

### Phase 3: Commercial Layer

- Launch team features: shared commandments, shared audit trails, user management
- Crabwise Cloud: team sync, push notifications, secure cloud storage
- Introduce paid tiers for teams and enterprise
- **Key metrics:** Monthly recurring revenue, paying teams, incidents or attempted breaches detected

### Distribution Channels

1. CLI install (one-liner curl/npx)
2. Package managers (Homebrew, apt, snap, npm)
3. VS Code / Cursor extension
4. Docker (containerized deployment)
5. Desktop app (Tauri builds for macOS, Windows, Linux)
6. MCP marketplace (listed as an MCP server/tool)
7. Community word-of-mouth (Discord, GitHub, social)

---

## 10. Phased Delivery Roadmap

### Phase 1 — "See & Control"

The foundation. Ship a multi-agent monitor with commandment enforcement.

- Generalize Crabwalk monitor beyond OpenClaw
- Claude Code adapter (file-based log watcher)
- Commandments v1: YAML-based rules, pattern matching, hard block + warn enforcement
- Audit v1: SQLite-backed event log, basic search/filter, JSON/CSV export
- Desktop app v1: Tauri wrapper, tray icon, background daemon
- **Milestone:** Install Crabwise, see Claude Code + OpenClaw in one dashboard, block destructive commands, export audit logs

### Phase 2 — "Expand & Notify"

More agents, more control, more visibility — and an agent-first interface.

- Additional adapters: Cursor, Copilot, Ollama
- Commandments v2: cost limits, rate limits, confirmation mode, MCP proxy interception
- **Crabbot v1:** CLI and desktop presence; answers questions about agent activity, delivers notifications, actions low-risk changes (pause, acknowledge, export); links to dashboard for high-risk operations
- Notifications delivered through Crabbot: desktop native + webhook integration (Slack, Discord)
- Cost tracking + analytics dashboard (spend per agent, per day, trends)
- **Milestone:** A 5-person team uses Crabwise, interacts daily through Crabbot, shares commandments, gets cost alerts

### Phase 3 — "Share & Grow"

Team features, community growth, and Crabbot everywhere.

- Team features: shared commandments, shared audit trail, user management
- **Crabbot v2:** Slack and Discord channel presence for teams; team-aware context (knows who's asking and scopes responses to their permissions); proactive daily/weekly health digests
- Cloud sync: secure opt-in cloud for team policy distribution and cross-device access
- Standards contributions: Agent Commandment Protocol draft, OpenTelemetry GenAI proposals
- Mobile companion: PWA for read-only monitoring + notification management
- **Milestone:** Active teams using shared policies; most daily interactions happen through Crabbot; growing open-source community with 500+ Discord members

### Phase 4 — "Ecosystem"

Platform maturity and revenue diversification.

- Enterprise-lite tier: SSO, compliance-ready audit exports, SLA
- Community-driven adapter contributions (Windsurf, Aider, Continue, custom bots)
- A2A protocol support for agent-to-agent monitoring
- **Crabbot v3:** AI-powered proactive insights ("your agents are 30% more efficient on Tuesdays", "this commandment blocked 47 destructive actions this month"); email channel support; natural language commandment authoring ("block all agents from accessing my email after 6pm" → Crabbot drafts the rule, links to dashboard for approval)

---

## 11. Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Platform vendors build native monitoring** (e.g., Anthropic adds a dashboard to Claude Code) | HIGH | Cross-vendor value is the moat. More native tools from more vendors = more fragmentation = more need for a unified control plane. No single vendor will build for all agents. |
| **MCP or AAIF absorbs commandments as a standard** | MEDIUM | Be the ones proposing the standard. If commandments become a standard, Crabwise is the reference implementation. This validates the concept and drives adoption to the best tooling. |
| **Enterprise players move downmarket** (Zenity, Arthur AI launch free tiers) | MEDIUM | Enterprise DNA doesn't serve developers or small teams. Their UX will be complex, their pricing will creep up. Stay opinionated, simple, fast. |
| **Agent platforms make integration harder** (lock down APIs, change log formats) | MEDIUM | Diversify integration strategies. Log-watcher and proxy approaches don't depend on official APIs. Open-source community contributes adapter fixes faster than platforms can break things. |
| **Personal agent adoption slower than expected** | HIGH | Keep burn rate low. Product is useful today for existing agent users. Even if multi-agent adoption takes 18+ months, developers using 2-3 coding agents is already standard. |
| **Commandment enforcement failure (liability)** | HIGH | Clear "best effort" legal language. Commandments are technical controls, not contractual guarantees. Invest in testing. Run a bug bounty program. Transparent incident communication. |
| **Consolidation wave** | MEDIUM | Being acquired is a valid outcome. $2B+ spent on AI security acquisitions in 18 months shows appetite. Community traction makes Crabwise an attractive target. |

---

## 12. Open Questions

1. **Desktop runtime:** Tauri vs Electron for the desktop app?
2. **Commandment engine runtime:** In-process Node.js or separate sidecar (Rust/Go) for reliability and performance?
3. **Agent adapter architecture:** Separate npm packages (plugin system) or monorepo?
4. **Standards engagement:** Timeline and resource allocation for AAIF/OpenTelemetry contributions?
5. **Legal:** "No-means-no" marketing language — review needed to distinguish best-effort technical controls from guarantees
6. **Sandboxed environments:** How does the daemon reach agents running in Docker containers or cloud VMs?
7. **Crabbot LLM provider:** Which model powers Crabbot — self-hosted, third-party API, or user-configurable? How do we ensure Crabbot itself respects the same privacy principles (local-first) we apply to agent data?

---

## 13. References

### Market Data
- [Deloitte TMT Predictions 2026: AI Agent Orchestration](https://www.deloitte.com/us/en/insights/industry/technology/technology-media-and-telecom-predictions/2026/ai-agent-orchestration.html)
- [Gartner: 40% of Enterprise Apps Will Feature AI Agents by 2026](https://www.gartner.com/en/newsroom/press-releases/2025-08-26-gartner-predicts-40-percent-of-enterprise-apps-will-feature-task-specific-ai-agents-by-2026-up-from-less-than-5-percent-in-2025)
- [Master of Code: 150+ AI Agent Statistics 2026](https://masterofcode.com/blog/ai-agent-statistics)
- [CIO Dive: IT Leaders Grapple with Agent Sprawl](https://www.ciodive.com/news/it-leaders-grapple-ai-agent-sprawl-integration/811411/)
- [ISACA: Rise of Shadow AI](https://www.isaca.org/resources/news-and-trends/industry-news/2025/the-rise-of-shadow-ai-auditing-unauthorized-ai-tools-in-the-enterprise)

### Standards & Protocols
- [Pento: A Year of MCP — 2025 Review](https://www.pento.ai/blog/a-year-of-mcp-2025-review)
- [The New Stack: Why the Model Context Protocol Won](https://thenewstack.io/why-the-model-context-protocol-won/)
- [Linux Foundation: AAIF Announcement](https://www.linuxfoundation.org/press/linux-foundation-announces-the-formation-of-the-agentic-ai-foundation)
- [OpenTelemetry Blog: AI Agent Observability](https://opentelemetry.io/blog/2025/ai-agent-observability/)
- [OWASP Top 10 for Agentic Applications 2026](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/)

### Security & Governance
- [Lakera: Q4 2025 Agent Attack Trends](https://www.lakera.ai/blog/the-year-of-the-agent-what-recent-attacks-revealed-in-q4-2025-and-what-it-means-for-2026)
- [IBM 2025 Cost of Data Breach Report](https://www.ibm.com/reports/data-breach)
- [MIT Sloan: The Emerging Agentic Enterprise](https://sloanreview.mit.edu/projects/the-emerging-agentic-enterprise-how-leaders-must-navigate-a-new-age-of-ai/)
- [Cloud Security Alliance: AI Safety Initiative](https://cloudsecurityalliance.org/)

### Regulatory
- [DLA Piper: EU AI Act Latest Obligations](https://www.dlapiper.com/en-us/insights/publications/2025/08/latest-wave-of-obligations-under-the-eu-ai-act-take-effect)
- [White House: AI Executive Orders 2025](https://www.whitehouse.gov/presidential-actions/2025/12/eliminating-state-law-obstruction-of-national-artificial-intelligence-policy/)

### Internal Documents
- [Architecture Document](./architecture.md/THEPRODUCT_AI_Agent_Watchdog_Architecture.md)
- [Strategy Document](./strategy.md)
- [Market Research](./market-research.md)

---

*Document Version: 1.2*
*Last Updated: February 2026*
