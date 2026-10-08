#!/usr/bin/env bash
# ==============================================================================
# ProbeWatch Safe Deployment Script with Automatic Atomic Rollback
# 固化生产环境发布流程为严格安全顺序：
# 1. 预检磁盘空间（可配置最小剩余空间）
# 2. 检查新二进制存在且大于最小尺寸 (MIN_BYTES)
# 3. 备份旧版本二进制、版本标记与静态资源
# 4. 原子替换新二进制（同文件系统临时文件 mv -f）
# 5. 设置标准权限与属组 (0755, probewatch:probewatch)
# 6. 重启服务并检查 active 状态
# 7. 检查本地与公网 healthz 探活接口
# 8. 任一环节失败时，自动原子回滚至旧版本并验证旧版本健康
# 9. 执行历史备份保留策略 (MAX_BACKUPS_RETAIN)
# ==============================================================================
set -euo pipefail

# 可配置参数与合理默认值
STAGE_DIR="${STAGE_DIR:-/opt/probewatch/.deploy-stage-current}"
TARGET_DIR="${TARGET_DIR:-/opt/probewatch}"
TARGET_BIN="${TARGET_DIR}/probewatch"
NEW_BIN="${1:-${STAGE_DIR}/probewatch-bin-new}"
SERVICE_NAME="${SERVICE_NAME:-probewatch.service}"
LOCAL_HEALTHZ="${LOCAL_HEALTHZ:-http://127.0.0.1:8080/healthz}"
PUBLIC_HEALTHZ="${PUBLIC_HEALTHZ:-https://tz.115yu.us.ci/healthz}"
MIN_BYTES="${MIN_BYTES:-1048576}" # 默认最小 1MB
MIN_FREE_DISK_MB="${MIN_FREE_DISK_MB:-100}" # 默认目标分区至少保留 100MB 空间
MAX_BACKUPS_RETAIN="${MAX_BACKUPS_RETAIN:-10}" # 默认保留最新 10 个历史备份
SKIP_PUBLIC_HEALTHZ="${SKIP_PUBLIC_HEALTHZ:-0}"
NGINX_STATIC_DIR="${NGINX_STATIC_DIR:-/var/www/probewatch}"

SYSTEMCTL_CMD="${SYSTEMCTL_CMD:-systemctl}"
CURL_CMD="${CURL_CMD:-curl}"

TIMESTAMP=$(date +%Y%m%d%H%M%S)
BACKUP_FILE=""
PREV_COMMIT_BACKUP=""
STATIC_BACKUP_DIR=""
TMP_BIN=""

