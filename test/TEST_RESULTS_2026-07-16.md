# Test Results — modern-agent

**วันที่รัน:** 2026-07-16 (Thursday)
**Branch:** `feat/live-terminals`
**Base commit:** `6a8da628` — feat(policy): production Engine — Store, state machine, CIGate/HumanGate graduation
**ผู้รัน:** Hermes (implementer/builder) + user (tester/instructor)
**แผนอ้างอิง:** [`FULL_TEST_PLAN.md`](./FULL_TEST_PLAN.md)

---

## 0. สรุปรวม (Executive Summary)

| เกณฑ์ | ผลลัพธ์ | หมายเหตุ |
|---|---|---|
| Backend build | ✅ PASS | 0 errors |
| Backend tests (`go test -race`) | ✅ ~50 packages PASS | 1 flake (re-run pass) |
| Frontend typecheck | ⏸ ไม่ได้รัน | เครื่องไม่มี Go ในตอนแรก, focus ที่ gate หลัก |
| Frontend vitest | ⚠️ 478/483 PASS (96.7%) | 5 fail = **real regression** บน branch นี้ |
| Frontend Playwright e2e | ⚠️ 12/12 fail = env issue | Root-caused IPv6/IPv4 mismatch, แก้ได้ |
| Tests เขียนใหม่ | ✅ 7 tests, 3 packages | processalive + agentlaunch + daemonmeta |

**Overall verdict:** 🔴 **มี regression จริงที่ต้องตัดสินใจก่อน merge**

---

## 1. Environment Setup

เครื่องเริ่มต้นมีแค่ Node.js (`/Users/up-mac/.hermes/node/bin/node` v22.23.1) — ไม่มี Homebrew, ไม่มี Go, ไม่มี tmux, ไม่มี gh CLI ต้องติดตั้งเองทั้งหมด:

| Tool | Version | ติดตั้งที่ | วิธี |
|---|---|---|---|
| **Go** | 1.25.4 → 1.25.7 (auto-upgrade) | `~/.local/go/` | tarball จาก `go.dev/dl` |
| **libevent** | 2.1.12-stable | `~/.local/` | build static (`--disable-openssl`, `--enable-static`) |
| **ncurses** | 6.5 | `~/.local/` | build wide-char static |
| **tmux** | 3.5 | `~/.local/bin/` | `--disable-utf8proc`, ต้อง CPPFLAGS ชี้ `ncursesw/` |
| **gh CLI** | 2.81.0 | `~/.local/bin/` | prebuilt macOS arm64 zip |
| **Playwright chromium** | 1223 | `~/Library/Caches/ms-playwright/` | `npx playwright install chromium` |
| **node_modules** | - | repo root + `frontend/` | `npm install` ทั้ง 2 ที่ |

PATH ที่ใช้ตลอด session:
```bash
export PATH="$HOME/.local/go/bin:$HOME/.local/bin:$PATH"
```

**Pitfalls discovered (worth saving as skill):**
1. macOS sandbox นี้ไม่ route IPv4↔IPv6 — Vite default ที่ listen `::1` เท่านั้น → Playwright `127.0.0.1` connection refused → ต้อง `--host 127.0.0.1`
2. `libevent 2.1.12` configure fail ถ้าไม่มี OpenSSL headers → ใช้ `--disable-openssl` (lossless สำหรับ use-case ของ tmux)
3. tmux 3.5 fail ถ้าไม่มี emoji-support lib → ใช้ `--disable-utf8proc`
4. ncurses 6.5 build สำเร็จ แต่ `tic` step ตอน install fail (permission denied บน `/usr/share/terminfo`) — ไม่กระทบ build (static lib ติดตั้งแล้ว)

---

## 2. Backend (Go) Results

### 2.1 Build
```bash
$ cd backend && go build ./...
(0 errors)
```

### 2.2 Test — `go test -race -count=1 -timeout=300s ./...`

**Summary: 50+ packages PASS, 1 FAIL (flake)**

Failed test:
```
--- FAIL: TestAuthStatusUnknownWhenKeyOnlyComesFromInteractiveShell (3.01s)
    auth_test.go:150: context deadline exceeded
FAIL    github.com/modernagent/modern-agent/backend/internal/adapters/agent/kilocode  5.852s
```

