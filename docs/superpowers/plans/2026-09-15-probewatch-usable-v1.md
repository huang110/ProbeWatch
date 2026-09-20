# ProbeWatch 可用版 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a locally runnable ProbeWatch monitoring system with GitHub OAuth, secure node registration, a Go monitoring Agent, resource/network/MTR/media-HTTP checks, and the existing Chinese React console connected to real APIs.

**Architecture:** A Go monolith owns OAuth, sessions, SQLite, admin APIs, Agent APIs, structured check configuration, and the embedded production SPA. A separate Go Agent makes outbound HTTPS requests only, reports resource data, executes allowlisted checks locally, and never exposes SSH, terminal, file, exec, MCP, or an inbound listener. Local development runs the Go API on `127.0.0.1:8080` and Vite on `127.0.0.1:5173` with a same-origin proxy; production packages the built SPA with the Go binary.

**Tech Stack:** Go 1.23+, `modernc.org/sqlite`, standard-library `net/http`, GitHub OAuth, Argon2id-compatible token hashing through `golang.org/x/crypto`, React 18, Vite, Phosphor Icons, Playwright smoke tests, systemd for Linux Agent deployment.

---

## File Map

Create the Go workspace at the repository root:

```text
go.mod
cmd/probewatch/main.go
cmd/probewatch-agent/main.go
internal/config/config.go
internal/db/migrations.go
internal/db/store.go
internal/auth/github.go
internal/auth/session.go
internal/auth/csrf.go
internal/api/server.go
internal/api/middleware.go
internal/api/auth_handlers.go
internal/api/admin_handlers.go
internal/api/agent_handlers.go
internal/api/validation.go
internal/protocol/agent.go
internal/monitor/resource.go
internal/monitor/network.go
internal/monitor/mtr.go
internal/monitor/media.go
internal/security/targets.go
internal/security/tokens.go
internal/agent/config.go
internal/agent/reporter.go
internal/agent/scheduler.go
internal/agent/install.go
internal/agent/systemd.go
web/embed.go
web/dist/.gitkeep
deploy/probewatch.service
deploy/probewatch-agent.service
deploy/install-agent.sh
deploy/docker-compose.local.yml
```

Modify the existing frontend:

```text
frontend/vite.config.js
frontend/src/main.jsx
frontend/src/api/client.js
frontend/src/api/errors.js
frontend/src/components/ConnectionState.jsx
frontend/src/components/NodeList.jsx
frontend/src/components/NodeDrawer.jsx
frontend/src/pages/LoginPage.jsx
frontend/src/pages/OverviewPage.jsx
frontend/src/pages/NodesPage.jsx
frontend/src/pages/ChecksPage.jsx
frontend/src/pages/MtrPage.jsx
frontend/src/pages/MediaPage.jsx
frontend/tests/browser_regression.py
```

Tests:

```text
internal/auth/*_test.go
internal/db/*_test.go
internal/security/*_test.go
internal/monitor/*_test.go
internal/api/*_test.go
internal/agent/*_test.go
tests/integration_test.go
```

Do not add SSH, terminal, file-manager, exec, MCP, arbitrary shell, or auto-update code to any of these files.

## Task 1: Create the Go Workspace and Configuration Boundary

**Files:**
- Create: `go.mod`
- Create: `cmd/probewatch/main.go`
- Create: `cmd/probewatch-agent/main.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `deploy/docker-compose.local.yml`
- Create: `.env.example`

- [ ] **Step 1: Write failing configuration tests**

Test that required production values are rejected, local defaults bind only to loopback, and unsafe TLS bypass is never accepted:

```go
func TestLoadRejectsMissingProductionSecrets(t *testing.T) {
    t.Setenv("PROBEWATCH_ENV", "production")
    t.Setenv("GITHUB_CLIENT_ID", "")
    t.Setenv("GITHUB_CLIENT_SECRET", "")
    t.Setenv("SESSION_SECRET", "short")
    if _, err := Load(); err == nil {
        t.Fatal("Load() accepted an invalid production configuration")
    }
}

