# ProbeWatch Local Validation

This document covers local validation of the control plane, outbound Agent, and local container layout. The control-plane binary starts a real HTTP server. The Agent runtime is outbound-only: it requires `PROBEWATCH_AGENT_ENDPOINT`, `PROBEWATCH_AGENT_NODE_UUID`, and `PROBEWATCH_AGENT_NODE_TOKEN`, pulls only enabled structured checks, reports resource snapshots every 30 seconds, and never opens a listener or executes shell commands.

## Configuration

Development defaults are safe for local process use:

- `PROBEWATCH_LISTEN=127.0.0.1:8080`
- `PROBEWATCH_DATABASE=./data/probewatch.db`
- `PROBEWATCH_PUBLIC_BASE_URL=http://127.0.0.1:8080`
- `PROBEWATCH_TOKEN_PEPPER` (development uses a clearly non-production built-in fallback; set a stable value for persistent local data)
- `AGENT_TOKEN_TTL=15m`
- `AGENT_NODE_TOKEN_TTL=8760h` (365 days)
- `AGENT_CLOCK_SKEW=5m`
- `MAX_REQUEST_BODY=1MiB`
- `AGENT_IGNORE_UNSAFE_CERT=false`

The configuration loader reads environment variables only during startup. Request handling must consume the loaded config rather than reading the process environment again.

## Production Variables

Production requires these non-empty values:

```text
PROBEWATCH_ENV=production
PROBEWATCH_PUBLIC_BASE_URL=https://monitor.example.test
GITHUB_CLIENT_ID=<provider client id>
GITHUB_CLIENT_SECRET=<provider client secret>
GITHUB_REDIRECT_URL=https://monitor.example.test/auth/github/callback
SESSION_SECRET=<at least 32 random bytes>
PROBEWATCH_TOKEN_PEPPER=<at least 32 random bytes>
AGENT_NODE_TOKEN_TTL=<positive duration no greater than 17520h (730 days)>
```

Production also requires at least one authorization policy:

```text
GITHUB_ALLOWED_USERS=alice,bob
```

or:

```text
GITHUB_ALLOWED_ORG=example-org
```

`AGENT_TOKEN_TTL` must be positive and no greater than `24h`. `AGENT_NODE_TOKEN_TTL` must be explicitly set to a positive duration in production and must be no greater than `17520h` (two 365-day years). Development defaults it to `8760h` (365 days). `AGENT_CLOCK_SKEW` must be positive and no greater than `15m`. `MAX_REQUEST_BODY` must be positive and no greater than `16MiB`. These upper bounds apply in development and production.

## Public URL And OAuth Redirect

In production, `PROBEWATCH_PUBLIC_BASE_URL` and `GITHUB_REDIRECT_URL` must both be absolute HTTPS URLs with:

- A non-empty hostname.
- A port between `1` and `65535` when a port is specified.
- No userinfo, query string, or fragment.
- No bare trailing `?` or `#` delimiter.

Their origins must match exactly, including hostname and port. For example, these origins match:

```text
PROBEWATCH_PUBLIC_BASE_URL=https://monitor.example.test:8443
GITHUB_REDIRECT_URL=https://monitor.example.test:8443/auth/github/callback
```

These do not match because the ports differ:

```text
PROBEWATCH_PUBLIC_BASE_URL=https://monitor.example.test:8443
GITHUB_REDIRECT_URL=https://monitor.example.test/auth/github/callback
```

## Listen Safety

Listen addresses are loopback-only by default. A non-loopback address such as `0.0.0.0:8080` requires both settings:

```text
PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN=true
PROBEWATCH_DEPLOYMENT_MODE=container
```

The deployment mode must be `container` or `trusted-proxy` whenever the non-loopback opt-in is enabled for a non-loopback listen address. The mode is required in development and production; `0.0.0.0` is never silently accepted.

## Local Compose

`deploy/docker-compose.local.yml` deliberately separates host exposure from container listening:

