#!/usr/bin/env bash
# ==============================================================================
# ProbeWatch Safe Deployment Script
# 固化生产环境发布流程为严格安全顺序：
# 1. 检查新二进制存在且大于 1MB
# 2. 复制旧二进制到带提交号和时间戳的备份文件
# 3. 原子替换新二进制（同文件系统临时文件 mv -f）
# 4. 设置标准权限与属组 (chmod 755, chown probewatch:probewatch)
# 5. 重启服务 (systemctl restart probewatch.service)
# 6. 检查服务存活状态 (systemctl is-active probewatch.service)
# 7. 检查本地与公网 healthz 探活接口
# ==============================================================================
set -euo pipefail

STAGE_DIR="${STAGE_DIR:-/opt/probewatch/.deploy-stage-current}"
TARGET_DIR="${TARGET_DIR:-/opt/probewatch}"
TARGET_BIN="${TARGET_DIR}/probewatch"
NEW_BIN="${1:-${STAGE_DIR}/probewatch-bin-new}"
SERVICE_NAME="probewatch.service"
LOCAL_HEALTHZ="http://127.0.0.1:8080/healthz"
PUBLIC_HEALTHZ="https://tz.115yu.us.ci/healthz"
MIN_BYTES=1048576 # 1 MB (1048576 bytes)

echo "=================================================="
echo " Starting ProbeWatch Safe Deployment Sequence"
echo "=================================================="

# ------------------------------------------------------------------------------
# 步骤 1: 检查新二进制存在且大于 1MB
# ------------------------------------------------------------------------------
echo "[1/7] Checking new binary at: ${NEW_BIN}"
if [ ! -f "${NEW_BIN}" ]; then
  echo "[-] ERROR: Candidate binary does not exist: ${NEW_BIN}" >&2
  exit 1
fi

BIN_SIZE=$(stat -c%s "${NEW_BIN}" 2>/dev/null || stat -f%z "${NEW_BIN}" 2>/dev/null || wc -c < "${NEW_BIN}")
if [ "${BIN_SIZE}" -lt "${MIN_BYTES}" ]; then
  echo "[-] ERROR: Candidate binary size (${BIN_SIZE} bytes) is below minimum required 1MB (${MIN_BYTES} bytes)" >&2
  exit 1
fi
echo "[+] Step 1 PASSED: New binary verified (${BIN_SIZE} bytes, >= 1MB)"

# ------------------------------------------------------------------------------
# 步骤 2: 复制旧二进制到备份文件
# ------------------------------------------------------------------------------
TIMESTAMP=$(date +%Y%m%d%H%M%S)
COMMIT="${DEPLOY_COMMIT:-}"
if [ -z "${COMMIT}" ] && [ -f "${STAGE_DIR}/.commit" ]; then
  COMMIT=$(head -n 1 "${STAGE_DIR}/.commit" 2>/dev/null | tr -d ' \r\n' | cut -c1-7 || true)
fi
if [ -z "${COMMIT}" ] && [ -d "${STAGE_DIR}/.git" ]; then
  COMMIT=$(git -C "${STAGE_DIR}" rev-parse --short HEAD 2>/dev/null || true)
fi
if [ -z "${COMMIT}" ] && [ -f "${TARGET_DIR}/CURRENT_COMMIT" ]; then
  COMMIT=$(head -n 1 "${TARGET_DIR}/CURRENT_COMMIT" 2>/dev/null | tr -d ' \r\n' | cut -c1-7 || true)
fi
COMMIT="${COMMIT:-manual}"
BACKUP_FILE="${TARGET_DIR}/probewatch.backup-pre-${COMMIT}-${TIMESTAMP}"

echo "[2/7] Backing up existing binary to: ${BACKUP_FILE}"
if [ -f "${TARGET_BIN}" ]; then
  cp -p "${TARGET_BIN}" "${BACKUP_FILE}"
  BACKUP_SIZE=$(stat -c%s "${BACKUP_FILE}" 2>/dev/null || wc -c < "${BACKUP_FILE}")
  echo "[+] Step 2 PASSED: Existing binary backed up (${BACKUP_SIZE} bytes)"