func TestLoadLocalDefaultsToLoopback(t *testing.T) {
    t.Setenv("PROBEWATCH_ENV", "development")
    cfg, err := Load()
    if err != nil {
        t.Fatal(err)
    }
    if cfg.ListenAddress != "127.0.0.1:8080" {
        t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
    }
    if cfg.AgentIgnoreUnsafeCert {
        t.Fatal("development default enabled unsafe TLS bypass")
    }
}
```

- [ ] **Step 2: Run the tests and verify the expected failure**

Run from the repository root:

```powershell
go test ./internal/config -run TestLoad -v
```

Expected: compilation failure because `Load` and the config package do not exist.

- [ ] **Step 3: Implement the configuration package**

Define a `Config` containing `Environment`, `ListenAddress`, `DatabasePath`, `PublicBaseURL`, GitHub OAuth values, `SessionSecret`, `AgentTokenTTL`, `AgentClockSkew`, `MaxRequestBody`, and `AgentIgnoreUnsafeCert`. Parse durations and sizes from environment variables, require a 32-byte minimum session secret outside development, reject `AgentIgnoreUnsafeCert=true` outside development, and set local defaults to `127.0.0.1:8080` and `./data/probewatch.db`.

The executable entry points should only load config, open the store, construct the API or Agent runtime, and return errors. No package may read environment variables during request handling.

- [ ] **Step 4: Add local environment and Compose definitions**

`.env.example` must contain names only, never credentials:

```text
PROBEWATCH_ENV=development
PROBEWATCH_LISTEN=127.0.0.1:8080
PROBEWATCH_DATABASE=./data/probewatch.db
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=
GITHUB_REDIRECT_URL=http://127.0.0.1:8080/auth/github/callback
GITHUB_ALLOWED_USERS=
GITHUB_ALLOWED_ORG=
SESSION_SECRET=
```

Compose must bind the API to `127.0.0.1`, mount only `./data`, and must not mount the Docker socket or host root.

- [ ] **Step 5: Run focused tests**

```powershell
go test ./internal/db ./internal/security -v
```

Expected: all configuration tests pass.

## Task 2: Implement SQLite Schema and Token-Safe Persistence

**Files:**
- Create: `internal/db/migrations.go`
- Create: `internal/db/store.go`
- Create: `internal/db/store_test.go`
- Create: `internal/security/tokens.go`
- Create: `internal/security/tokens_test.go`

- [ ] **Step 1: Write failing persistence tests**

Cover migrations, registration-token one-time consumption, node-token hash-only storage, token revocation, and request-ID replay rejection:

```go
func TestRegistrationTokenCanBeConsumedOnlyOnce(t *testing.T) { /* create, consume, consume again */ }
func TestNodeTokenPlaintextIsNotPersisted(t *testing.T) { /* inspect DB values */ }
func TestRevokedNodeTokenCannotAuthenticate(t *testing.T) { /* revoke, authenticate */ }
func TestRequestIDReplayIsRejected(t *testing.T) { /* insert twice */ }
```

- [ ] **Step 2: Run tests to verify they fail**

```powershell
go test ./internal/db ./internal/security -race -v
```

Expected: compilation failure because the store and token functions do not exist.

- [ ] **Step 3: Add the schema**

Create SQLite tables for `admin_users`, `sessions`, `oauth_states`, `registration_tokens`, `nodes`, `node_tokens`, `resource_latest`, `network_targets`, `network_results_latest`, `mtr_targets`, `mtr_results_latest`, `media_detectors`, `media_results_latest`, `request_replays`, and `audit_events`.

Apply `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, and `PRAGMA busy_timeout=5000`. Create the data directory with mode `0700`; make the database file and backup mode `0600`.

- [ ] **Step 4: Implement cryptographic token helpers**

Use `crypto/rand` for token generation and SHA-256 HMAC with a server-side token-pepper for stored node and registration token digests. Compare digests with `crypto/subtle.ConstantTimeCompare`. Return plaintext only at registration-token creation and node-registration response; never log it or persist it.

- [ ] **Step 5: Implement store methods and replay protection**

Use transactions for consuming a registration token and creating a node/token pair. Store a request ID with an expiration timestamp and reject duplicate IDs inside the transaction. Add cleanup for expired replay IDs. Every revoke/rotate/delete operation writes an audit event.

- [ ] **Step 6: Run focused tests and race detection**

```powershell
go test ./internal/auth ./internal/api -run 'TestOAuth|TestUnauthorized|TestWrite|TestSession' -v
```

Expected: all tests pass with no race reports.

## Task 3: Implement GitHub OAuth, Sessions, CSRF, and HTTP Middleware

**Files:**
- Create: `internal/auth/github.go`
- Create: `internal/auth/session.go`
- Create: `internal/auth/csrf.go`
- Create: `internal/auth/auth_test.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/auth_handlers.go`
- Create: `internal/api/middleware_test.go`

- [ ] **Step 1: Write failing auth tests**

