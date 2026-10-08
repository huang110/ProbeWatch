#!/usr/bin/env bash
# ==============================================================================
# Isolated verification suite for safe_deploy.sh atomic rollback logic
# Tests all 5 failure & success scenarios without touching production systems.
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_SCRIPT="${SCRIPT_DIR}/safe_deploy.sh"
TEST_BASE=$(mktemp -d /tmp/test-safe-deploy-XXXXXX)

cleanup() {
  rm -rf "${TEST_BASE}"
}
trap cleanup EXIT

echo "=== Starting safe_deploy.sh Isolated Rollback Suite in ${TEST_BASE} ==="

mkdir -p "${TEST_BASE}/target" "${TEST_BASE}/stage" "${TEST_BASE}/nginx/assets" "${TEST_BASE}/bin"

# Helper to create dummy 2MB binary
create_binary() {
  local path="$1"
  local content="$2"
  dd if=/dev/zero of="${path}" bs=1024 count=1100 status=none
  echo "${content}" >> "${path}"
  chmod 755 "${path}"
}

export TARGET_DIR="${TEST_BASE}/target"
export STAGE_DIR="${TEST_BASE}/stage"
export NGINX_STATIC_DIR="${TEST_BASE}/nginx"
export MIN_BYTES=1048576
export MIN_FREE_DISK_MB=1
export MAX_BACKUPS_RETAIN=3
export SERVICE_NAME="mock-service"
export LOCAL_HEALTHZ="http://mock-local/healthz"
export PUBLIC_HEALTHZ="http://mock-public/healthz"

# ------------------------------------------------------------------------------
# Test 1: Service start failure triggers automatic rollback
# ------------------------------------------------------------------------------
echo "--- Test 1: Service restart failure triggers rollback ---"
create_binary "${TARGET_DIR}/probewatch" "VERSION_OLD_1"
create_binary "${STAGE_DIR}/probewatch-bin-new" "VERSION_NEW_1"
echo "commit_old_1" > "${TARGET_DIR}/CURRENT_COMMIT"
echo "commit_new_1" > "${STAGE_DIR}/.commit"

# Mock systemctl that fails on first restart (new version), succeeds on second restart (rollback)
cat << 'EOF' > "${TEST_BASE}/bin/systemctl"
#!/usr/bin/env bash
cmd="$1"
svc="$2"
flag="${TEST_BASE}/restart_count"
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
chmod +x "${TEST_BASE}/bin/systemctl"

cat << 'EOF' > "${TEST_BASE}/bin/curl"
#!/usr/bin/env bash
echo "ok"
exit 0
EOF
chmod +x "${TEST_BASE}/bin/curl"

export SYSTEMCTL_CMD="${TEST_BASE}/bin/systemctl"
export CURL_CMD="${TEST_BASE}/bin/curl"

set +e
"${DEPLOY_SCRIPT}" "${STAGE_DIR}/probewatch-bin-new" > "${TEST_BASE}/test1.log" 2>&1
T1_CODE=$?
set -e

if [ "${T1_CODE}" -eq 0 ]; then
  echo "[-] Test 1 FAILED: Expected non-zero exit code on deploy failure, got 0" >&2
  cat "${TEST_BASE}/test1.log" >&2
  exit 1
fi

grep -q "Initiating automatic atomic rollback" "${TEST_BASE}/test1.log" || {
  echo "[-] Test 1 FAILED: Rollback message not found" >&2
  cat "${TEST_BASE}/test1.log" >&2
  exit 1
}

# Verify old binary and old commit are preserved
grep -q "VERSION_OLD_1" "${TARGET_DIR}/probewatch" || {
  echo "[-] Test 1 FAILED: Old binary was not restored!" >&2
  exit 1
}
test "$(cat "${TARGET_DIR}/CURRENT_COMMIT")" = "commit_old_1" || {
  echo "[-] Test 1 FAILED: CURRENT_COMMIT was not restored to old commit!" >&2
  exit 1
}
echo "[+] Test 1 PASSED: Start failure cleanly triggered rollback, restored old binary and commit, exited with code 1"