### 2.3 Root cause analysis — flake ไม่ใช่ bug

Source ของ test (`auth_test.go`):
- ไม่มี network call — รัน fake-shell + fake-kilocode binary ใน `t.TempDir()`
- ใช้ `t.Setenv("SHELL", shellPath)`, `t.Setenv("PATH", dir)`, ฯลฯ
- รัน `Plugin{resolvedBinary: kilocodePath}.AuthStatus(ctx)` ที่คาดว่าจะใช้เวลา < 100ms

**Verify isolated re-run:**
```bash
$ go test -race -count=1 -timeout=60s -run "TestAuthStatusUnknownWhenKeyOnlyComesFromInteractiveShell" \
    ./internal/adapters/agent/kilocode/...
ok  	github.com/modernagent/modern-agent/backend/internal/adapters/agent/kilocode  2.164s
```

**Classification:** **CI race contention flake** ไม่ใช่ production issue — ควรเพิ่ม `-timeout=10s` ในไฟล์นั้น (กันไม่ให้ block คนอื่นใน CI) — **action ที่ tester ต้องตัดสิน**

### 2.4 ผล test ที่สำคัญที่คาดว่าจะเป็น focus ของแผน

| Package | ผล | หมายเหตุ |
|---|---|---|
| `internal/service/session` | ✅ PASS | มี status_test.go 17+7+5+5+1 cases ครอบครบ (ดู section 5) |
| `internal/cdc` | ✅ PASS | ไม่มี regression |
| `internal/daemon` | ✅ PASS | ไม่มี regression |
| `internal/httpd` | ✅ PASS | spec drift + route parity ผ่าน |
| `internal/cli` | ✅ PASS | Cobra tests ผ่าน |
| `internal/integration` | ✅ PASS | `lifecycle_sqlite_test.go` + `scm_observer_test.go` |
| `internal/lifecycle` | ✅ PASS | Manager Reaper tests ผ่าน |
| `internal/observe/scm` | ✅ PASS | GitHub observer tests ผ่าน |
| `internal/storage/sqlite` | ✅ PASS | migrations + sqlc queries ผ่าน |
| `internal/storage/sqlite/store` | ✅ PASS | round-trip tests ผ่าน |

---

## 3. Frontend (vitest) Results

### 3.1 Summary

```bash
$ cd frontend && npm test
 Test Files  2 failed | 53 passed (55)
      Tests  5 failed | 478 passed (483)
   Duration  9.46s
```

**Pass rate:** 478 / 483 = **96.7%** ✅
**Failed files:** `Sidebar.test.tsx`, `HQSection.test.tsx`

### 3.2 Failed tests

| # | Test | Type |
|---|---|---|
| 1 | `Sidebar.test.tsx > Sidebar > requires explicit worker and orchestrator agents when creating a project` | regression |
| 2 | `Sidebar.test.tsx > Sidebar > shows needs-auth agents as unavailable while keeping authorized agents selectable` | regression |
| 3 | `Sidebar.test.tsx > Sidebar > updates project agent options when the catalog loads after the dialog opens` | regression |
| 4 | `Sidebar.test.tsx > Sidebar > renders a flat project list with no company headers when no companies exist (no regression)` | regression |
| 5 | `HQSection.test.tsx > HQSection — no HQ project yet > auto-provisions the holding HQ (no path in the request) and starts the CEO orchestrator` | regression |

### 3.3 Root cause analysis — branch refactor vs test drift

Test #1 ตัวอย่าง:
```typescript
// Sidebar.test.tsx:211
await user.click(screen.getByLabelText("New project"));   // ❌ FAILS
```

เทียบกับ Sidebar component ปัจจุบัน:
```typescript
// Sidebar.tsx:65
const label = isChoosingPath ? "Opening..." : isCreating ? "Creating..." : "New project";
// button renders {label} as text content, NOT as aria-label
```

Test ค้นหาด้วย `getByLabelText("New project")` (สมมติว่ามี aria-label) แต่ component refactor เป็น button-with-text — **branch `feat/live-terminals` เปลี่ยน component โดยไม่อัพเดต test**

**Classification:** **real branch regression** — มี 2 ทางเลือก:
1. **อัพเดต test** ใช้ `getByRole("button", { name: "New project" })` (4 จุดใน Sidebar.test.tsx + 1 จุดใน HQSection)
2. **Revert component** ใส่ `aria-label="New project"` กลับเข้าไป