Use `httptest.Server` as a fake GitHub provider. Test unknown-user rejection, OAuth state reuse rejection, session-cookie flags, unauthenticated API 401, CSRF failure for a write request, and accepted same-origin CSRF:

```go
func TestOAuthStateIsSingleUse(t *testing.T) { /* callback twice */ }
func TestUnauthorizedGitHubUserIsRejected(t *testing.T) { /* fake user not allowlisted */ }
func TestWriteWithoutCSRFIsForbidden(t *testing.T) { /* POST with valid session, no CSRF */ }
func TestSessionCookieIsHttpOnlyAndSameSite(t *testing.T) { /* inspect Set-Cookie */ }
```

- [ ] **Step 2: Run the tests and verify failure**

```powershell
go test ./internal/auth ./internal/api -v
```

Expected: compilation failure because auth and middleware do not exist.

- [ ] **Step 3: Implement OAuth state and callback handling**

Generate a 32-byte random state, store only its HMAC digest with a 10-minute expiry, and set a temporary HttpOnly state cookie. On callback require exact state match, consume the state in a transaction, exchange the code with the configured GitHub endpoint, request `/user` and optional organization membership, and reject users outside the allowlist.

- [ ] **Step 4: Implement server-side sessions**

Generate a 32-byte session ID, store only its digest and expiry in SQLite, and set a cookie with `HttpOnly`, `SameSite=Lax`, and `Secure` when not in development. Add logout and session expiration. Never put a GitHub access token in the browser or React state.

- [ ] **Step 5: Implement CSRF and middleware**

Expose `GET /api/csrf` for an authenticated session. Store the CSRF secret server-side and return a one-time or rotating token to the frontend. Require it on `POST`, `PATCH`, and `DELETE`; also validate `Origin` against the configured public base URL. Middleware must enforce request body limits and structured JSON content types.

- [ ] **Step 6: Run auth tests**

```powershell
go test ./internal/api ./internal/protocol -v
```

Expected: all auth and middleware tests pass.

## Task 4: Define Agent Protocol and Registration/Report APIs

