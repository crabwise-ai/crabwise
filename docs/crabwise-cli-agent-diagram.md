# How Crabwise governs your AI coding agent

```
                        YOUR SYSTEM
  +---------------------------------------------------------------+
  |                                                               |
  |  launch with:             +----------------------------+      |
  |  crabwise wrap -- claude  |   AI Coding Agent          |      |
  |  crabwise wrap -- codex   |   (Claude Code, Codex CLI) |      |
  |                           +----------------------------+      |
  |                                        |                      |
  |                                   AI request                  |
  |                                        |                      |
  |                                        v                      |
  |                  +------------------------------------+        |
  |   Your Policy    |                                    |        |
  |   Rules    ----->|    Crabwise Proxy + Policy Engine  |        |
  |  (Commandments)  |                                    |        |
  |                  +------------------------------------+        |
  |                          /                  \                  |
  |                    blocked                allowed through      |
  |                        /                        \             |
  |              +-----------------+         +-----------------+  |
  |              |    Blocked      |         |                 |--+---> AI Provider
  |              |    HTTP 403     |         |  (forwarded)    |        (OpenAI,
  |              +-----------------+         +-----------------+         Anthropic)
  |              request never                                    |
  |              reaches the                                      |
  |              AI provider                                      |
  |                    \                          /               |
  |                     \                        /                |
  |                      v                      v                 |
  |              +------------------------------------------------+|
  |              |        Tamper-Evident Audit Log                ||
  |              |   Every action recorded -- hash-chain verified ||
  |              +------------------------------------------------+|
  +---------------------------------------------------------------+
```
