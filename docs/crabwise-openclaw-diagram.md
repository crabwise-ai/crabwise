# How Crabwise governs your OpenClaw agent

```
                        YOUR SYSTEM
  +---------------------------------------------------------------+
  |                                                               |
  |  one-time setup:          +----------------------------+      |
  |  crabwise service inject  |   OpenClaw Agent Service   |      |
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