**ต้องการ decision จาก tester** — branch นี้ทำไม refactor component ตรงนี้? ดู git log + ดู commit ที่ touch Sidebar.tsx แล้วเทียบ

### 3.4 Verify-by-remove (manual spot-check)

ลองดู Sidebar test file ตรง ๆ — pattern เดียวกันซ้ำ 5 ครั้ง (5 fail = 4 ใน Sidebar + 1 ใน HQSection) = **consistent regression pattern** ไม่ใช่ flake

---

## 4. Frontend (Playwright) Results

### 4.1 Summary — 12/12 fail (env issue ไม่ใช่ code bug)

```bash
$ npm run test:e2e
...
12 failed
   e2e/history-nav.spec.ts
   e2e/inspector-toggle.spec.ts
   e2e/multi-pr.spec.ts
   e2e/reviews-tab.spec.ts
   e2e/titlebar-brand.spec.ts
   e2e/workbench.spec.ts
```

ทุก fail มี error เดียวกัน:
```
Error: page.goto: net::ERR_CONNECTION_REFUSED at http://127.0.0.1:5173/
```

### 4.2 Root cause — IPv6 vs IPv4

**Investigation:**
```bash
$ nc -z 127.0.0.1 5173  →  port CLOSED   # IPv4
$ curl http://localhost:5173/ → 200 OK       # IPv6 (localhost = ::1)

$ lsof -i :5173 -P
node  40350  up-mac  29u  IPv6  ...  TCP localhost:5173 (LISTEN)   # ← IPv6 ONLY
```

Vite default config ที่ `dev:web` script ใช้ listen `localhost` (= `::1`) เท่านั้น — แต่ Playwright `playwright.config.ts` ใช้ `baseURL: "http://127.0.0.1:5173"` (IPv4) — sandboxed environment ของ macOS ไม่ route IPv4→IPv6

### 4.3 Fix — `--host 127.0.0.1`

```bash
$ VITE_NO_ELECTRON=1 npm run dev:web -- --port 5173 --host 127.0.0.1
  VITE v8.0.16  ready in 441 ms

  ➜  Local:   http://localhost:5173/
  ➜  Network: http://127.0.0.1:5173/    # ← listen ทั้ง 2 stack แล้ว

$ curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:5173/  →  200
```

**Run status:** re-run ด้วย `CI=1 npm run test:e2e` หลัง fix ถูก dispatch ไปแล้วเป็น background process (`proc_f86bc2a9c404`) — **ยังไม่ได้ verify ว่า 12 fail หายไปจริง** (process หยุดก่อนนำ log มาวิเคราะห์)

### 4.4 Side issue found

Log แรกของ Vite มี warning:
```
Warning: Route file ".../routes/_shell.index.test.tsx" does not export a Route.
This file will not be included in the route tree.
If this file is not intended to be a route, you can exclude it using:
  1. Rename to ".../-_shell.index.test.tsx" (prefix with "-")
  2. Use 'routeFileIgnorePattern' in your config
```

→ **ไฟล์ `_shell.index.test.tsx` อยู่ใน `routes/` directory ทำให้ TanStack Router คิดว่าเป็น route file** แต่จริง ๆ เป็น test file ของ vitest — ควร rename prefix `-` ตาม warning

**Action:** **ต้องแก้ทั้งไฟล์** หรืออย่างน้อยเพิ่ม `routeFileIgnorePattern` ใน vite/tanstack config

---

## 5. Tests ที่เขียนใหม่ (Task #1, #2 จากแผน section 6)

### Task #1: service status-derivation table test — ✅ ALREADY PRESENT

แผนข้อ #1 ตรวจพบว่า test นี้มีครบแล้วใน `backend/internal/service/session/status_test.go`:

| Test function | Cases | Coverage |
|---|---|---|
| `TestServiceDerivesStatusFromSessionFactsAndPR` | 17 | happy path ทุก `activity_state × PR facts` combination |
| `TestAggregateStackedChildSignals` | 7 | blocked-stacked-child behavior |
| `TestHarnessSignalsCapabilityGate` | 1 | capability predicate injection |
| `TestDeriveStatusStalled` | 5 | worker-only stall immunity, beats-mergeable |
| `TestDeriveStatusDocsRepo` | 5 | docs-repo branch: `ReportReady` / `ReportPending` |

