#!/usr/bin/env bash
# ==============================================================================
# Isolated verification suite for safe_deploy.sh atomic rollback logic
# Tests all failure & success scenarios without touching production systems.
# Each test scenario is strictly isolated with independent directories and counters.
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_SCRIPT="${SCRIPT_DIR}/safe_deploy.sh"
TEST_BASE=$(mktemp -d /tmp/test-safe-deploy-XXXXXX)

cleanup() {
  rm -rf "${TEST_BASE}"
}
trap cleanup EXIT

# CRITICAL: Export TEST_BASE so child subshells can strictly locate test counters
export TEST_BASE="${TEST_BASE}"

echo "=== Starting safe_deploy.sh Isolated Rollback Suite in ${TEST_BASE} ==="

# Helper to create dummy binary
create_binary() {
  local path="$1"
  local content="$2"
  mkdir -p "$(dirname "${path}")"
  dd if=/dev/zero of="${path}" bs=1024 count=1100 status=none
  echo "${content}" >> "${path}"
  chmod 755 "${path}"
}

# ------------------------------------------------------------------------------
# Test 1: Service restart failure triggers automatic rollback
# ------------------------------------------------------------------------------
echo "--- Test 1: Service restart failure triggers rollback ---"
T1_DIR="${TEST_BASE}/t1"
mkdir -p "${T1_DIR}/target" "${T1_DIR}/stage/frontend/dist/assets" "${T1_DIR}/nginx/assets" "${T1_DIR}/bin"

create_binary "${T1_DIR}/target/probewatch" "VERSION_OLD_1"
create_binary "${T1_DIR}/stage/probewatch-bin-new" "VERSION_NEW_1"
echo "commit_old_1" > "${T1_DIR}/target/CURRENT_COMMIT"
echo "commit_new_1" > "${T1_DIR}/stage/.commit"
echo "<!-- old index 1 -->" > "${T1_DIR}/nginx/index.html"
echo "old-js-1" > "${T1_DIR}/nginx/assets/app.js"
echo "<!-- new index 1 -->" > "${T1_DIR}/stage/frontend/dist/index.html"
echo "new-js-1" > "${T1_DIR}/stage/frontend/dist/assets/app.js"

cat << 'EOF' > "${T1_DIR}/bin/systemctl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then
  echo "FATAL: TEST_BASE is not set in child process!" >&2
  exit 99
fi
cmd="$1"
flag="${TEST_BASE}/t1/restart_count"
count=$(cat "${flag}" 2>/dev/null || echo 0)
if [ "${cmd}" = "restart" ]; then
  count=$((count + 1))
  echo "${count}" > "${flag}"
  if [ "${count}" -eq 1 ]; then
    echo "Simulated start failure for new binary" >&2
    exit 1
  fi
  exit 0
fi
if [ "${cmd}" = "is-active" ]; then
  echo "active"
  exit 0
fi
exit 0
EOF
chmod +x "${T1_DIR}/bin/systemctl"

cat << 'EOF' > "${T1_DIR}/bin/curl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then
  echo "FATAL: TEST_BASE is not set in child process!" >&2
  exit 99
fi
echo "ok"
exit 0
EOF
chmod +x "${T1_DIR}/bin/curl"

set +e
(
  export TARGET_DIR="${T1_DIR}/target"
  export STAGE_DIR="${T1_DIR}/stage"
  export NGINX_STATIC_DIR="${T1_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  export MAX_BACKUPS_RETAIN=3
  export SERVICE_NAME="mock-service"
  export LOCAL_HEALTHZ="http://mock-local/healthz"
  export PUBLIC_HEALTHZ="http://mock-public/healthz"
  export SYSTEMCTL_CMD="${T1_DIR}/bin/systemctl"
  export CURL_CMD="${T1_DIR}/bin/curl"
  bash "${DEPLOY_SCRIPT}" "${T1_DIR}/stage/probewatch-bin-new"
) > "${T1_DIR}/test1.log" 2>&1
T1_CODE=$?
set -e