# ------------------------------------------------------------------------------
# Test 2: Local healthz failure triggers automatic rollback
# ------------------------------------------------------------------------------
echo "--- Test 2: Local healthz failure triggers rollback ---"
rm -f "${TEST_BASE}/restart_count"
create_binary "${TARGET_DIR}/probewatch" "VERSION_OLD_2"
create_binary "${STAGE_DIR}/probewatch-bin-new" "VERSION_NEW_2"
echo "commit_old_2" > "${TARGET_DIR}/CURRENT_COMMIT"

cat << 'EOF' > "${TEST_BASE}/bin/systemctl"
#!/usr/bin/env bash
if [ "$1" = "is-active" ]; then echo "active"; exit 0; fi
exit 0
EOF
chmod +x "${TEST_BASE}/bin/systemctl"

# Mock curl that returns error for new version, ok for rollback check
cat << 'EOF' > "${TEST_BASE}/bin/curl"
#!/usr/bin/env bash
flag="${TEST_BASE}/curl_calls"
calls=$(cat "${flag}" 2>/dev/null || echo 0)
calls=$((calls + 1))
echo "${calls}" > "${flag}"
if [ "${calls}" -le 5 ]; then
  # Initial 5 probes fail
  exit 1
fi
# Rollback probe succeeds
echo "ok"
exit 0
EOF
chmod +x "${TEST_BASE}/bin/curl"

set +e
"${DEPLOY_SCRIPT}" "${STAGE_DIR}/probewatch-bin-new" > "${TEST_BASE}/test2.log" 2>&1
T2_CODE=$?
set -e

if [ "${T2_CODE}" -eq 0 ]; then
  echo "[-] Test 2 FAILED: Expected exit 1, got 0" >&2
  exit 1
fi

grep -q "Local healthz check failed" "${TEST_BASE}/test2.log" || {
  echo "[-] Test 2 FAILED: Local healthz failure not logged" >&2
  cat "${TEST_BASE}/test2.log" >&2
  exit 1
}

grep -q "VERSION_OLD_2" "${TARGET_DIR}/probewatch" || {
  echo "[-] Test 2 FAILED: Old binary not restored after local healthz failure" >&2
  exit 1
}
echo "[+] Test 2 PASSED: Local healthz failure cleanly triggered rollback and restored old binary"

# ------------------------------------------------------------------------------
# Test 3: Public healthz failure triggers automatic rollback
# ------------------------------------------------------------------------------
echo "--- Test 3: Public healthz failure triggers rollback ---"
rm -f "${TEST_BASE}/curl_calls"
create_binary "${TARGET_DIR}/probewatch" "VERSION_OLD_3"
create_binary "${STAGE_DIR}/probewatch-bin-new" "VERSION_NEW_3"
echo "commit_old_3" > "${TARGET_DIR}/CURRENT_COMMIT"

# Local healthz succeeds, public fails, rollback check succeeds
cat << 'EOF' > "${TEST_BASE}/bin/curl"
#!/usr/bin/env bash
url="${!#}"
if [[ "${url}" =~ mock-public ]]; then
  echo "502 Bad Gateway"
  exit 1
fi
echo "ok"
exit 0
EOF
chmod +x "${TEST_BASE}/bin/curl"

set +e
"${DEPLOY_SCRIPT}" "${STAGE_DIR}/probewatch-bin-new" > "${TEST_BASE}/test3.log" 2>&1
T3_CODE=$?
set -e

if [ "${T3_CODE}" -eq 0 ]; then
  echo "[-] Test 3 FAILED: Expected exit 1, got 0" >&2
  exit 1
fi

grep -q "Public healthz check failed" "${TEST_BASE}/test3.log" || {
  echo "[-] Test 3 FAILED: Public healthz failure not logged" >&2
  cat "${TEST_BASE}/test3.log" >&2
  exit 1
}