# ------------------------------------------------------------------------------
# 自动原子回滚函数
# ------------------------------------------------------------------------------
rollback() {
  local reason="$1"
  echo "==================================================" >&2
  echo "[!] DEPLOYMENT FAILED: ${reason}" >&2
  echo "[!] Initiating automatic atomic rollback sequence..." >&2
  echo "==================================================" >&2

  # 清理未完成的替换临时文件
  if [ -n "${TMP_BIN:-}" ] && [ -f "${TMP_BIN}" ]; then
    rm -f "${TMP_BIN}"
  fi

  local rollback_ok=1

  # 1. 恢复二进制
  if [ -n "${BACKUP_FILE:-}" ] && [ -f "${BACKUP_FILE}" ]; then
    echo "[*] Restoring previous binary from: ${BACKUP_FILE}" >&2
    local restore_tmp="${TARGET_DIR}/.probewatch.rollback.$$"
    if cp -f "${BACKUP_FILE}" "${restore_tmp}" && chmod 755 "${restore_tmp}"; then
      if id -u probewatch >/dev/null 2>&1; then
        chown probewatch:probewatch "${restore_tmp}" 2>/dev/null || true
      fi
      if mv -f "${restore_tmp}" "${TARGET_BIN}"; then
        echo "[+] Previous binary restored atomically" >&2
      else
        echo "[-] CRITICAL: Failed to atomic-rename binary during rollback" >&2
        rollback_ok=0
      fi
    else
      echo "[-] CRITICAL: Failed to stage rollback binary" >&2
      rollback_ok=0
    fi
  else
    echo "[*] Notice: No previous binary backup existed to restore" >&2
  fi

  # 2. 恢复版本标记
  if [ -n "${PREV_COMMIT_BACKUP:-}" ] && [ -f "${PREV_COMMIT_BACKUP}" ]; then
    echo "[*] Restoring CURRENT_COMMIT to previous version..." >&2
    cp -f "${PREV_COMMIT_BACKUP}" "${TARGET_DIR}/CURRENT_COMMIT"
    rm -f "${PREV_COMMIT_BACKUP}"
  fi

  # 3. 恢复静态资源
  if [ -n "${STATIC_BACKUP_DIR:-}" ] && [ -d "${STATIC_BACKUP_DIR}" ] && [ -d "${NGINX_STATIC_DIR}" ]; then
    echo "[*] Restoring Nginx static assets..." >&2
    if rm -rf "${NGINX_STATIC_DIR}/assets" && cp -a "${STATIC_BACKUP_DIR}" "${NGINX_STATIC_DIR}/assets"; then
      echo "[+] Nginx static assets restored" >&2
    else
      echo "[-] WARNING: Failed to fully restore static assets" >&2
    fi
    rm -rf "${STATIC_BACKUP_DIR}"
  fi

  # 4. 重启旧版本服务
  echo "[*] Restarting ${SERVICE_NAME} to apply rolled-back binary..." >&2
  if ${SYSTEMCTL_CMD} restart "${SERVICE_NAME}" 2>/dev/null; then
    sleep 2
    local restored_state
    restored_state=$(${SYSTEMCTL_CMD} is-active "${SERVICE_NAME}" 2>/dev/null || true)
    if [ "${restored_state}" = "active" ]; then
      echo "[+] Restored service is active" >&2
      # 验证旧版本探活
      local restored_health=""
      for i in {1..5}; do
        restored_health=$(${CURL_CMD} -s -S -f --max-time 3 "${LOCAL_HEALTHZ}" 2>/dev/null || true)
        if [ "${restored_health}" = "ok" ]; then
          break
        fi
        sleep 1
      done
      if [ "${restored_health}" = "ok" ]; then
        echo "[+] Rolled-back service local healthz verified: OK" >&2
      else
        echo "[-] WARNING: Rolled-back service is active but healthz returned: '${restored_health}'" >&2
        rollback_ok=0
      fi
    else
      echo "[-] CRITICAL: Restored service is not active (state: '${restored_state}')" >&2
      rollback_ok=0
    fi
  else
    echo "[-] CRITICAL: Failed to restart service during rollback" >&2
    rollback_ok=0
  fi

  if [ "${rollback_ok}" -eq 1 ]; then
    echo "==================================================" >&2
    echo "[+] Rollback completed successfully. Service remains operational on previous version." >&2
    echo "[-] Deployment terminated with failure code (1)." >&2
    echo "==================================================" >&2
  else
    echo "==================================================" >&2
    echo "[-] CRITICAL: Automatic rollback encountered errors!" >&2
    echo "    Manual recovery instructions:" >&2
    echo "    1. Check logs: journalctl -u ${SERVICE_NAME} -n 50 --no-pager" >&2
    echo "    2. Historical backups in ${TARGET_DIR}:" >&2
    ls -lt "${TARGET_DIR}"/probewatch.backup-* 2>/dev/null >&2 || true
    echo "    3. Manually restore binary:" >&2
    echo "       cp -f <backup-file> ${TARGET_BIN} && chmod 755 ${TARGET_BIN} && systemctl restart ${SERVICE_NAME}" >&2
    echo "==================================================" >&2
  fi

  exit 1
}

echo "=================================================="
echo " Starting ProbeWatch Safe Deployment Sequence"
echo "=================================================="

# ------------------------------------------------------------------------------
# 步骤 0: 磁盘空间预检
# ------------------------------------------------------------------------------
echo "[0/8] Checking available disk space in ${TARGET_DIR}..."
TARGET_DIR_REAL="${TARGET_DIR}"
[ -d "${TARGET_DIR_REAL}" ] || TARGET_DIR_REAL="$(dirname "${TARGET_DIR_REAL}")"
AVAIL_MB=$(df -P -m "${TARGET_DIR_REAL}" | awk 'NR==2 {print $4}')
if [ -n "${AVAIL_MB}" ] && [ "${AVAIL_MB}" -lt "${MIN_FREE_DISK_MB}" ]; then
  echo "[-] ERROR: Insufficient disk space on ${TARGET_DIR_REAL} (${AVAIL_MB}MB available, minimum required is ${MIN_FREE_DISK_MB}MB)" >&2
  exit 1
fi
echo "[+] Step 0 PASSED: Disk space precheck OK (${AVAIL_MB}MB available >= ${MIN_FREE_DISK_MB}MB)"

# ------------------------------------------------------------------------------
# 步骤 1: 检查新二进制存在且满足最小尺寸
# ------------------------------------------------------------------------------
echo "[1/8] Checking candidate binary at: ${NEW_BIN}"
if [ ! -f "${NEW_BIN}" ]; then
  echo "[-] ERROR: Candidate binary does not exist: ${NEW_BIN}" >&2
  exit 1
fi

