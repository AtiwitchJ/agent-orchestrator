# ao org

Inspect and control the holding/company org hierarchy: the holding CEO headquarters, every company's PM headquarters, their delivery projects, and the global heartbeat kill switch.

If you are a PM or CEO orchestrator reacting to an `[AO heartbeat]` wake-up message, run `ao org status` first before doing anything else.

## Syntax

```
ao org <subcommand> [flags]
```

## Subcommands

---

### ao org status

Show the holding tree: the holding HQ, every company with its HQ and delivery projects, each project's active orchestrator (if any), and the current heartbeat pause state.

**Syntax:**
```
ao org status [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--company <id>` | Limit the report to one company id | - |
| `--json` | Output as JSON | - |

**Examples:**

```bash
# Show the whole holding tree
ao org status
```

```bash
# Show only one company's PM and projects (use this from inside a PM's HQ)
ao org status --company acme
```

---

### ao org pause

Pause the global heartbeat kill switch: no HQ orchestrator will receive further wake-up nudges until resumed. Survives a daemon restart.

**Syntax:**
```
ao org pause
```

---

### ao org resume

Resume the global heartbeat kill switch.

**Syntax:**
```
ao org resume
```