grep -q "VERSION_OLD_3" "${TARGET_DIR}/probewatch" || {
  echo "[-] Test 3 FAILED: Old binary not restored after public healthz failure" >&2
  exit 1
}
echo "[+] Test 3 PASSED: Public healthz failure cleanly triggered rollback and restored old binary"

# ------------------------------------------------------------------------------
# Test 4: Rollback failure outputs critical diagnostic manual recovery path
# ------------------------------------------------------------------------------
echo "--- Test 4: Rollback failure outputs critical manual recovery instructions ---"
rm -f "${TEST_BASE}/restart_count"
create_binary "${TARGET_DIR}/probewatch" "VERSION_OLD_4"
create_binary "${STAGE_DIR}/probewatch-bin-new" "VERSION_NEW_4"
echo "commit_old_4" > "${TARGET_DIR}/CURRENT_COMMIT"

# Systemctl always fails
cat << 'EOF' > "${TEST_BASE}/bin/systemctl"
#!/usr/bin/env bash
exit 1
EOF
chmod +x "${TEST_BASE}/bin/systemctl"

set +e
"${DEPLOY_SCRIPT}" "${STAGE_DIR}/probewatch-bin-new" > "${TEST_BASE}/test4.log" 2>&1
T4_CODE=$?
set -e

if [ "${T4_CODE}" -eq 0 ]; then
  echo "[-] Test 4 FAILED: Expected exit 1 on rollback failure" >&2
  exit 1
fi

grep -q "Manual recovery instructions" "${TEST_BASE}/test4.log" || {
  echo "[-] Test 4 FAILED: Manual recovery instructions missing" >&2
  cat "${TEST_BASE}/test4.log" >&2
  exit 1
}
echo "[+] Test 4 PASSED: Rollback failure output clear manual recovery steps"

# ------------------------------------------------------------------------------
# Test 5: Successful deployment workflow
# ------------------------------------------------------------------------------
echo "--- Test 5: Successful deployment workflow ---"
create_binary "${TARGET_DIR}/probewatch" "VERSION_OLD_5"
create_binary "${STAGE_DIR}/probewatch-bin-new" "VERSION_NEW_5"
echo "commit_old_5" > "${TARGET_DIR}/CURRENT_COMMIT"
echo "commit_new_5" > "${STAGE_DIR}/.commit"

cat << 'EOF' > "${TEST_BASE}/bin/systemctl"
#!/usr/bin/env bash
if [ "$1" = "is-active" ]; then echo "active"; exit 0; fi
exit 0
EOF
chmod +x "${TEST_BASE}/bin/systemctl"

cat << 'EOF' > "${TEST_BASE}/bin/curl"
#!/usr/bin/env bash
echo "ok"
exit 0
EOF
chmod +x "${TEST_BASE}/bin/curl"

set +e
"${DEPLOY_SCRIPT}" "${STAGE_DIR}/probewatch-bin-new" > "${TEST_BASE}/test5.log" 2>&1
T5_CODE=$?
set -e

if [ "${T5_CODE}" -ne 0 ]; then
  echo "[-] Test 5 FAILED: Expected exit 0 on successful deploy, got ${T5_CODE}" >&2
  cat "${TEST_BASE}/test5.log" >&2
  exit 1
fi

grep -q "VERSION_NEW_5" "${TARGET_DIR}/probewatch" || {
  echo "[-] Test 5 FAILED: New binary not in place after successful deploy" >&2
  exit 1
}
test "$(cat "${TARGET_DIR}/CURRENT_COMMIT")" = "commit_new_5" || {
  echo "[-] Test 5 FAILED: CURRENT_COMMIT not updated to commit_new_5!" >&2
  exit 1
}
echo "[+] Test 5 PASSED: Normal deployment completed cleanly, exit code 0, commit updated"

echo "=================================================="
echo " ALL 5 SAFE DEPLOYMENT & ROLLBACK TESTS PASSED!"
echo "=================================================="