else
  echo "[*] Step 2 NOTICE: Target binary did not exist prior to this run, skipping backup"
fi

# ------------------------------------------------------------------------------
# 步骤 3 & 4: 原子替换新二进制，并设置 chmod 755 与 chown probewatch:probewatch
# ------------------------------------------------------------------------------
echo "[3/7] Atomically replacing binary at: ${TARGET_BIN}"
TMP_BIN="${TARGET_DIR}/.probewatch.tmp.$$"
cp -f "${NEW_BIN}" "${TMP_BIN}"

echo "[4/7] Setting permissions and ownership..."
chmod 755 "${TMP_BIN}"
if id -u probewatch >/dev/null 2>&1; then
  chown probewatch:probewatch "${TMP_BIN}"
fi

# 原子重命名替换
mv -f "${TMP_BIN}" "${TARGET_BIN}"
echo "[+] Step 3 & 4 PASSED: Binary atomically replaced with 0755 permissions"

# ------------------------------------------------------------------------------
# 步骤 5: 重启服务
# ------------------------------------------------------------------------------
echo "[5/7] Restarting ${SERVICE_NAME}..."
systemctl restart "${SERVICE_NAME}"

# ------------------------------------------------------------------------------
# 步骤 6: 检查 systemctl is-active
# ------------------------------------------------------------------------------
echo "[6/7] Checking systemctl is-active for ${SERVICE_NAME}..."
sleep 2
ACTIVE_STATE=$(systemctl is-active "${SERVICE_NAME}" 2>/dev/null || true)
if [ "${ACTIVE_STATE}" != "active" ]; then
  echo "[-] ERROR: Service ${SERVICE_NAME} failed to enter active state (current: '${ACTIVE_STATE}')" >&2
  systemctl status "${SERVICE_NAME}" --no-pager >&2
  exit 1
fi
echo "[+] Step 6 PASSED: Service is active"

# ------------------------------------------------------------------------------
# 步骤 7: 检查本地和公网 healthz
# ------------------------------------------------------------------------------
echo "[7/7] Checking healthz endpoints..."
echo "      -> Probing local: ${LOCAL_HEALTHZ}"
LOCAL_RESP=""
for i in {1..5}; do
  LOCAL_RESP=$(curl -s -S -f --max-time 3 "${LOCAL_HEALTHZ}" 2>/dev/null || true)
  if [ "${LOCAL_RESP}" = "ok" ]; then
    break
  fi
  sleep 1
done

if [ "${LOCAL_RESP}" != "ok" ]; then
  echo "[-] ERROR: Local healthz check failed (expected 'ok', got '${LOCAL_RESP}')" >&2
  exit 1
fi
echo "[+] Local healthz OK ('ok')"

echo "      -> Probing public: ${PUBLIC_HEALTHZ}"
PUBLIC_RESP=""
for i in {1..5}; do
  PUBLIC_RESP=$(curl -s -S -f --max-time 5 "${PUBLIC_HEALTHZ}" 2>/dev/null || true)
  if [ "${PUBLIC_RESP}" = "ok" ]; then
    break
  fi
  sleep 1
done

if [ "${PUBLIC_RESP}" != "ok" ]; then
  echo "[-] ERROR: Public healthz check failed (expected 'ok', got '${PUBLIC_RESP}')" >&2
  exit 1
fi
if [ -n "${DEPLOY_COMMIT:-}" ]; then
  echo "${DEPLOY_COMMIT}" > "${TARGET_DIR}/CURRENT_COMMIT"
elif [ -f "${STAGE_DIR}/.commit" ]; then
  cp -f "${STAGE_DIR}/.commit" "${TARGET_DIR}/CURRENT_COMMIT"
fi

echo "=================================================="
echo " Safe Deployment Sequence Completed Successfully!"
echo "=================================================="