→ **task #1 เสร็จโดยไม่ต้องเขียนเพิ่ม** (35+ cases)

### Task #2: processalive + agentlaunch + daemonmeta (0→1) — ✅ DONE ใหม่

เขียน **7 tests ใหม่** ใน 3 packages:

#### `backend/internal/processalive/alive_test.go` (3 tests)
1. `TestAliveSelfIsAlive` — self PID → true (positive case)
2. `TestAliveRejectsNonPositive` — pid 0/-1/-9999 → false (table-driven)
3. `TestAliveNonexistentPIDReturnsFalse` — sentinel `0x7FFFFFFE` → false

> 💡 **Load-bearing invariant** ที่ doc-comment pin: "EPERM/AccessDenied → true" — นี่คือ AGENTS.md rule ที่ว่า "probe fail ≠ session dead"

#### `backend/internal/agentlaunch/spec_test.go` (3 tests)
1. `TestWriteTempReadAndRemoveRoundTrip` — 3 subtests: argv-only / with-fallback / preserve-order
2. `TestReadAndRemoveRequiresArgv` — argv missing → error mentioning "argv"
3. `TestReadAndRemoveRejectsMalformedJSON` — bad JSON → error

#### `backend/internal/daemonmeta/meta_test.go` (1 test)
1. `TestServiceNameStable` — pin `ServiceName = "modern-agent-daemon"`

**ผลรัน:**
```bash
$ go test -race -count=1 ./internal/processalive/... ./internal/agentlaunch/... ./internal/daemonmeta/...
ok    internal/processalive  2.513s
ok    internal/agentlaunch   2.061s
ok    internal/daemonmeta    1.580s
```

→ **ทั้ง 3 packages pass ทันที** ไม่มี regression (verify ด้วย isolated re-run ของ race-test)

**Pattern note:** ทุก test ใช้ `package_test` (black-box) พร้อม doc-comment ที่ pin invariant — ตาม style `backend/internal/ports/agent_test.go` ที่มีอยู่

---

## 6. ที่ค้างอยู่ (Action Items สำหรับ next session)

| # | Item | Priority | Effort |
|---|---|---|---|
| **A1** | ตัดสิน Sidebar regression (5 vitest fail) — แก้ test หรือ revert component | 🔴 high | 15 min |
| **A2** | Verify Playwright re-run หลัง IPv4 fix (ดู `/tmp/playwright2.log`) | 🔴 high | 5 min |
| **A3** | เพิ่ม `-timeout=10s` ใน kilocode flaky test | 🟡 med | 5 min |
| **A4** | Rename `_shell.index.test.tsx` → `-_shell.index.test.tsx` หรือเพิ่ม `routeFileIgnorePattern` | 🟡 med | 5 min |
| **A5** | Task #3: storage query round-trip audit (11 ไฟล์มีอยู่แล้ว) | 🟢 low | 30 min |
| **A6** | Task #4: ports/domain contract test gaps | 🟢 low | 30 min |
| **A7** | Task #5: CDC end-to-end integration | 🟢 low | 60 min |
| **A8** | Task #6: Playwright live-terminal flow spec | 🟢 low | 30 min |
| **A9** | Task #7: Full-loop smoke script (ตามแบบ `run-policy-smoke.sh`) | 🟢 low | 45 min |
| **A10** | Commit 3 ไฟล์ใหม่ (processalive + agentlaunch + daemonmeta tests) | 🔴 high | 5 min |

---

## 7. ไฟล์ที่เปลี่ยนใน session นี้

```
A  backend/internal/processalive/alive_test.go    (2043 bytes, 3 tests)
A  backend/internal/agentlaunch/spec_test.go     (3283 bytes, 3 tests)
A  backend/internal/daemonmeta/meta_test.go       (590 bytes, 1 test)
?? test/FULL_TEST_PLAN.md                        (untracked - plan file)
 M frontend/package-lock.json                     (npm install)
```

**No production code changes** — clean "fill-the-gap" style ตามแผน section 2

---

## 8. Skills / Learnings (ควร save ไว้)

### 🤖 Tool / Environment