if [ "${T1_CODE}" -eq 0 ]; then
  echo "[-] Test 1 FAILED: Expected exit code 1 on deploy failure, got ${T1_CODE}" >&2
  cat "${T1_DIR}/test1.log" >&2
  exit 1
fi

grep -q "Initiating automatic atomic rollback" "${T1_DIR}/test1.log" || {
  echo "[-] Test 1 FAILED: Rollback message not found" >&2
  cat "${T1_DIR}/test1.log" >&2
  exit 1
}

# Assertions
grep -q "VERSION_OLD_1" "${T1_DIR}/target/probewatch" || {
  echo "[-] Test 1 FAILED: Old binary was not restored!" >&2
  exit 1
}
test "$(cat "${T1_DIR}/target/CURRENT_COMMIT")" = "commit_old_1" || {
  echo "[-] Test 1 FAILED: CURRENT_COMMIT was not restored to commit_old_1!" >&2
  exit 1
}
grep -q "old index 1" "${T1_DIR}/nginx/index.html" || {
  echo "[-] Test 1 FAILED: Nginx index.html was not restored to old index 1!" >&2
  exit 1
}
grep -q "old-js-1" "${T1_DIR}/nginx/assets/app.js" || {
  echo "[-] Test 1 FAILED: Nginx assets/app.js was not restored to old-js-1!" >&2
  exit 1
}
echo "[+] Test 1 PASSED: Start failure triggered rollback, restored old binary/commit/static assets, exit code=${T1_CODE}"

# ------------------------------------------------------------------------------
# Test 2: Local healthz failure triggers automatic rollback
# ------------------------------------------------------------------------------
echo "--- Test 2: Local healthz failure triggers rollback ---"
T2_DIR="${TEST_BASE}/t2"
mkdir -p "${T2_DIR}/target" "${T2_DIR}/stage/frontend/dist/assets" "${T2_DIR}/nginx/assets" "${T2_DIR}/bin"

create_binary "${T2_DIR}/target/probewatch" "VERSION_OLD_2"
create_binary "${T2_DIR}/stage/probewatch-bin-new" "VERSION_NEW_2"
echo "commit_old_2" > "${T2_DIR}/target/CURRENT_COMMIT"
echo "commit_new_2" > "${T2_DIR}/stage/.commit"
echo "<!-- old index 2 -->" > "${T2_DIR}/nginx/index.html"
echo "old-js-2" > "${T2_DIR}/nginx/assets/app.js"
echo "<!-- new index 2 -->" > "${T2_DIR}/stage/frontend/dist/index.html"
echo "new-js-2" > "${T2_DIR}/stage/frontend/dist/assets/app.js"

cat << 'EOF' > "${T2_DIR}/bin/systemctl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
if [ "$1" = "is-active" ]; then echo "active"; exit 0; fi
exit 0
EOF
chmod +x "${T2_DIR}/bin/systemctl"

cat << 'EOF' > "${T2_DIR}/bin/curl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
flag="${TEST_BASE}/t2/curl_calls"
calls=$(cat "${flag}" 2>/dev/null || echo 0)
calls=$((calls + 1))
echo "${calls}" > "${flag}"
if [ "${calls}" -le 5 ]; then
  exit 1
fi
echo "ok"
exit 0
EOF
chmod +x "${T2_DIR}/bin/curl"

set +e
(
  export TARGET_DIR="${T2_DIR}/target"
  export STAGE_DIR="${T2_DIR}/stage"
  export NGINX_STATIC_DIR="${T2_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  export MAX_BACKUPS_RETAIN=3
  export SERVICE_NAME="mock-service"
  export LOCAL_HEALTHZ="http://mock-local/healthz"
  export PUBLIC_HEALTHZ="http://mock-public/healthz"
  export SYSTEMCTL_CMD="${T2_DIR}/bin/systemctl"
  export CURL_CMD="${T2_DIR}/bin/curl"
  bash "${DEPLOY_SCRIPT}" "${T2_DIR}/stage/probewatch-bin-new"
) > "${T2_DIR}/test2.log" 2>&1
T2_CODE=$?
set -e

