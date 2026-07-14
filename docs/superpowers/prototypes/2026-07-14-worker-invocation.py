#!/usr/bin/env python3
"""
Prototype: Test 3 worker invocation modes (subprocess + tmux + custom).
Verifies that Hermes can control Claude/Codex CLIs and custom shell workers.
"""
import asyncio
import subprocess
import sys
import time
from pathlib import Path

# Color codes for output
GREEN = "\033[92m"
YELLOW = "\033[93m"
RED = "\033[91m"
BLUE = "\033[94m"
RESET = "\033[0m"

def log(msg, color=BLUE):
    print(f"{color}[{time.strftime('%H:%M:%S')}]{RESET} {msg}")

def ok(msg):
    print(f"{GREEN}  ✓ {msg}{RESET}")

def warn(msg):
    print(f"{YELLOW}  ⚠ {msg}{RESET}")

def err(msg):
    print(f"{RED}  ✗ {msg}{RESET}")


# ─────────────────────────────────────────────────────
# Test 1: Subprocess — invoke shell command (no LLM)
# ─────────────────────────────────────────────────────
async def test_subprocess_shell():
    """Simplest case: subprocess that runs a shell command and returns output."""
    log("Test 1: Subprocess (shell command, no LLM)")
    try:
        proc = await asyncio.create_subprocess_exec(
            "echo", "hello from subprocess",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE
        )
        stdout, stderr = await proc.communicate()
        output = stdout.decode().strip()
        assert output == "hello from subprocess", f"unexpected: {output!r}"
        ok(f"subprocess returned: {output!r}")
        return True
    except Exception as e:
        err(f"subprocess failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Test 2: Subprocess — invoke `claude --version` (if available)
# ─────────────────────────────────────────────────────
async def test_subprocess_claude_version():
    """Check if `claude` CLI is available and returns version."""
    log("Test 2: Subprocess (claude --version)")
    claude = Path.home() / ".local" / "bin" / "claude"
    if not claude.exists():
        warn("claude CLI not found at ~/.local/bin/claude — skipping")
        return None
    try:
        proc = await asyncio.create_subprocess_exec(
            str(claude), "--version",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=5)
        output = stdout.decode().strip()
        ok(f"claude --version: {output}")
        return True
    except asyncio.TimeoutError:
        err("claude --version timed out (>5s)")
        return False
    except Exception as e:
        err(f"claude --version failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Test 3: Subprocess — invoke `codex --version` (if available)
# ─────────────────────────────────────────────────────
async def test_subprocess_codex_version():
    """Check if `codex` CLI is available."""
    log("Test 3: Subprocess (codex --version)")
    codex = Path.home() / ".local" / "bin" / "codex"
    if not codex.exists():
        warn("codex CLI not found at ~/.local/bin/codex — skipping")
        return None
    try:
        proc = await asyncio.create_subprocess_exec(
            str(codex), "--version",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=5)
        output = stdout.decode().strip()
        ok(f"codex --version: {output}")
        return True
    except asyncio.TimeoutError:
        err("codex --version timed out (>5s)")
        return False
    except Exception as e:
        err(f"codex --version failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Test 4: Subprocess — invoke `agy --version` (if available)
# ─────────────────────────────────────────────────────
async def test_subprocess_agy_version():
    """Check if `agy` CLI is available."""
    log("Test 4: Subprocess (agy --version)")
    agy = Path.home() / ".local" / "bin" / "agy"
    if not agy.exists():
        warn("agy CLI not found at ~/.local/bin/agy — skipping")
        return None
    try:
        proc = await asyncio.create_subprocess_exec(
            str(agy), "--version",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=5)
        output = stdout.decode().strip()
        ok(f"agy --version: {output}")
        return True
    except asyncio.TimeoutError:
        err("agy --version timed out (>5s)")
        return False
    except Exception as e:
        err(f"agy --version failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Test 5: TMUX — check if tmux is available
# ─────────────────────────────────────────────────────
async def test_tmux_available():
    """Check if tmux binary is available."""
    log("Test 5: TMUX (binary check)")
    try:
        proc = await asyncio.create_subprocess_exec(
            "tmux", "-V",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE
        )
        stdout, stderr = await proc.communicate()
        output = stdout.decode().strip()
        ok(f"tmux: {output}")
        return True
    except FileNotFoundError:
        warn("tmux not found — skipping TMUX tests")
        return None
    except Exception as e:
        err(f"tmux check failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Test 6: TMUX — spawn a session, run a command, capture output
# ─────────────────────────────────────────────────────
async def test_tmux_spawn():
    """Spawn a tmux session, run a shell command, capture output, kill session."""
    log("Test 6: TMUX (spawn + capture + kill)")
    # Check tmux first
    tmux_check = await test_tmux_available()
    if tmux_check is None:
        return None

    session_name = f"prototype-test-{int(time.time())}"
    try:
        # Spawn tmux session with a command
        proc = await asyncio.create_subprocess_exec(
            "tmux", "new-session", "-d", "-s", session_name, "-x", "200", "-y", "50",
            "echo", "hello from tmux; sleep 1; echo done"
        )
        await proc.communicate()
        await asyncio.sleep(0.5)

        # Capture pane content
        proc = await asyncio.create_subprocess_exec(
            "tmux", "capture-pane", "-t", session_name, "-p",
            stdout=asyncio.subprocess.PIPE
        )
        stdout, _ = await proc.communicate()
        output = stdout.decode()
        ok(f"tmux captured output: {output!r}")

        # Wait for command to finish
        await asyncio.sleep(2)

        # Kill session
        proc = await asyncio.create_subprocess_exec(
            "tmux", "kill-session", "-t", session_name,
            stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE
        )
        await proc.communicate()
        ok(f"killed tmux session: {session_name}")
        return True
    except Exception as e:
        err(f"tmux spawn failed: {e}")
        # Cleanup
        subprocess.run(["tmux", "kill-session", "-t", session_name],
                       capture_output=True, check=False)
        return False


# ─────────────────────────────────────────────────────
# Test 7: libtmux (Python lib for tmux) — check if installed
# ─────────────────────────────────────────────────────
def test_libtmux_installed():
    """Check if libtmux Python library is installed."""
    log("Test 7: libtmux (Python library)")
    try:
        import libtmux  # noqa: F401
        ok(f"libtmux installed: {libtmux.__version__ if hasattr(libtmux, '__version__') else 'unknown version'}")
        return True
    except ImportError:
        warn("libtmux NOT installed — Hermes can use raw tmux CLI instead")
        return None


# ─────────────────────────────────────────────────────
# Test 8: Custom worker — invoke a real shell command as a "worker"
# ─────────────────────────────────────────────────────
async def test_custom_worker():
    """Simulate a custom worker that does real work (e.g., git status)."""
    log("Test 8: Custom worker (git status in /tmp)")
    try:
        proc = await asyncio.create_subprocess_exec(
            "git", "status", "--short",
            cwd="/tmp",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE
        )
        stdout, stderr = await proc.communicate()
        output = stdout.decode().strip()
        ok(f"custom worker (git status) returned {len(output)} chars")
        return True
    except Exception as e:
        err(f"custom worker failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Test 9: Worker registry — load YAML and list workers
# ─────────────────────────────────────────────────────
def test_worker_registry_yaml():
    """Verify worker registry YAML syntax is parseable."""
    log("Test 9: Worker registry (YAML parse)")
    import yaml  # type: ignore
    registry_yaml = """
workers:
  - name: claude-pm
    adapter: claude_code
    modes: [subprocess, sdk, tmux]
    default_mode: sdk
    tier: trusted
    cost_per_call: 0.15

  - name: codex-worker
    adapter: codex_cli
    modes: [subprocess, tmux]
    default_mode: subprocess
    cmd: ["codex", "exec", "--json"]
    tier: trusted
    cost_per_call: 0.12

  - name: agy-worker
    adapter: agy_cli
    modes: [subprocess]
    default_mode: subprocess
    cmd: ["agy", "-p", "--print"]
    tier: trusted
    cost_per_call: 0.10
"""
    try:
        data = yaml.safe_load(registry_yaml)
        assert "workers" in data
        assert len(data["workers"]) == 3
        ok(f"parsed {len(data['workers'])} workers from YAML")
        for w in data["workers"]:
            ok(f"  - {w['name']} (tier={w['tier']}, modes={w['modes']})")
        return True
    except ImportError:
        warn("PyYAML not installed — skipping YAML parse test")
        return None
    except Exception as e:
        err(f"YAML parse failed: {e}")
        return False


# ─────────────────────────────────────────────────────
# Main
# ─────────────────────────────────────────────────────
async def main():
    print(f"\n{BLUE}{'='*60}{RESET}")
    print(f"{BLUE}  Hermes Worker Prototype — 3-mode invocation test{RESET}")
    print(f"{BLUE}{'='*60}{RESET}\n")

    results = {}

    # Subprocess tests
    results["subprocess_shell"] = await test_subprocess_shell()
    results["subprocess_claude"] = await test_subprocess_claude_version()
    results["subprocess_codex"] = await test_subprocess_codex_version()
    results["subprocess_agy"] = await test_subprocess_agy_version()

    # TMUX tests
    results["tmux_available"] = await test_tmux_available()
    results["tmux_spawn"] = await test_tmux_spawn()

    # Custom worker + registry
    results["libtmux"] = test_libtmux_installed()
    results["custom_worker"] = await test_custom_worker()
    results["registry_yaml"] = test_worker_registry_yaml()

    # Summary
    print(f"\n{BLUE}{'='*60}{RESET}")
    print(f"{BLUE}  Results{RESET}")
    print(f"{BLUE}{'='*60}{RESET}\n")

    passed = 0
    skipped = 0
    failed = 0
    for name, r in results.items():
        if r is True:
            ok(f"{name}: PASS")
            passed += 1
        elif r is None:
            warn(f"{name}: SKIP (tool not available)")
            skipped += 1
        else:
            err(f"{name}: FAIL")
            failed += 1

    print()
    print(f"  Passed: {GREEN}{passed}{RESET}  Skipped: {YELLOW}{skipped}{RESET}  Failed: {RED}{failed}{RESET}")

    # Feasibility verdict
    print()
    if failed == 0:
        print(f"{GREEN}✓ Prototype successful — 3-mode worker invocation is feasible{RESET}")
        return 0
    else:
        print(f"{RED}✗ Prototype has failures — design may need adjustment{RESET}")
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