**Files:**
- Create: `internal/protocol/agent.go`
- Create: `internal/protocol/agent_test.go`
- Create: `internal/api/validation.go`
- Create: `internal/api/agent_handlers.go`
- Create: `internal/api/admin_handlers.go`
- Create: `internal/api/agent_handlers_test.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Write failing protocol and API tests**

Cover JSON decoding limits, registration expiry, node UUID binding, bearer authentication, timestamp skew, request replay, report persistence, and token rotation:

```go
func TestRegisterConsumesTokenAndReturnsNodeToken(t *testing.T) { /* POST register */ }
func TestRegisterWithExpiredTokenReturnsUnauthorized(t *testing.T) { /* 401 */ }
func TestReportRejectsReplayAndWrongNode(t *testing.T) { /* 401/409 */ }
func TestAdminCanRotateAndRevokeNodeToken(t *testing.T) { /* 200 then 401 */ }
```

- [ ] **Step 2: Run tests to verify failure**

```powershell
go test ./internal/api ./internal/protocol -race -v
```

Expected: compilation failure because handlers and protocol types do not exist.

- [ ] **Step 3: Define strict wire types**

Define `RegisterRequest`, `RegisterResponse`, `ReportRequest`, `ResourceSnapshot`, `CheckTask`, `CheckResult`, and `ErrorResponse`. Use `json.Decoder.DisallowUnknownFields`, explicit maximum lengths, and `http.MaxBytesReader`. Do not expose database structs directly.

- [ ] **Step 4: Implement Agent registration**

Add authenticated-admin `POST /api/registration-tokens`, public `POST /api/agent/v1/register`, and admin `GET /api/nodes`. Hash and consume registration tokens transactionally, bind the returned long-lived token to the submitted node UUID, and return no token after the initial response.

- [ ] **Step 5: Implement report and result endpoints**

Add `POST /api/agent/v1/report`, `POST /api/agent/v1/network-result`, `POST /api/agent/v1/mtr-result`, and `POST /api/agent/v1/media-result`. Require bearer token, timestamp skew, request-ID replay protection, node UUID match, body limits, and per-node rate limits. Store only latest values in phase one.

- [ ] **Step 6: Implement admin node operations**

Add `POST /api/nodes/:id/rotate-token` and `POST /api/nodes/:id/revoke`, protected by session and CSRF. Return a new token only once from rotation and write audit events.

- [ ] **Step 7: Run API tests**

```powershell
go test ./internal/security ./internal/monitor -run 'Test.*Target|Test.*SSRF' -v
```

Expected: all registration, report, replay, and token lifecycle tests pass.

## Task 5: Implement SSRF-Safe Network Target Validation

**Files:**
- Create: `internal/security/targets.go`
- Create: `internal/security/targets_test.go`
- Create: `internal/monitor/network.go`
- Create: `internal/monitor/network_test.go`

- [ ] **Step 1: Write failing target-validation tests**

Test rejection of loopback, RFC1918, link-local, IPv6 local, cloud metadata, unsupported schemes, userinfo, invalid ports, oversized paths, and DNS rebinding where a hostname resolves to a blocked address.

- [ ] **Step 2: Run tests to verify failure**

```powershell
go test ./internal/security ./internal/monitor -race -v
```

Expected: compilation failure because validation and network checks do not exist.

- [ ] **Step 3: Implement address validation**

Resolve hostnames with a bounded context, inspect every returned IP using `net/netip`, reject loopback/private/link-local/unspecified/multicast and known metadata ranges, and re-resolve before dialing. Reject Unix sockets, proxy environment variables, credentials in URLs, non-HTTP schemes, and ports outside `1..65535`.

- [ ] **Step 4: Implement TCP, HTTP, HTTPS, and DNS checks**

Use per-check contexts and a transport with `Proxy=nil`, disabled connection reuse, a bounded redirect policy that revalidates every destination, `GET` only, no caller-supplied headers, a response-body limit, and a total timeout. DNS checks must use a bounded resolver and return only typed timing/status data.

- [ ] **Step 5: Run security and network tests**

```powershell
go test ./internal/monitor -run 'TestMTR|TestRoute|TestPath' -v
```

Expected: all SSRF, timeout, redirect, and result-normalization tests pass.

## Task 6: Implement Built-In MTR and Media HTTP Detectors

**Files:**
- Create: `internal/monitor/mtr.go`
- Create: `internal/monitor/mtr_test.go`
- Create: `internal/monitor/media.go`
- Create: `internal/monitor/media_test.go`
- Modify: `internal/protocol/agent.go`

- [ ] **Step 1: Write failing MTR tests**

Inject a route-probe function and test maximum-hop clamping, total timeout, destination matching, intermediate timeout handling, and path fingerprint stability.

- [ ] **Step 2: Run tests to verify failure**

```powershell
go test ./internal/monitor -run 'TestMTR|TestRoute|TestPath|TestMedia' -race -v
```

Expected: compilation failure because the MTR package does not exist.

- [ ] **Step 3: Implement bounded MTR**

Use Go ICMP APIs rather than shelling out to `mtr`. Clamp hops to `1..30`, use a 900 ms per-hop timeout and 45-second total context, serialize one MTR run per Agent, match replies by destination and sequence, and normalize each route into a stable fingerprint that ignores reverse-DNS name changes.

- [ ] **Step 4: Write failing media-detector tests**

Test fixed GET method, allowed headers, bounded body reads, status mapping, region-rule matching, timeout classification, and rejection of detector definitions containing cookies, credentials, scripts, or arbitrary headers.

- [ ] **Step 5: Implement media HTTP detector**

Allow only structured detector definitions with URL, expected status, redirect policy, bounded match rules, and region mapping. Return `available`, `blocked`, `region_limited`, `unknown`, `timeout`, or `error`. Never persist complete response bodies.

- [ ] **Step 6: Run detector tests**

```powershell
go test ./internal/agent -v
```

Expected: all MTR and media detector tests pass.

## Task 7: Implement the Go Agent Runtime and Linux Installer

**Files:**
- Create: `internal/agent/config.go`
- Create: `internal/agent/reporter.go`
- Create: `internal/agent/scheduler.go`
- Create: `internal/agent/metrics.go`
- Create: `internal/agent/agent_test.go`
- Create: `deploy/install-agent.sh`
- Create: `deploy/probewatch-agent.service`
- Modify: `cmd/probewatch-agent/main.go`

- [ ] **Step 1: Write failing Agent tests**

Test config file mode expectations, no-listener startup, report retry/backoff, 30-second resource interval, task allowlist rejection, and token not appearing in logs.

- [ ] **Step 2: Run tests to verify failure**

```powershell
go test ./internal/agent -race -v
go vet ./internal/agent ./cmd/probewatch-agent
```

Expected: compilation failure because the Agent runtime does not exist.

- [ ] **Step 3: Implement resource collection**

Collect CPU/load, memory/swap, filesystem usage, network counters, system identity, startup time, and Agent version without exposing an HTTP listener. Use bounded reads and typed report structs.

- [ ] **Step 4: Implement outbound reporter**

Use an HTTPS client with explicit timeout, no proxy from environment, certificate validation enabled by default, bearer token, timestamp, request ID, exponential backoff, and bounded response-body reads. Never log Authorization headers or token values.

- [ ] **Step 5: Implement structured scheduler**

Accept only the protocol task kinds, validate each task again locally, limit concurrent work, run resource reporting every 30 seconds, and execute configured checks according to their interval. There must be no `os/exec`, shell string, SSH library, terminal package, file-manager package, or MCP package in the Agent module.

- [ ] **Step 6: Implement systemd installer**

`deploy/install-agent.sh` must require an explicit endpoint and one-time registration token, download a pinned release artifact, verify a supplied SHA-256 value, create `probewatch-agent` user, write config with mode `0600`, and install a unit with:

```ini
User=probewatch-agent
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=read-only
CapabilityBoundingSet=CAP_NET_RAW
AmbientCapabilities=CAP_NET_RAW
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
```

The installer must not enable unattended updates and must not expose a listener.

- [ ] **Step 7: Run Agent tests and static checks**

```powershell
go test ./tests -v
```

Expected: all tests and vet checks pass.

## Task 8: Wire the HTTP Server and Embed the Frontend

**Files:**
- Create: `internal/api/server.go`
- Create: `web/embed.go`
- Modify: `cmd/probewatch/main.go`
- Modify: `frontend/vite.config.js`
- Modify: `frontend/package.json`
- Create: `tests/integration_test.go`

- [ ] **Step 1: Write failing integration tests**

Start an in-memory SQLite server with fake GitHub provider and verify `/healthz`, OAuth redirect, protected API 401, Agent registration, report persistence, and static SPA serving.

- [ ] **Step 2: Run integration tests to verify failure**

```powershell
go test ./tests -race -v
```

Expected: compilation failure because the server constructor and embedded frontend are absent.

- [ ] **Step 3: Implement server construction and routes**

Build one `http.ServeMux`, apply security headers and request limits, register auth routes, admin routes, Agent routes, `/healthz`, and static SPA fallback. Do not expose database files, `.env`, source maps, or arbitrary filesystem paths.

- [ ] **Step 4: Configure Vite development proxy**

Proxy `/api` and `/auth` to `http://127.0.0.1:8080`; keep browser requests same-origin. Remove demo-only “未连接主控” state from the production path while preserving explicit API loading/error states.