if [ "${T2_CODE}" -eq 0 ]; then
  echo "[-] Test 2 FAILED: Expected exit code 1, got ${T2_CODE}" >&2
  cat "${T2_DIR}/test2.log" >&2
  exit 1
fi

grep -q "Local healthz check failed" "${T2_DIR}/test2.log" || {
  echo "[-] Test 2 FAILED: Local healthz failure not logged" >&2
  cat "${T2_DIR}/test2.log" >&2
  exit 1
}

grep -q "VERSION_OLD_2" "${T2_DIR}/target/probewatch" || {
  echo "[-] Test 2 FAILED: Old binary not restored after local healthz failure" >&2
  exit 1
}
test "$(cat "${T2_DIR}/target/CURRENT_COMMIT")" = "commit_old_2" || {
  echo "[-] Test 2 FAILED: CURRENT_COMMIT not restored to commit_old_2" >&2
  exit 1
}
grep -q "old index 2" "${T2_DIR}/nginx/index.html" || {
  echo "[-] Test 2 FAILED: index.html not restored after local healthz failure" >&2
  exit 1
}
echo "[+] Test 2 PASSED: Local healthz failure cleanly triggered rollback, exit code=${T2_CODE}"

# ------------------------------------------------------------------------------
# Test 3: Public healthz failure triggers automatic rollback
# ------------------------------------------------------------------------------
echo "--- Test 3: Public healthz failure triggers rollback ---"
T3_DIR="${TEST_BASE}/t3"
mkdir -p "${T3_DIR}/target" "${T3_DIR}/stage/frontend/dist/assets" "${T3_DIR}/nginx/assets" "${T3_DIR}/bin"

create_binary "${T3_DIR}/target/probewatch" "VERSION_OLD_3"
create_binary "${T3_DIR}/stage/probewatch-bin-new" "VERSION_NEW_3"
echo "commit_old_3" > "${T3_DIR}/target/CURRENT_COMMIT"
echo "commit_new_3" > "${T3_DIR}/stage/.commit"
echo "<!-- old index 3 -->" > "${T3_DIR}/nginx/index.html"
echo "old-js-3" > "${T3_DIR}/nginx/assets/app.js"
echo "<!-- new index 3 -->" > "${T3_DIR}/stage/frontend/dist/index.html"
echo "new-js-3" > "${T3_DIR}/stage/frontend/dist/assets/app.js"

cat << 'EOF' > "${T3_DIR}/bin/systemctl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
if [ "$1" = "is-active" ]; then echo "active"; exit 0; fi
exit 0
EOF
chmod +x "${T3_DIR}/bin/systemctl"

cat << 'EOF' > "${T3_DIR}/bin/curl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
url="${!#}"
if [[ "${url}" =~ mock-public ]]; then
  echo "502 Bad Gateway"
  exit 1
fi
echo "ok"
exit 0
EOF
chmod +x "${T3_DIR}/bin/curl"

set +e
(
  export TARGET_DIR="${T3_DIR}/target"
  export STAGE_DIR="${T3_DIR}/stage"
  export NGINX_STATIC_DIR="${T3_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  export MAX_BACKUPS_RETAIN=3
  export SERVICE_NAME="mock-service"
  export LOCAL_HEALTHZ="http://mock-local/healthz"
  export PUBLIC_HEALTHZ="http://mock-public/healthz"
  export SYSTEMCTL_CMD="${T3_DIR}/bin/systemctl"
  export CURL_CMD="${T3_DIR}/bin/curl"
  bash "${DEPLOY_SCRIPT}" "${T3_DIR}/stage/probewatch-bin-new"
) > "${T3_DIR}/test3.log" 2>&1
T3_CODE=$?
set -e

if [ "${T3_CODE}" -eq 0 ]; then
  echo "[-] Test 3 FAILED: Expected exit code 1, got ${T3_CODE}" >&2
  cat "${T3_DIR}/test3.log" >&2
  exit 1
fi

grep -q "Public healthz check failed" "${T3_DIR}/test3.log" || {
  echo "[-] Test 3 FAILED: Public healthz failure not logged" >&2
  cat "${T3_DIR}/test3.log" >&2
  exit 1
}