- Host publication is `127.0.0.1:8080:8080`, so the API is not exposed on external interfaces.
- The process inside the container listens on `0.0.0.0:8080`, allowing Docker's published port to reach it.
- Compose explicitly sets `PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN=true` and `PROBEWATCH_DEPLOYMENT_MODE=container` for that internal bind.
- Only repository-root `./data` is mounted, expressed from the `deploy/` directory as `../data:/app/data`.
- Because the host bind mount replaces the image's `/app/data` directory, create it and grant it to the image's non-root UID before starting Compose. The Dockerfile runs as UID `10001`; the setup below makes the mounted database directory writable without running the application as root.
- The Docker socket, host root, privileged mode, and host networking are not mounted or enabled.

From the repository root, run the following sequence. It enters the `deploy/` directory before using the `../data` source, which is the repository-root `data/` directory:

```sh
cd deploy
mkdir -p ../data
chown 10001:10001 ../data
chmod 700 ../data
docker compose -f docker-compose.local.yml config
docker compose -f docker-compose.local.yml build
docker compose -f docker-compose.local.yml up
```

On Windows PowerShell, the directory can be created with:

```powershell
Set-Location deploy
New-Item -ItemType Directory -Force ..\data
docker compose -f docker-compose.local.yml config
docker compose -f docker-compose.local.yml build
docker compose -f docker-compose.local.yml up
```

The PowerShell `New-Item` command creates the directory but does not automatically set Linux ownership for UID `10001`. Use a Linux host, WSL, or another environment that can apply the `chown` and `chmod` commands before starting the non-root container.

The Dockerfile supplies the sole `/probewatch` `ENTRYPOINT`; Compose does not override it.

The control-plane binary now validates configuration, opens the SQLite store, and runs the HTTP server on `PROBEWATCH_LISTEN`. A local process exposes:

- `GET /healthz`
- `GET /auth/github`
- `GET /auth/github/callback`
- `POST /auth/logout`
- `GET /api/csrf`
- Protected `GET /api/me/protected` and `GET /api/me`
- Protected write-test `POST /api/test/protected`

Protected writes require an authenticated session, `Origin` matching `PROBEWATCH_PUBLIC_BASE_URL`, `Content-Type: application/json`, and the current `X-CSRF-Token`. `GET /api/csrf` is the only refresh method and issues a session-bound token. Refreshes and writes are serialized per auth service; the middleware atomically claims and rotates the token before invoking a write handler, so concurrent requests cannot execute the same token-authorized handler twice. Successful write responses in the 2xx range return the next token in `X-CSRF-Token`; if a refresh was waiting concurrently, the response also includes `X-CSRF-Refresh-Required: true` and the client must call `GET /api/csrf` before another write. Failed handlers consume the submitted token without returning the replacement, so the client must call `GET /api/csrf` before retrying. A refresh response supersedes any older write response token, so clients must use the most recently received replacement. Logout is separate because it ends the session and does not return a CSRF replacement. Previous tokens cannot be reused.

The Agent runtime remains a deliberate stub and returns its explicit not-configured error until the later Agent task. No Agent API or remote-control functionality is enabled by this local control-plane validation.

## Secret Generation

Generate a session secret locally with a cryptographically secure tool and place it only in an untracked environment file or process environment. For example, in Windows PowerShell:

```powershell
$bytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
[Convert]::ToBase64String($bytes)
$rng.Dispose()
```

Use an equivalent approved local generator, such as a password manager or operating-system secure random facility, and ensure the result is at least 32 random bytes. Do not paste credentials into tracked files.

Generate a separate random value of at least 32 bytes for `PROBEWATCH_TOKEN_PEPPER`. Keep it stable for the lifetime of a database because changing it invalidates stored token and session digests.

## Validation Commands

Run from the repository root:

```powershell
& 'C:\Program Files\Go\bin\go.exe' test ./... -count=1 -v
& 'C:\Program Files\Go\bin\go.exe' vet ./...
& 'C:\Program Files\Go\bin\go.exe' build ./cmd/probewatch ./cmd/probewatch-agent
```

`.env.example` contains variable names and safe local defaults only. It must not contain real OAuth credentials, session secrets, or other production credentials.
