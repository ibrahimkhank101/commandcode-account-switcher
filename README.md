# Command Code Account Switcher

Autonomous multi-account pool manager, real-time quota telemetry supervisor, and transparent HTTP 429 failover engine for **Command Code** (`command-code` / `cmdc`).

Engineered directly on Windows with Go 1.24 and pure SQLite.

---

## Architecture and Core Mechanics

Command Code resolves its backend API endpoint dynamically via `COMMANDCODE_API_URL` and `COMMANDCODE_SANDBOX`. By launching Command Code under the switcher supervisor proxy:

1. **Transparent Upstream Routing**: Every API call from `command-code` routes through local port `8766` to `https://api.commandcode.ai`.
2. **Dynamic Bearer Injection**: The supervisor strips any client auth header and injects the active pool account's API key.
3. **Zero-Drop 429 In-Memory Replay**: If the active account breaches rolling 5-hour or weekly limits, the supervisor catches the HTTP 429 response, selects the next healthy pool account, and immediately replays the in-flight request in memory without terminating the session or failing the agent.
4. **Synchronized System Auth**: On every rotation, `~/.commandcode/auth.json` is atomically swapped so external CLI commands always reflect the current active account.
5. **Per-Terminal Account Launcher**: `scripts/cmd-as.cmd <alias> [args...]` exports `COMMAND_CODE_API_KEY` strictly within that terminal process tree, enabling multiple accounts to run concurrently in side-by-side terminal windows.

---

## CLI Reference

```bash
# Launch Command Code under active supervisor proxy (auto-rotates on 429)
cmd-switch launch

# Run the supervisor daemon and web dashboard on http://127.0.0.1:8766
cmd-switch serve

# Onboard a new account into the pool
cmd-switch add-account --alias backup-acc --key user_...

# Display all registered accounts, quotas, and health
cmd-switch list-accounts

# Atomically switch active account
cmd-switch switch <alias>

# Synchronize quotas from api.commandcode.ai
cmd-switch refresh-quotas

# Inspect current supervisor health and quotas
cmd-switch status

# Execute any command with scoped proxy environment
cmd-switch wrap -- cmdc
```

---

## Per-Terminal Multi-Account Execution

To run different accounts concurrently in separate terminal tabs without collision:

```cmd
# Terminal 1: runs primary account
cmd-as primary

# Terminal 2: runs secondary account
cmd-as backup-acc
```

---

## Web Dashboard

Start daemon mode to access the Midnight Hive dashboard:

```bash
cmd-switch serve
```

Open `http://127.0.0.1:8766` in your browser to view:
- Live 5-hour and weekly window quota bars with percentage tracking.
- Real-time Server-Sent Events (SSE) telemetry log showing proxied requests and failover events.
- Privacy toggle (`P` key) to blur email addresses.
- One-click account activation and management.