grep -q "VERSION_OLD_3" "${T3_DIR}/target/probewatch" || {
  echo "[-] Test 3 FAILED: Old binary not restored after public healthz failure" >&2
  exit 1
}
test "$(cat "${T3_DIR}/target/CURRENT_COMMIT")" = "commit_old_3" || {
  echo "[-] Test 3 FAILED: CURRENT_COMMIT not restored to commit_old_3" >&2
  exit 1
}
grep -q "old index 3" "${T3_DIR}/nginx/index.html" || {
  echo "[-] Test 3 FAILED: index.html not restored after public healthz failure" >&2
  exit 1
}
echo "[+] Test 3 PASSED: Public healthz failure cleanly triggered rollback, exit code=${T3_CODE}"

# ------------------------------------------------------------------------------
# Test 4: Rollback failure outputs critical manual recovery instructions
# ------------------------------------------------------------------------------
echo "--- Test 4: Rollback failure outputs critical manual recovery instructions ---"
T4_DIR="${TEST_BASE}/t4"
mkdir -p "${T4_DIR}/target" "${T4_DIR}/stage" "${T4_DIR}/nginx" "${T4_DIR}/bin"

create_binary "${T4_DIR}/target/probewatch" "VERSION_OLD_4"
create_binary "${T4_DIR}/stage/probewatch-bin-new" "VERSION_NEW_4"
echo "commit_old_4" > "${T4_DIR}/target/CURRENT_COMMIT"

cat << 'EOF' > "${T4_DIR}/bin/systemctl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
exit 1
EOF
chmod +x "${T4_DIR}/bin/systemctl"

set +e
(
  export TARGET_DIR="${T4_DIR}/target"
  export STAGE_DIR="${T4_DIR}/stage"
  export NGINX_STATIC_DIR="${T4_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  export MAX_BACKUPS_RETAIN=3
  export SERVICE_NAME="mock-service"
  export LOCAL_HEALTHZ="http://mock-local/healthz"
  export PUBLIC_HEALTHZ="http://mock-public/healthz"
  export SYSTEMCTL_CMD="${T4_DIR}/bin/systemctl"
  bash "${DEPLOY_SCRIPT}" "${T4_DIR}/stage/probewatch-bin-new"
) > "${T4_DIR}/test4.log" 2>&1
T4_CODE=$?
set -e

if [ "${T4_CODE}" -eq 0 ]; then
  echo "[-] Test 4 FAILED: Expected exit code 1 on rollback failure" >&2
  cat "${T4_DIR}/test4.log" >&2
  exit 1
fi

grep -q "Manual recovery instructions" "${T4_DIR}/test4.log" || {
  echo "[-] Test 4 FAILED: Manual recovery instructions missing" >&2
  cat "${T4_DIR}/test4.log" >&2
  exit 1
}
echo "[+] Test 4 PASSED: Rollback failure outputted clear manual recovery steps, exit code=${T4_CODE}"

# ------------------------------------------------------------------------------
# Test 5: Injected static asset failure triggers rollback
# ------------------------------------------------------------------------------
echo "--- Test 5: Injected static asset copy failure triggers rollback ---"
T5_DIR="${TEST_BASE}/t5"
mkdir -p "${T5_DIR}/target" "${T5_DIR}/stage/frontend/dist/assets" "${T5_DIR}/nginx/assets" "${T5_DIR}/bin"

create_binary "${T5_DIR}/target/probewatch" "VERSION_OLD_5"
create_binary "${T5_DIR}/stage/probewatch-bin-new" "VERSION_NEW_5"
echo "commit_old_5" > "${T5_DIR}/target/CURRENT_COMMIT"
echo "<!-- old index 5 -->" > "${T5_DIR}/nginx/index.html"
echo "old-js-5" > "${T5_DIR}/nginx/assets/app.js"
echo "<!-- new index 5 -->" > "${T5_DIR}/stage/frontend/dist/index.html"
cat << 'EOF' > "${T5_DIR}/bin/cp"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
for arg in "$@"; do
  if [[ "${arg}" =~ "frontend/dist" ]]; then
    echo "Simulated cp I/O error copying static assets" >&2
    exit 1
  fi