BIN_SIZE=$(stat -c%s "${NEW_BIN}" 2>/dev/null || stat -f%z "${NEW_BIN}" 2>/dev/null || wc -c < "${NEW_BIN}")
if [ "${BIN_SIZE}" -lt "${MIN_BYTES}" ]; then
  echo "[-] ERROR: Candidate binary size (${BIN_SIZE} bytes) is below minimum required (${MIN_BYTES} bytes)" >&2
  exit 1
fi
echo "[+] Step 1 PASSED: Candidate binary verified (${BIN_SIZE} bytes >= ${MIN_BYTES} bytes)"

# ------------------------------------------------------------------------------
# 步骤 2: 备份旧版本二进制、版本标记与静态资源
# ------------------------------------------------------------------------------
echo "[2/8] Creating pre-deploy backups..."
PREV_COMMIT="unknown"
if [ -f "${TARGET_DIR}/CURRENT_COMMIT" ]; then
  PREV_COMMIT=$(head -n 1 "${TARGET_DIR}/CURRENT_COMMIT" 2>/dev/null | tr -d ' \r\n' | cut -c1-7 || true)
  PREV_COMMIT_BACKUP="${TARGET_DIR}/.prev_commit_backup.${TIMESTAMP}"
  cp -f "${TARGET_DIR}/CURRENT_COMMIT" "${PREV_COMMIT_BACKUP}"
fi

if [ -f "${TARGET_BIN}" ]; then
  BACKUP_FILE="${TARGET_DIR}/probewatch.backup-pre-${PREV_COMMIT}-${TIMESTAMP}"
  cp -p "${TARGET_BIN}" "${BACKUP_FILE}"
  BACKUP_SIZE=$(stat -c%s "${BACKUP_FILE}" 2>/dev/null || wc -c < "${BACKUP_FILE}")
  echo "[+] Existing binary backed up to: ${BACKUP_FILE} (${BACKUP_SIZE} bytes)"
else
  echo "[*] Target binary did not exist prior to this run, skipping binary backup"
fi

# 备份现有 Nginx 静态文件以防回滚版本不一致
if [ -d "${NGINX_STATIC_DIR}/assets" ]; then
  STATIC_BACKUP_DIR="${TARGET_DIR}/.static_backup_${TIMESTAMP}"
  cp -a "${NGINX_STATIC_DIR}/assets" "${STATIC_BACKUP_DIR}"
  echo "[+] Existing Nginx static assets backed up to: ${STATIC_BACKUP_DIR}"
fi

# ------------------------------------------------------------------------------
# 步骤 3 & 4: 原子替换新二进制并设置安全权限
# ------------------------------------------------------------------------------
echo "[3/8] Staging and atomically replacing binary at: ${TARGET_BIN}..."
TMP_BIN="${TARGET_DIR}/.probewatch.tmp.$$"
cp -f "${NEW_BIN}" "${TMP_BIN}"

echo "[4/8] Setting permissions and ownership..."
chmod 755 "${TMP_BIN}"
if id -u probewatch >/dev/null 2>&1; then
  chown probewatch:probewatch "${TMP_BIN}" 2>/dev/null || true
fi

