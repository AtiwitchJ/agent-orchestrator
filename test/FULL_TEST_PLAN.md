# Full Test Plan — modern-agent

แผนการทดสอบทั้งระบบ อิงสภาพจริงของ repo ณ 2026-07-16 (branch `feat/live-terminals`)

---

## 0. เริ่มใช้ project (setup ก่อน test)

### สิ่งที่ต้องมี

| เครื่องมือ | เวอร์ชัน | ใช้ทำอะไร |
|---|---|---|
| Go | 1.25+ | backend daemon + CLI |
| Node.js | 20+ | frontend + สคริปต์ root |
| Git | ใดก็ได้ | worktrees |
| tmux | (macOS/Linux) | terminal runtime |
| Docker | optional | CLI fresh-install test |
| gh CLI + `GITHUB_TOKEN` | optional | SCM observer ยิง GitHub จริง |

### ติดตั้ง dependencies

```bash
git clone <repo> && cd moden-agent
npm install                # root (openapi-typescript)
cd frontend && npm install # Electron + React
```

### Build + รัน daemon

```bash
cd backend
go build ./...
go run ./cmd/ao start      # daemon ที่ 127.0.0.1:3001
```

เช็คว่า daemon ขึ้น:

```bash
curl localhost:3001/healthz   # liveness
curl localhost:3001/readyz    # readiness
```

### รัน frontend (desktop app)

```bash
cd frontend
npm run dev        # Electron app (spawn/attach daemon ให้เอง)
# หรือ npm run dev:web  — renderer อย่างเดียวใน browser
```

### ใช้งาน loop หลักครั้งแรก

```bash
ao project add <path-to-git-repo>   # เพิ่ม project
ao spawn <project> "<task>"          # spawn session ใน worktree แยก
ao session list                      # ดูสถานะ
```

(คำสั่งทั้งหมด: `ao --help`; CLI เป็น thin client — ต้องมี daemon รันอยู่)

### Config สำคัญ (env ล้วน ไม่มี config file)

| ตัวแปร | ค่าเริ่มต้น | ความหมาย |
|---|---|---|
| `AO_PORT` | `3001` | พอร์ต HTTP |
| `AO_DATA_DIR` | `~/.ao/data` | SQLite data dir |
| `AO_RUN_FILE` | `~/.ao/running.json` | PID/port handshake |
| `GITHUB_TOKEN` | - | GitHub auth |

กฎเหล็ก: state ทุกอย่างอยู่ใต้ `~/.ao` เท่านั้น — ลบทิ้งทั้ง dir = reset ระบบสะอาด (ระวัง: ลบ worktrees ที่ยังมีงานค้างด้วย)

### เช็คว่า environment พร้อม test

```bash
cd backend && go build ./... && go test ./internal/config/...   # เร็ว, ยืนยัน toolchain
cd frontend && npm run typecheck                                # ยืนยัน TS setup
```

---

## 1. ภาพรวมระบบ

ระบบมี 3 ชั้นหลัก + สัญญา API ที่ generate อัตโนมัติ:

```
ao CLI (Cobra, thin client)
        │ HTTP loopback
        ▼
Go daemon (127.0.0.1) ── chi router / SSE / WebSocket mux
        │
        ├─ service layer (derive status ตอนอ่าน — ไม่ store)
        ├─ session_manager / lifecycle / reaper
        ├─ observe (SCM polling GitHub, tracker intake)
        ├─ adapters (agent 23 ตัว, runtime, workspace, tracker)
        ├─ terminal mux (tmux PTY / conpty)
        └─ SQLite (goose migrations, sqlc, CDC ผ่าน DB triggers → change_log)
        ▲
Electron + React frontend (thin supervisor, typed client จาก OpenAPI)
```

หลักการที่ test ต้องคุ้มครอง (จาก `docs/architecture.md` + `AGENTS.md`):

- **Status ไม่ถูก store** — derive จาก durable facts (`activity_state`, `is_terminated`, PR facts) ตอน read เท่านั้น
- daemon เป็น loopback-only
- CLI ห้ามแตะ SQLite/runtime ตรง ๆ — ผ่าน daemon HTTP เท่านั้น
- CDC มาจาก DB trigger เท่านั้น ห้าม emit เอง
- probe fail ≠ session ตาย
- app state อยู่ใต้ `~/.ao` เท่านั้น

## 2. สถานะ test ปัจจุบัน

มีอยู่แล้วเยอะ — **ไม่ต้องเขียนใหม่จากศูนย์ ให้เติมช่องว่าง**

| ชั้น | มีอยู่ | เครื่องมือ |
|---|---|---|
| Backend unit/integration | ~197 ไฟล์ `_test.go` | `go test -race ./...` |
| Frontend unit/component | 51 ไฟล์ `.test.ts(x)` | vitest (`npm test` ใน `frontend/`) |
| Frontend e2e | 6 spec ใน `frontend/e2e/` | Playwright (`npm run test:e2e`) |
| CLI fresh-install | `test/cli/` | Docker container check |
| Policy smoke | `test/cli/run-policy-smoke.sh` | shell |
| CI | `.github/workflows/` (go, frontend, cli-e2e, api-drift, gitleaks ฯลฯ) | GitHub Actions |

### ช่องว่างที่พบ (จัดลำดับความสำคัญ)

| Package | src/test | ความเสี่ยง |
|---|---|---|
| `ports` | 12/1 | contract กลางของทั้งระบบ แทบไม่มี test |
| `domain` | 17/3 | vocabulary + derive logic |
| `storage` | 33/11 | migrations/queries ครอบคลุมบางส่วน |
| `agentlaunch` | 1/0 | ไม่มี test เลย |
| `processalive` | 2/0 | ไม่มี test เลย — ใช้ตัดสิน session เป็น/ตาย |
| `daemonmeta` | 1/0 | ไม่มี test เลย |
| `service` | 29/13 | จุด derive status — ต้องแน่นที่สุด |