done
exec /bin/cp "$@"
EOF
chmod +x "${T5_DIR}/bin/cp"

cat << 'EOF' > "${T5_DIR}/bin/systemctl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
if [ "$1" = "is-active" ]; then echo "active"; exit 0; fi
exit 0
EOF
chmod +x "${T5_DIR}/bin/systemctl"

cat << 'EOF' > "${T5_DIR}/bin/curl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
echo "ok"
exit 0
EOF
chmod +x "${T5_DIR}/bin/curl"

set +e
(
  export TARGET_DIR="${T5_DIR}/target"
  export STAGE_DIR="${T5_DIR}/stage"
  export NGINX_STATIC_DIR="${T5_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  export MAX_BACKUPS_RETAIN=3
  export SERVICE_NAME="mock-service"
  export LOCAL_HEALTHZ="http://mock-local/healthz"
  export PUBLIC_HEALTHZ="http://mock-public/healthz"
  export SYSTEMCTL_CMD="${T5_DIR}/bin/systemctl"
  export CURL_CMD="${T5_DIR}/bin/curl"
  export CP_CMD="${T5_DIR}/bin/cp"
  bash "${DEPLOY_SCRIPT}" "${T5_DIR}/stage/probewatch-bin-new"
) > "${T5_DIR}/test5.log" 2>&1
T5_CODE=$?
set -e

if [ "${T5_CODE}" -eq 0 ]; then
  echo "[-] Test 5 FAILED: Expected exit code 1 on static asset copy failure" >&2
  cat "${T5_DIR}/test5.log" >&2
  exit 1
fi

grep -q "Initiating automatic atomic rollback" "${T5_DIR}/test5.log" || {
  echo "[-] Test 5 FAILED: Rollback message not found on static asset failure" >&2
  cat "${T5_DIR}/test5.log" >&2
  exit 1
}

grep -q "VERSION_OLD_5" "${T5_DIR}/target/probewatch" || {
  echo "[-] Test 5 FAILED: Old binary was not preserved after static asset failure" >&2
  exit 1
}
test "$(cat "${T5_DIR}/target/CURRENT_COMMIT")" = "commit_old_5" || {
  echo "[-] Test 5 FAILED: CURRENT_COMMIT was not preserved" >&2
  exit 1
}
echo "[+] Test 5 PASSED: Injected static asset failure triggered rollback, old version preserved, exit code=${T5_CODE}"

# ------------------------------------------------------------------------------
# Test 6: Concurrent deployment lock conflict rejects duplicate run
# ------------------------------------------------------------------------------
echo "--- Test 6: Concurrent deployment lock conflict ---"
T6_DIR="${TEST_BASE}/t6"
mkdir -p "${T6_DIR}/target/.deploy.lock.d" "${T6_DIR}/stage" "${T6_DIR}/nginx"
create_binary "${T6_DIR}/target/probewatch" "VERSION_OLD_6"
create_binary "${T6_DIR}/stage/probewatch-bin-new" "VERSION_NEW_6"

set +e
(
  export TARGET_DIR="${T6_DIR}/target"
  export STAGE_DIR="${T6_DIR}/stage"
  export NGINX_STATIC_DIR="${T6_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  bash "${DEPLOY_SCRIPT}" "${T6_DIR}/stage/probewatch-bin-new"
) > "${T6_DIR}/test6.log" 2>&1
T6_CODE=$?
set -e

if [ "${T6_CODE}" -eq 0 ]; then
  echo "[-] Test 6 FAILED: Expected exit 1 on concurrent deployment lock conflict, got 0" >&2
  cat "${T6_DIR}/test6.log" >&2
  exit 1
fi
grep -q "Another deployment is currently in progress" "${T6_DIR}/test6.log" || {
  echo "[-] Test 6 FAILED: Lock conflict error message missing" >&2
  cat "${T6_DIR}/test6.log" >&2
  exit 1
}
echo "[+] Test 6 PASSED: Concurrency lock prevented collision, exit code=${T6_CODE}"

