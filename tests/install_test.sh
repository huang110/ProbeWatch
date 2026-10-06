#!/bin/sh
# ==============================================================================
# ProbeWatch deploy/install.sh Automated Test Suite
# Tests parameter parsing matrix for remote control flags, validation, and dry-run.
# ==============================================================================
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL_SCRIPT="${SCRIPT_DIR}/deploy/install.sh"

echo "=== 1. Shell Syntax Check ==="
sh -n "${INSTALL_SCRIPT}"
echo "[PASS] sh -n passed"
if command -v bash >/dev/null 2>&1; then
    bash -n "${INSTALL_SCRIPT}"
    echo "[PASS] bash -n passed"
fi

echo "=== 2. Help Output Check ==="
HELP_OUT=$(sh "${INSTALL_SCRIPT}" --help)
echo "${HELP_OUT}" | grep -q -- "--enable-remote-control" || { echo "FAIL: --help missing --enable-remote-control"; exit 1; }
echo "${HELP_OUT}" | grep -q -- "--enable-remote-control=true|false" || { echo "FAIL: --help missing --enable-remote-control=true|false"; exit 1; }
echo "${HELP_OUT}" | grep -q -- "--remote-control=true|false" || { echo "FAIL: --help missing --remote-control=true|false"; exit 1; }
echo "${HELP_OUT}" | grep -q "默认: false" || { echo "FAIL: --help missing default false notice"; exit 1; }
echo "[PASS] Help text verified"

DUMMY_EP="https://test.example.com/api/agent/v1"
DUMMY_UUID="00000000-0000-0000-0000-000000000001"
DUMMY_TOK="test-token-safe-12345"

test_variant() {
    desc="$1"
    flag="$2"
    expected_val="$3"

    out=$(sh "${INSTALL_SCRIPT}" --endpoint "${DUMMY_EP}" --uuid "${DUMMY_UUID}" --token "${DUMMY_TOK}" ${flag} --dry-run 2>&1)
    echo "${out}" | grep -q "REMOTE_CONTROL_ENABLED: ${expected_val}" || {
        echo "FAIL [${desc}]: expected ${expected_val}, got output:\n${out}"
        exit 1
    }
    echo "[PASS] ${desc} -> REMOTE_CONTROL_ENABLED: ${expected_val}"
}

test_equal_variant() {
    desc="$1"
    flag="$2"
    expected_val="$3"

    out=$(sh "${INSTALL_SCRIPT}" --endpoint="${DUMMY_EP}" --uuid="${DUMMY_UUID}" --token="${DUMMY_TOK}" "${flag}" --dry-run 2>&1)
    echo "${out}" | grep -q "REMOTE_CONTROL_ENABLED: ${expected_val}" || {
        echo "FAIL [${desc}]: expected ${expected_val}, got output:\n${out}"
        exit 1
    }
    echo "[PASS] ${desc} -> REMOTE_CONTROL_ENABLED: ${expected_val}"
}

echo "=== 3. Testing True Variants ==="
test_variant "Standalone --enable-remote-control" "--enable-remote-control" "true"
test_equal_variant "--enable-remote-control=true" "--enable-remote-control=true" "true"
test_equal_variant "--remote-control=true" "--remote-control=true" "true"
test_equal_variant "--enable-remote-control=1" "--enable-remote-control=1" "true"
test_equal_variant "--enable-remote-control=yes" "--enable-remote-control=yes" "true"
test_equal_variant "--enable-remote-control=on" "--enable-remote-control=on" "true"
test_equal_variant "--enable-remote-control=TRUE (upper)" "--enable-remote-control=TRUE" "true"
test_variant "Two-part --enable-remote-control true" "--enable-remote-control true" "true"
test_variant "Two-part --remote-control true" "--remote-control true" "true"

echo "=== 4. Testing False Variants ==="
test_equal_variant "--enable-remote-control=false" "--enable-remote-control=false" "false"
test_equal_variant "--remote-control=false" "--remote-control=false" "false"
test_equal_variant "--enable-remote-control=0" "--enable-remote-control=0" "false"
test_equal_variant "--enable-remote-control=no" "--enable-remote-control=no" "false"
test_equal_variant "--enable-remote-control=off" "--enable-remote-control=off" "false"
test_equal_variant "--enable-remote-control=FALSE (upper)" "--enable-remote-control=FALSE" "false"
test_variant "Two-part --enable-remote-control false" "--enable-remote-control false" "false"
test_variant "Two-part --remote-control false" "--remote-control false" "false"

echo "=== 5. Testing Default Behavior (Omitted Flag) ==="
out=$(sh "${INSTALL_SCRIPT}" --endpoint "${DUMMY_EP}" --uuid "${DUMMY_UUID}" --token "${DUMMY_TOK}" --dry-run 2>&1)
echo "${out}" | grep -q "REMOTE_CONTROL_ENABLED: false" || {
    echo "FAIL: expected false by default, got:\n${out}"
    exit 1
}
echo "[PASS] Default behavior verified -> REMOTE_CONTROL_ENABLED: false"

echo "=== 6. Testing Invalid Values (Expected Exit Code 2) ==="
test_invalid() {
    desc="$1"
    shift
    set +e
    out=$(sh "${INSTALL_SCRIPT}" --endpoint "${DUMMY_EP}" --uuid "${DUMMY_UUID}" --token "${DUMMY_TOK}" "$@" --dry-run 2>&1)
    code=$?
    set -e
    if [ "$code" -ne 2 ]; then
        echo "FAIL [${desc}]: expected exit code 2, got ${code}. Output:\n${out}"
        exit 1
    fi
    echo "${out}" | grep -q "远程管理参数值无效，请使用 true/false、1/0、yes/no 或 on/off" || {
        echo "FAIL [${desc}]: error message did not match expected wording. Output:\n${out}"
        exit 1
    }
    echo "[PASS] ${desc} correctly failed with exit code 2"
}

test_invalid "--enable-remote-control=invalid" "--enable-remote-control=invalid"
test_invalid "--enable-remote-control=abc" "--enable-remote-control=abc"
test_invalid "--remote-control=wrong" "--remote-control=wrong"
test_invalid "Two-part --enable-remote-control invalid" "--enable-remote-control" "invalid"

echo "=== 7. Testing Missing Required Parameters (Expected Exit Code 1) ==="
set +e
out=$(sh "${INSTALL_SCRIPT}" --uuid "${DUMMY_UUID}" --token "${DUMMY_TOK}" --dry-run 2>&1)
code=$?
set -e
if [ "$code" -ne 1 ]; then
    echo "FAIL: missing endpoint expected exit code 1, got ${code}"
    exit 1
fi
echo "[PASS] Missing endpoint correctly reported error"

set +e
out=$(sh "${INSTALL_SCRIPT}" --unknown-arg 2>&1)
code=$?
set -e
if [ "$code" -ne 2 ]; then
    echo "FAIL: unknown argument expected exit code 2, got ${code}"
    exit 1
fi
echo "[PASS] Unknown argument correctly failed with exit code 2"

echo "=============================================="
echo " ALL INSTALL SCRIPT TESTS PASSED SUCCESSFULLY!"
echo "=============================================="