## 3. Test Pyramid ที่ควรเป็น

```
        E2E (Playwright + CLI container)     ← น้อย, ช้า, full loop
        ─────────────────────────────
        Integration (httptest + sqlite จริง) ← ปานกลาง
        ─────────────────────────────
        Unit (go test / vitest)              ← เยอะ, เร็ว, ทุก PR
```

### 3.1 Backend unit — `go test -race ./...`

กติกา (ตาม `AGENTS.md`):

- table-driven ตามสไตล์ `backend/internal/cli/*_test.go`
- ห้ามยิง network — ใช้ `httptest`, fakes, injected deps
- ทุก behavior ใหม่ต องครอบ: happy path, validation/missing args, daemon error envelope, destructive confirmation

สิ่งที่ต้องเติม (เรียงตามความเสี่ยง):

1. **`processalive`** — probe pid มี/ไม่มี, pid reuse, permission denied ต้องไม่ตีความว่าตาย
2. **`service` status derivation** — ตารางเคส: ทุก combination ของ `activity_state` × `is_terminated` × PR facts (checks fail, mergeable, comments) → expected display status หนึ่งเดียว นี่คือ invariant สำคัญสุดของระบบ
3. **`domain`** — state transitions ที่ยังไม่มี test
4. **`storage`** — ทุก query ใน `queries/*` มีอย่างน้อย 1 round-trip test กับ sqlite จริง (in-memory/temp file), migration up จากศูนย์ต้องผ่านเสมอ
5. **`agentlaunch`**, `daemonmeta` — เล็ก แต่ 0 test = เติม 1 ไฟล์พอ

### 3.2 Backend integration — `backend/internal/integration/`

มี `lifecycle_sqlite_test.go`, `scm_observer_test.go` แล้ว เติม:

- **CDC end-to-end**: write ผ่าน store → trigger → `change_log` → poller → broadcaster → SSE subscriber ได้ event ถูกลำดับ + `Last-Event-ID` replay
- **Session full lifecycle บน sqlite จริง**: spawn → activity update → terminate → cleanup; ยืนยัน status ที่ read ออกมาถูกทุก step
- **Terminal mux**: attach/detach/reattach ผ่าน WebSocket (มี `attachment_integration_test.go` แล้ว — ขยายเคส disconnect กลางทาง)

### 3.3 API contract — กันการ drift

มีอยู่แล้วใน CI: spec drift + route/spec parity (`go test ./internal/httpd/...`) + `dto_drift_e2e_test.go` (CLI/HTTP wire compat)

กติกาเวลาแตะ API: แก้ `dto.go` + `specgen/build.go` → `npm run api` → commit `openapi.yaml` + `schema.ts` พร้อมกัน — CI fail ถ้าลืม

### 3.4 Frontend unit — vitest

- component test มีครบ component หลักแล้ว (SessionView, Sidebar, TerminalTile ฯลฯ)
- เติม: main-process modules ที่ยังไม่มี test (เช็คด้วย `find frontend/src/main -name '*.ts' ! -name '*.test.ts'`)
- กติกา: mock daemon ที่ API client boundary — อย่า mock ลึกกว่านั้น

### 3.5 Frontend e2e — Playwright

มี 6 spec. เติม flow หลักที่ยังขาด:

- add project → spawn session → เห็น session card ขึ้น
- terminal attach → พิมพ์ → เห็น output (live terminals — งานบน branch นี้พอดี)
- notification: `needs_input` โผล่ + read ack แล้วหาย

### 3.6 System smoke — full loop

flow ที่ `docs/STATUS.md` บอกว่า ship แล้ว: **add project → spawn → attach terminal → observe PR → merge**

- ทำเป็น script ใน `test/` แบบเดียวกับ `run-policy-smoke.sh`
- ใช้ daemon จริง + sqlite จริง + fake GitHub (httptest) — ห้ามยิง GitHub จริงใน CI

## 4. วิธีเขียน — TDD ทุก test ใหม่

ทุก test ที่เติมตามแผนนี้ เขียนแบบ red-green:

1. **RED** — เขียน test ก่อน รัน ดูมัน fail ด้วยเหตุผลที่ถูก (feature ขาด ไม่ใช่ typo)
2. **GREEN** — โค้ดน้อยสุดให้ผ่าน
3. **REFACTOR** — เก็บกวาดโดย test ยังเขียว

ห้าม: test ที่ผ่านทันทีตั้งแต่รันแรก (แปลว่า test ของเดิม ไม่ใช่ของใหม่), test mock แทน behavior จริง, เพิ่ม method ใน production code เพื่อ test อย่างเดียว

## 5. คำสั่งรันทั้งหมด (local gate)

```bash
# backend — gate หลัก
cd backend && go build ./... && go test -race ./...

# lint รวม (จาก root)
npm run lint

# frontend
cd frontend && npm run typecheck && npm test && npm run build

# frontend e2e
cd frontend && npm run test:e2e

# API drift
npm run api && git diff --exit-code

# CLI fresh-install (ต้องมี Docker)
# ดู test/cli/README.md
```

## 6. ลำดับงานแนะนำ

1. `service` status-derivation table test (invariant หลักของระบบ)
2. `processalive` + `agentlaunch` + `daemonmeta` (0→1)
3. `storage` query round-trips + migration-from-zero
4. `ports`/`domain` contract tests
5. CDC end-to-end integration
6. Playwright: live-terminal flow (ตรงกับ branch ปัจจุบัน)
7. Full-loop smoke script ใน `test/`