- [ ] **Step 5: Embed production assets**

Build the frontend into a controlled `web/dist` directory and use `go:embed`. The Go server must serve `index.html` only for known SPA paths and must return 404 for unknown asset paths; it must never serve repository root files.

- [ ] **Step 6: Run integration tests**

```powershell
```

Expected: OAuth, protected API, Agent registration/report, health, static asset, and path-isolation tests pass.

## Task 9: Replace Demo Frontend Data with Real API States

**Files:**
- Create: `frontend/src/api/client.js`
- Create: `frontend/src/api/errors.js`
- Create: `frontend/src/components/ConnectionState.jsx`
- Create: `frontend/src/pages/LoginPage.jsx`
- Modify: `frontend/src/main.jsx`
- Modify: `frontend/src/components/NodeList.jsx`
- Modify: `frontend/src/components/NodeDrawer.jsx`
- Modify: `frontend/src/pages/OverviewPage.jsx`
- Modify: `frontend/src/pages/NodesPage.jsx`
- Modify: `frontend/src/pages/ChecksPage.jsx`
- Modify: `frontend/src/pages/MtrPage.jsx`
- Modify: `frontend/src/pages/MediaPage.jsx`
- Modify: `frontend/tests/browser_regression.py`

- [ ] **Step 1: Write failing browser checks**

Add Playwright checks for loading state, 401 redirect to GitHub, API failure state, empty node state, real node rendering from a mocked API response, and absence of hardcoded production IPs.

- [ ] **Step 2: Run browser checks to verify failure**

```powershell
python frontend/tests/browser_regression.py
```

Expected: new API-state checks fail because the frontend still uses hardcoded demo data.

- [ ] **Step 3: Implement API client**

Use same-origin `fetch`, `credentials: 'include'`, `AbortController` timeout, JSON content checks, 401 handling, 403/429/5xx typed errors, and no token storage in localStorage/sessionStorage. Read CSRF token before write requests.