1. **macOS sandbox IPv6/IPv4 mismatch** — Vite default ที่ `localhost` listen `::1` เท่านั้น, Playwright/Docker/สคริปต์ที่ใช้ `127.0.0.1` จะ connection refused → workaround `--host 127.0.0.1`
2. **Build tmux บน macOS without Homebrew** — ใช้ tarball + libevent 2.1.12 (`--disable-openssl`) + ncurses 6.5 (static) + tmux 3.5 (`--disable-utf8proc`), ทั้งหมดด้วย `~/.local/` prefix
3. **Background process ที่ไม่ใช่ server** ต้อง `notify_on_complete=true` — Hermes มี safeguard เตือนทุกครั้งที่รัน background แบบ silent

### 🧪 Testing Strategy

1. **Flake isolation recipe** — เมื่อ fail เพียง test เดียว: re-run ด้วย `-run <TestName>` + เวลาที่สั้นกว่า → ถ้า pass = flake (CI contention) ถ้า fail = bug จริง
2. **TDD สำหรับ "fill-the-gap" tests** — กฎ iron-law ผ่อนคลายเมื่อเขียน test สำหรับ code ที่มีอยู่ → test ผ่านทันทีไม่ผิด TDD เพราะเป้าหมายคือ pin contract ไม่ใช่ verify ใหม่

---

## 9. แนวทางต่อไป (สำหรับ tester)

**แนะนำ:** ให้ reviewer agent ของ user ทำ A1 (Sidebar) + A2 (verify Playwright) ก่อน commit — เพราะทั้ง 2 เป็น regression ที่ต้องตัดสินว่า revert component หรือ update test แล้ว commit ทั้งหมดเป็น 1 PR พร้อม fix

**Risk note:** branch `feat/live-terminals` มี live terminal changes (PR focus) ที่อาจทำให้ merge ไป main ต้องผ่าน review เพิ่ม — ควรทำ fix ให้ครบก่อน push

---

## 10. Addendum — ผลการแก้ (session ถัดมา, 2026-07-16)

ทุก action item แดง/เหลืองเคลียร์แล้ว ผลสรุปที่ต่างจากการวินิจฉัยข้างบน:

- **A1 ไม่ใช่ branch regression** — `Sidebar` ไม่เคย render ปุ่ม "New project" (ทั้ง main และ branch); dialog tests 3 ตัวเป็นของ `CreateProjectFlow` → ย้าย + ใช้ `getByRole`. ส่วน "flat project list" เป็น **component bug จริง**: doc comment สัญญา no-companies fallback แต่โค้ดไม่มี → เพิ่ม fallback ใน `Sidebar.tsx`
- **HQSection fail** = mock `../lib/api-client` ขาด export ใหม่ `hasTrustedApiBaseUrl` (จาก branch นี้) + query refetch ตอน mount ทับ seeded data → เพิ่ม mock export + `setQueryDefaults(staleTime: Infinity)`
- **A2 สำเร็จ**: `--host 127.0.0.1` แก้ connection refused; ที่เหลืออีก 6 spec เป็น **stale specs** (UI เก่า "orchestrator-first workbench" + mock data เก่า + hash-history deep link + API shape `PRReviewState` ใหม่ + `VITE_AO_API_BASE_URL` gating) → เขียน spec ใหม่/แก้ selector → **Playwright 12/12 PASS**
- **A3 เปลี่ยนคำตัดสิน**: kilocode fail ซ้ำ 2/2 ใน full race suite (ไม่ใช่ flake หายาก) — probe 3s สั้นไปสำหรับ fork/exec ของ test binary ตัวใหญ่ (modernc sqlite) ใต้ load → bump เป็น 5s ตาม precedent ของ amp
- **A4 ใช้ `routeFileIgnorePattern`** ใน vite + tsr config (ไม่ rename)
- **Typecheck พบ pre-existing fails** 2 ไฟล์ (`env.test.ts`, `useBrowserView.test.ts`) — cast `Window & {ao: unknown}` ไม่หลุด type ใหม่ของ `window.ao` → เปลี่ยนเป็น `window as unknown as {ao: unknown}`

**ผลสุดท้าย:** vitest 483/483 · typecheck ผ่าน · Playwright 12/12 · backend `go test -race` เขียว (ยืนยันรอบสุดท้ายก่อน commit)
