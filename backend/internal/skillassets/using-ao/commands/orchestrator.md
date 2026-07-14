# ao orchestrator

Manage orchestrator sessions.

## Syntax

```
ao orchestrator <subcommand> [flags]
```

## Subcommands

---

### ao orchestrator ls

List orchestrator sessions. Aliases: `ls`, `list`.

**Syntax:**
```
ao orchestrator ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |

**Examples:**

```bash
# List all orchestrator sessions
ao orchestrator ls
```

```bash
# List orchestrator sessions as JSON
ao orchestrator ls --json
```

---

### ao orchestrator spawn

Spawn (or replace) a project's orchestrator session. A project has at most one active orchestrator: without `--clean`, an existing active orchestrator is returned unchanged; with `--clean`, it is retired and a fresh one is spawned.

**Syntax:**
```
ao orchestrator spawn --project <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--project` | Project id to spawn the orchestrator in | Required |
| `--clean` | Retire any existing active orchestrator and spawn a fresh one | `false` |

**Examples:**

```bash
# Start (or fetch) a project's orchestrator
ao orchestrator spawn --project acme-api
```

```bash
# Replace a project's orchestrator with a fresh one
ao orchestrator spawn --project acme-api --clean
```