- [ ] **Step 4: Replace demo state with API state**

On initial load show “正在连接主控”; on 401 show the GitHub login action; on timeout show “主控不可达”; on empty data show an empty state; on API success derive metric counts from returned nodes. Never display fabricated online counts or “同步正常”.

- [ ] **Step 5: Add real check-management forms**

Create forms for network targets, MTR targets, and media HTTP detectors. Client-side validation improves UX but server validation remains authoritative. Forms must not accept shell, headers, cookies, scripts, or arbitrary paths.

- [ ] **Step 6: Run frontend validation**

```powershell
npm run build
npm audit --include=dev --audit-level=low
python frontend/tests/browser_regression.py
python -m pytest frontend/tests/security_contract.py -q
```

Expected: build succeeds, audit reports zero known vulnerabilities, browser checks pass, and security contracts pass.

## Task 10: Add Local End-to-End Runbook and Acceptance Harness

**Files:**
- Create: `Makefile`
- Create: `scripts/run-local.ps1`
- Create: `scripts/stop-local.ps1`
- Create: `scripts/create-dev-oauth-app.md`
- Create: `docs/LOCAL-VALIDATION.md`
- Modify: `frontend/README.md`
- Modify: `frontend/SECURITY.md`

- [ ] **Step 1: Document GitHub OAuth local setup**

Document the callback URL `http://127.0.0.1:8080/auth/github/callback`, required environment variables, allowed-user configuration, data directory permissions, and the fact that real GitHub credentials must never be committed.

- [ ] **Step 2: Add local lifecycle commands**

Provide:

```powershell
.\scripts\run-local.ps1
.\scripts\stop-local.ps1
go test ./...
npm --prefix frontend run build
```

The run script must fail if required OAuth variables are absent, bind development services to loopback, and print the frontend URL plus Agent registration endpoint without printing secrets.

- [ ] **Step 3: Add an integration acceptance harness**

The harness must register a fake Agent, submit a signed resource report, submit TCP/HTTP/DNS/MTR/media results, query the admin API through a test session, revoke the node token, and confirm subsequent reports return 401.

- [ ] **Step 4: Run the complete acceptance set**

```powershell
go vet ./...
npm --prefix frontend audit --include=dev --audit-level=low
python frontend/tests/browser_regression.py
python -m pytest frontend/tests/security_contract.py -q
```

Expected: all Go tests and vet checks pass, the frontend builds, npm audit reports zero known vulnerabilities, and browser/security checks pass.

## Task 11: Final Security Review and Local Release Artifact

**Files:**
- Create: `docs/RELEASE-CHECKLIST.md`
- Create: `deploy/SHA256SUMS.example`
- Modify: `docs/LOCAL-VALIDATION.md`

- [ ] **Step 1: Scan for forbidden functionality**

Run a repository scan that fails if production Agent code contains `os/exec`, `exec.Command`, SSH libraries, terminal/file-manager packages, MCP imports, `net.Listen`, `http.ListenAndServe`, or `0.0.0.0` binds. Test files may mention forbidden strings only inside explicit security tests.

- [ ] **Step 2: Scan for secrets and real node data**

Run a scan excluding dependency/build directories that fails on private-key markers, bearer tokens, GitHub secrets, real VPS IPs, `.env` files, and plaintext Agent tokens. Keep sample values in `.env.example` empty.

- [ ] **Step 3: Verify release artifacts**

Build pinned `linux/amd64` and `linux/arm64` Agent artifacts, generate SHA-256 sums, and verify installation uses the supplied checksum. Do not enable automatic update.

- [ ] **Step 4: Complete release checklist**

Confirm:

- GitHub OAuth allowlist works.
- OAuth state is single-use.
- Session cookies are HttpOnly and SameSite.
- Agent tokens are hashed at rest.
- Token rotation and revocation work.
- No Agent listener exists.
- Agent runs as non-root.
- MTR is bounded and uses no shell.
- SSRF targets are blocked.
- Frontend has loading/error/empty/401 states.
- No SSH, terminal, files, exec, MCP, or auto-update exists.

## Execution Notes

- Do not deploy or modify the current Lite installation during this plan.
- Do not put real GitHub credentials, Agent tokens, or VPS IPs in source, tests, `.env.example`, screenshots, or documentation.
- Do not commit changes unless the user explicitly requests a commit.
- Because the current machine does not have Go installed, install and pin a Go toolchain before Task 1 verification; all Go tasks remain blocked until `go version` succeeds.