# 同步静态资源到 Nginx（如果存在新产物且目标目录存在）
if [ -d "${STAGE_DIR}/frontend/dist/assets" ] && [ -d "${NGINX_STATIC_DIR}" ]; then
  echo "[*] Synchronizing updated frontend assets to ${NGINX_STATIC_DIR}/assets..."
  mkdir -p "${NGINX_STATIC_DIR}/assets"
  cp -a "${STAGE_DIR}/frontend/dist/assets"/* "${NGINX_STATIC_DIR}/assets/" 2>/dev/null || true
fi

# 原子重命名替换二进制
mv -f "${TMP_BIN}" "${TARGET_BIN}"
echo "[+] Step 3 & 4 PASSED: Binary atomically replaced with 0755 permissions"

# ------------------------------------------------------------------------------
# 步骤 5: 重启服务
# ------------------------------------------------------------------------------
echo "[5/8] Restarting ${SERVICE_NAME}..."
if ! ${SYSTEMCTL_CMD} restart "${SERVICE_NAME}"; then
  rollback "Failed to execute ${SYSTEMCTL_CMD} restart ${SERVICE_NAME}"
fi

# ------------------------------------------------------------------------------
# 步骤 6: 检查服务运行状态
# ------------------------------------------------------------------------------
echo "[6/8] Checking service active state..."
sleep 2
ACTIVE_STATE=$(${SYSTEMCTL_CMD} is-active "${SERVICE_NAME}" 2>/dev/null || true)
if [ "${ACTIVE_STATE}" != "active" ]; then
  ${SYSTEMCTL_CMD} status "${SERVICE_NAME}" --no-pager >&2 || true
  rollback "Service ${SERVICE_NAME} failed to enter active state (current: '${ACTIVE_STATE}')"
fi
echo "[+] Step 6 PASSED: Service is active"

# ------------------------------------------------------------------------------
# 步骤 7: 检查健康接口 (本地与公网)
# ------------------------------------------------------------------------------
echo "[7/8] Checking healthz endpoints..."
echo "      -> Probing local endpoint: ${LOCAL_HEALTHZ}"
LOCAL_RESP=""
for i in {1..5}; do
  LOCAL_RESP=$(${CURL_CMD} -s -S -f --max-time 3 "${LOCAL_HEALTHZ}" 2>/dev/null || true)
  if [ "${LOCAL_RESP}" = "ok" ]; then
    break
  fi
  sleep 1
done

if [ "${LOCAL_RESP}" != "ok" ]; then
  rollback "Local healthz check failed (expected 'ok', got '${LOCAL_RESP}')"
fi
echo "[+] Local healthz OK ('ok')"

if [ "${SKIP_PUBLIC_HEALTHZ}" != "1" ]; then
  echo "      -> Probing public endpoint: ${PUBLIC_HEALTHZ}"
  PUBLIC_RESP=""
  for i in {1..5}; do
    PUBLIC_RESP=$(${CURL_CMD} -s -S -f --max-time 5 "${PUBLIC_HEALTHZ}" 2>/dev/null || true)
    if [ "${PUBLIC_RESP}" = "ok" ]; then
      break
    fi
    sleep 1
  done

  if [ "${PUBLIC_RESP}" != "ok" ]; then
    rollback "Public healthz check failed (expected 'ok', got '${PUBLIC_RESP}')"
  fi
  echo "[+] Public healthz OK ('ok')"
else
  echo "[*] Skipped public healthz check (SKIP_PUBLIC_HEALTHZ=1)"
fi

# ------------------------------------------------------------------------------
# 步骤 8: 成功收尾与历史备份保留管理
# ------------------------------------------------------------------------------
echo "[8/8] Finalizing release metadata and retention cleanup..."

# 更新 CURRENT_COMMIT
NEW_COMMIT="${DEPLOY_COMMIT:-}"
if [ -z "${NEW_COMMIT}" ] && [ -f "${STAGE_DIR}/.commit" ]; then
  NEW_COMMIT=$(head -n 1 "${STAGE_DIR}/.commit" 2>/dev/null | tr -d ' \r\n' || true)
fi
if [ -z "${NEW_COMMIT}" ] && [ -d "${STAGE_DIR}/.git" ]; then
  NEW_COMMIT=$(git -C "${STAGE_DIR}" rev-parse HEAD 2>/dev/null || true)
fi
if [ -n "${NEW_COMMIT}" ]; then
  echo "${NEW_COMMIT}" > "${TARGET_DIR}/CURRENT_COMMIT"
  echo "[+] CURRENT_COMMIT updated to: ${NEW_COMMIT}"
fi

# 清理本次临时备份记录
[ -n "${PREV_COMMIT_BACKUP:-}" ] && rm -f "${PREV_COMMIT_BACKUP}"
[ -n "${STATIC_BACKUP_DIR:-}" ] && rm -rf "${STATIC_BACKUP_DIR}"

# 历史备份保留策略：清理多余的旧二进制备份，防止占满磁盘
if [ -d "${TARGET_DIR}" ] && [ "${MAX_BACKUPS_RETAIN}" -gt 0 ]; then
  BACKUP_COUNT=$(ls -1 "${TARGET_DIR}"/probewatch.backup-* 2>/dev/null | wc -l || true)
  if [ "${BACKUP_COUNT}" -gt "${MAX_BACKUPS_RETAIN}" ]; then
    echo "[*] Pruning older binary backups (retaining newest ${MAX_BACKUPS_RETAIN} of ${BACKUP_COUNT})..."
    # 按时间由新到旧排序，跳过前 MAX_BACKUPS_RETAIN 个，删除剩余的
    ls -1t "${TARGET_DIR}"/probewatch.backup-* 2>/dev/null | tail -n +"$((MAX_BACKUPS_RETAIN + 1))" | while read -r old_bk; do
      if [ -f "${old_bk}" ]; then
        rm -f "${old_bk}"
        echo "    Removed old backup: $(basename "${old_bk}")"
      fi
    done
  fi
fi

# 清理历史可能残留的临时文件
find "${TARGET_DIR}" -maxdepth 1 -name ".probewatch.tmp.*" -mmin +60 -delete 2>/dev/null || true
find "${TARGET_DIR}" -maxdepth 1 -name ".probewatch.rollback.*" -mmin +60 -delete 2>/dev/null || true

echo "=================================================="
echo " Safe Deployment Sequence Completed Successfully!"
echo "=================================================="