# ------------------------------------------------------------------------------
# Test 7: Successful deployment workflow
# ------------------------------------------------------------------------------
echo "--- Test 7: Successful deployment workflow ---"
T7_DIR="${TEST_BASE}/t7"
mkdir -p "${T7_DIR}/target" "${T7_DIR}/stage/frontend/dist/assets" "${T7_DIR}/nginx/assets" "${T7_DIR}/bin"

create_binary "${T7_DIR}/target/probewatch" "VERSION_OLD_7"
create_binary "${T7_DIR}/stage/probewatch-bin-new" "VERSION_NEW_7"
echo "commit_old_7" > "${T7_DIR}/target/CURRENT_COMMIT"
echo "commit_new_7" > "${T7_DIR}/stage/.commit"
echo "<!-- old index 7 -->" > "${T7_DIR}/nginx/index.html"
echo "old-js-7" > "${T7_DIR}/nginx/assets/app.js"
echo "<!-- new index 7 -->" > "${T7_DIR}/stage/frontend/dist/index.html"
echo "new-js-7" > "${T7_DIR}/stage/frontend/dist/assets/app.js"

cat << 'EOF' > "${T7_DIR}/bin/systemctl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
if [ "$1" = "is-active" ]; then echo "active"; exit 0; fi
exit 0
EOF
chmod +x "${T7_DIR}/bin/systemctl"

cat << 'EOF' > "${T7_DIR}/bin/curl"
#!/usr/bin/env bash
if [ -z "${TEST_BASE:-}" ]; then echo "FATAL: TEST_BASE missing!" >&2; exit 99; fi
echo "ok"
exit 0
EOF
chmod +x "${T7_DIR}/bin/curl"

set +e
(
  export TARGET_DIR="${T7_DIR}/target"
  export STAGE_DIR="${T7_DIR}/stage"
  export NGINX_STATIC_DIR="${T7_DIR}/nginx"
  export MIN_BYTES=1048576
  export MIN_FREE_DISK_MB=1
  export MAX_BACKUPS_RETAIN=3
  export SERVICE_NAME="mock-service"
  export LOCAL_HEALTHZ="http://mock-local/healthz"
  export PUBLIC_HEALTHZ="http://mock-public/healthz"
  export SYSTEMCTL_CMD="${T7_DIR}/bin/systemctl"
  export CURL_CMD="${T7_DIR}/bin/curl"
  bash "${DEPLOY_SCRIPT}" "${T7_DIR}/stage/probewatch-bin-new"
) > "${T7_DIR}/test7.log" 2>&1
T7_CODE=$?
set -e

if [ "${T7_CODE}" -ne 0 ]; then
  echo "[-] Test 7 FAILED: Expected exit code 0 on successful deploy, got ${T7_CODE}" >&2
  cat "${T7_DIR}/test7.log" >&2
  exit 1
fi

grep -q "VERSION_NEW_7" "${T7_DIR}/target/probewatch" || {
  echo "[-] Test 7 FAILED: New binary not in place after successful deploy" >&2
  exit 1
}
test "$(cat "${T7_DIR}/target/CURRENT_COMMIT")" = "commit_new_7" || {
  echo "[-] Test 7 FAILED: CURRENT_COMMIT not updated to commit_new_7!" >&2
  exit 1
}
grep -q "new index 7" "${T7_DIR}/nginx/index.html" || {
  echo "[-] Test 7 FAILED: new index.html not deployed!" >&2
  exit 1
}
grep -q "new-js-7" "${T7_DIR}/nginx/assets/app.js" || {
  echo "[-] Test 7 FAILED: new assets/app.js not deployed!" >&2
  exit 1
}
echo "[+] Test 7 PASSED: Normal deployment completed cleanly, exit code=0, all assets updated"

echo "=================================================="
echo " ALL 7 SAFE DEPLOYMENT & ROLLBACK TESTS PASSED!"
echo "=================================================="
