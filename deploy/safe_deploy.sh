#!/usr/bin/env bash
# ==============================================================================
# ProbeWatch Safe Deployment Script with Automatic Atomic Rollback
# 明确区分四个发布阶段：
# Phase 1: 预检 (Pre-checks): 空间、二进制与工具检查、并发部署锁
# Phase 2: 准备 (Preparation): 备份旧版本二进制、版本标记、完整前端静态资源
# Phase 3: 开始修改线上文件 (Live Modification): 事务保护，任一失败立即回滚
# Phase 4: 提交成功 (Commit & Finalize): 确认新版本健康、写入版本标记、清理与保留
# ==============================================================================
set -euo pipefail

# 可配置参数与合理默认值
STAGE_DIR="${STAGE_DIR:-.}"
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
CP_CMD="${CP_CMD:-cp}"
MV_CMD="${MV_CMD:-mv}"
CHMOD_CMD="${CHMOD_CMD:-chmod}"

TIMESTAMP=$(date +%Y%m%d%H%M%S)
LOCK_DIR="${TARGET_DIR}/.deploy.lock.d"
BACKUP_FILE=""
PREV_COMMIT_BACKUP=""
STATIC_BACKUP_DIR=""
TMP_BIN=""
IN_LIVE_TRANSACTION=0
LOCK_HELD=0

# ------------------------------------------------------------------------------
# 锁管理与清理
# ------------------------------------------------------------------------------
acquire_lock() {
  if ! mkdir "${LOCK_DIR}" 2>/dev/null; then
    echo "[-] ERROR: Another deployment is currently in progress (${LOCK_DIR} exists)" >&2
    exit 1
  fi
  LOCK_HELD=1
}

release_lock() {
  if [ "${LOCK_HELD}" -eq 1 ] && [ -d "${LOCK_DIR}" ]; then
    rmdir "${LOCK_DIR}" 2>/dev/null || rm -rf "${LOCK_DIR}" 2>/dev/null || true
    LOCK_HELD=0
  fi
}

cleanup_exit() {
  local exit_code=$?
  if [ "${exit_code}" -ne 0 ] && [ "${IN_LIVE_TRANSACTION}" -eq 1 ]; then
    rollback "Script terminated unexpectedly with exit code ${exit_code}"
  fi
  release_lock
}
trap cleanup_exit EXIT

# ------------------------------------------------------------------------------
# 统一自动回滚函数
# ------------------------------------------------------------------------------
rollback() {
  # 禁用后续陷阱，防止回滚内部再次循环触发
  IN_LIVE_TRANSACTION=0
  local reason="${1:-Unknown error}"
  echo "==================================================" >&2
  echo "[!] DEPLOYMENT FAILED: ${reason}" >&2
  echo "[!] Initiating automatic atomic rollback sequence..." >&2
  echo "==================================================" >&2

  # 清理未完成的临时暂存文件
  if [ -n "${TMP_BIN:-}" ] && [ -f "${TMP_BIN}" ]; then
    rm -f "${TMP_BIN}"
  fi

  local rollback_ok=1

  # 1. 恢复二进制文件
  if [ -n "${BACKUP_FILE:-}" ] && [ -f "${BACKUP_FILE}" ]; then
    echo "[*] Restoring previous binary from: ${BACKUP_FILE}" >&2
    local restore_tmp="${TARGET_DIR}/.probewatch.rollback.$$"
    if ${CP_CMD} -f "${BACKUP_FILE}" "${restore_tmp}" && ${CHMOD_CMD} 755 "${restore_tmp}"; then
      if id -u probewatch >/dev/null 2>&1; then
        chown probewatch:probewatch "${restore_tmp}" 2>/dev/null || true
      fi
      if ${MV_CMD} -f "${restore_tmp}" "${TARGET_BIN}"; then
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
    echo "[*] Notice: No previous binary backup existed prior to deployment; removed candidate binary." >&2
    rm -f "${TARGET_BIN}"
  fi

  # 2. 恢复版本标记
  if [ -n "${PREV_COMMIT_BACKUP:-}" ] && [ -f "${PREV_COMMIT_BACKUP}" ]; then
    echo "[*] Restoring CURRENT_COMMIT to previous version..." >&2
    if ${CP_CMD} -f "${PREV_COMMIT_BACKUP}" "${TARGET_DIR}/CURRENT_COMMIT"; then
      rm -f "${PREV_COMMIT_BACKUP}"
      echo "[+] CURRENT_COMMIT restored" >&2
    else
      echo "[-] CRITICAL: Failed to restore CURRENT_COMMIT" >&2
      rollback_ok=0
    fi
  else
    rm -f "${TARGET_DIR}/CURRENT_COMMIT" 2>/dev/null || true
  fi

  # 3. 恢复完整前端静态资源
  if [ -n "${STATIC_BACKUP_DIR:-}" ] && [ -d "${STATIC_BACKUP_DIR}" ] && [ -d "${NGINX_STATIC_DIR}" ]; then
    echo "[*] Restoring Nginx static assets from: ${STATIC_BACKUP_DIR}..." >&2
    if rm -rf "${NGINX_STATIC_DIR:?}"/* && ${CP_CMD} -a "${STATIC_BACKUP_DIR}/." "${NGINX_STATIC_DIR}/"; then
      echo "[+] Nginx static assets restored" >&2
      rm -rf "${STATIC_BACKUP_DIR}"
    else
      echo "[-] CRITICAL: Failed to restore static assets to ${NGINX_STATIC_DIR}" >&2
      rollback_ok=0
    fi
  fi

  # 4. 重启旧版本服务并探活
  if [ -n "${BACKUP_FILE:-}" ] && [ -f "${TARGET_BIN}" ]; then
    echo "[*] Restarting ${SERVICE_NAME} to apply restored binary..." >&2
    if ${SYSTEMCTL_CMD} restart "${SERVICE_NAME}" 2>/dev/null; then
      sleep 2
      local restored_state
      restored_state=$(${SYSTEMCTL_CMD} is-active "${SERVICE_NAME}" 2>/dev/null || echo "inactive")
      if [ "${restored_state}" = "active" ]; then
        echo "[+] Restored service is active" >&2
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
          echo "[-] CRITICAL: Rolled-back service is active but healthz probe failed (got: '${restored_health}')" >&2
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
  fi

  release_lock

  if [ "${rollback_ok}" -eq 1 ]; then
    echo "==================================================" >&2
    echo "[+] Rollback completed successfully. Service remains operational on previous version." >&2
    echo "[-] Deployment terminated with non-zero exit code (1)." >&2
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

# ------------------------------------------------------------------------------
# 阶段 1: 预检 (Pre-checks)
# ------------------------------------------------------------------------------
echo "=================================================="
echo " Starting ProbeWatch Safe Deployment Sequence"
echo "=================================================="
echo "[Phase 1/4] Pre-flight checks..."

acquire_lock

TARGET_DIR_REAL="${TARGET_DIR}"
[ -d "${TARGET_DIR_REAL}" ] || TARGET_DIR_REAL="$(dirname "${TARGET_DIR_REAL}")"
AVAIL_MB=$(df -P -m "${TARGET_DIR_REAL}" | awk 'NR==2 {print $4}')
if [ -n "${AVAIL_MB}" ] && [ "${AVAIL_MB}" -lt "${MIN_FREE_DISK_MB}" ]; then
  echo "[-] ERROR: Insufficient disk space on ${TARGET_DIR_REAL} (${AVAIL_MB}MB available, minimum required is ${MIN_FREE_DISK_MB}MB)" >&2
  exit 1
fi
echo "[+] Disk space precheck OK (${AVAIL_MB}MB available >= ${MIN_FREE_DISK_MB}MB)"

if [ ! -f "${NEW_BIN}" ]; then
  echo "[-] ERROR: Candidate binary does not exist: ${NEW_BIN}" >&2
  exit 1
fi

BIN_SIZE=$(stat -c%s "${NEW_BIN}" 2>/dev/null || stat -f%z "${NEW_BIN}" 2>/dev/null || wc -c < "${NEW_BIN}")
if [ "${BIN_SIZE}" -lt "${MIN_BYTES}" ]; then
  echo "[-] ERROR: Candidate binary size (${BIN_SIZE} bytes) is below minimum required (${MIN_BYTES} bytes)" >&2
  exit 1
fi
echo "[+] Candidate binary verified (${BIN_SIZE} bytes >= ${MIN_BYTES} bytes)"
echo "[+] Phase 1 PASSED: All pre-flight checks satisfied"

# ------------------------------------------------------------------------------
# 阶段 2: 准备 (Preparation - Backups & Staging)
# ------------------------------------------------------------------------------
echo "[Phase 2/4] Preparation & pre-deploy backups..."

PREV_COMMIT="unknown"
if [ -f "${TARGET_DIR}/CURRENT_COMMIT" ]; then
  PREV_COMMIT=$(head -n 1 "${TARGET_DIR}/CURRENT_COMMIT" 2>/dev/null | tr -d ' \r\n' | cut -c1-7 || true)
  PREV_COMMIT_BACKUP="${TARGET_DIR}/.prev_commit_backup.${TIMESTAMP}"
  ${CP_CMD} -f "${TARGET_DIR}/CURRENT_COMMIT" "${PREV_COMMIT_BACKUP}"
  echo "[+] CURRENT_COMMIT backed up to: ${PREV_COMMIT_BACKUP}"
fi

if [ -f "${TARGET_BIN}" ]; then
  BACKUP_FILE="${TARGET_DIR}/probewatch.backup-pre-${PREV_COMMIT}-${TIMESTAMP}"
  ${CP_CMD} -p "${TARGET_BIN}" "${BACKUP_FILE}"
  BACKUP_SIZE=$(stat -c%s "${BACKUP_FILE}" 2>/dev/null || wc -c < "${BACKUP_FILE}")
  echo "[+] Existing binary backed up to: ${BACKUP_FILE} (${BACKUP_SIZE} bytes)"
else
  echo "[*] Target binary did not exist prior to this run; no previous binary to backup"
fi

# 备份现有 Nginx 静态文件（包含完整 index.html 与 assets）
if [ -d "${NGINX_STATIC_DIR}" ] && [ "$(ls -A "${NGINX_STATIC_DIR}" 2>/dev/null)" ]; then
  STATIC_BACKUP_DIR="${TARGET_DIR}/.static_backup_${TIMESTAMP}"
  mkdir -p "${STATIC_BACKUP_DIR}"
  ${CP_CMD} -a "${NGINX_STATIC_DIR}/." "${STATIC_BACKUP_DIR}/"
  echo "[+] Existing Nginx static assets fully backed up to: ${STATIC_BACKUP_DIR}"
fi

# 在临时文件准备好新二进制并赋予权限
TMP_BIN="${TARGET_DIR}/.probewatch.tmp.$$"
${CP_CMD} -f "${NEW_BIN}" "${TMP_BIN}"
${CHMOD_CMD} 755 "${TMP_BIN}"
if id -u probewatch >/dev/null 2>&1; then
  chown probewatch:probewatch "${TMP_BIN}" 2>/dev/null || true
fi
echo "[+] Phase 2 PASSED: Pre-deploy backups and staging completed"

# ------------------------------------------------------------------------------
# 阶段 3: 开始修改线上文件 (Live Modification - Transactional Protected)
# ------------------------------------------------------------------------------
echo "[Phase 3/4] Live Modification (Transactional section)..."
IN_LIVE_TRANSACTION=1

# 3.1 同步完整前端静态产物（包含 index.html 及 assets/，严禁忽略错误）
if [ -d "${STAGE_DIR}/frontend/dist" ] && [ -d "${NGINX_STATIC_DIR}" ]; then
  echo "[*] Synchronizing complete frontend dist to ${NGINX_STATIC_DIR}..."
  if ! ${CP_CMD} -a "${STAGE_DIR}/frontend/dist/." "${NGINX_STATIC_DIR}/"; then
    rollback "Failed to copy frontend dist to ${NGINX_STATIC_DIR}"
  fi
  if id -u www-data >/dev/null 2>&1; then
    chown -R www-data:www-data "${NGINX_STATIC_DIR}" 2>/dev/null || true
  fi
  echo "[+] Frontend static dist synchronized successfully"
fi

# 3.2 原子替换二进制文件
echo "[*] Atomically replacing binary at ${TARGET_BIN}..."
if ! ${MV_CMD} -f "${TMP_BIN}" "${TARGET_BIN}"; then
  rollback "Failed to atomically rename ${TMP_BIN} to ${TARGET_BIN}"
fi
echo "[+] Binary atomically replaced"

# 3.3 重启服务
echo "[*] Restarting ${SERVICE_NAME}..."
if ! ${SYSTEMCTL_CMD} restart "${SERVICE_NAME}"; then
  rollback "Failed to execute ${SYSTEMCTL_CMD} restart ${SERVICE_NAME}"
fi

# 3.4 检查服务 active 状态
echo "[*] Verifying service active state..."
sleep 2
ACTIVE_STATE=$(${SYSTEMCTL_CMD} is-active "${SERVICE_NAME}" 2>/dev/null || echo "inactive")
if [ "${ACTIVE_STATE}" != "active" ]; then
  ${SYSTEMCTL_CMD} status "${SERVICE_NAME}" --no-pager >&2 || true
  rollback "Service ${SERVICE_NAME} failed to enter active state (current: '${ACTIVE_STATE}')"
fi
echo "[+] Service is active"

# 3.5 探活接口校验 (本地与公网)
echo "[*] Checking local healthz endpoint: ${LOCAL_HEALTHZ}..."
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
  echo "[*] Checking public healthz endpoint: ${PUBLIC_HEALTHZ}..."
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

# 3.6 写入新的版本标记
NEW_COMMIT="${DEPLOY_COMMIT:-}"
if [ -z "${NEW_COMMIT}" ] && [ -f "${STAGE_DIR}/.commit" ]; then
  NEW_COMMIT=$(head -n 1 "${STAGE_DIR}/.commit" 2>/dev/null | tr -d ' \r\n' || true)
fi
if [ -z "${NEW_COMMIT}" ] && [ -d "${STAGE_DIR}/.git" ]; then
  NEW_COMMIT=$(git -C "${STAGE_DIR}" rev-parse HEAD 2>/dev/null || true)
fi
if [ -z "${NEW_COMMIT}" ] && git rev-parse HEAD >/dev/null 2>&1; then
  NEW_COMMIT=$(git rev-parse HEAD 2>/dev/null || true)
fi
if [ -n "${NEW_COMMIT}" ]; then
  if ! echo "${NEW_COMMIT}" > "${TARGET_DIR}/CURRENT_COMMIT"; then
    rollback "Failed to write CURRENT_COMMIT"
  fi
  echo "[+] CURRENT_COMMIT updated to: ${NEW_COMMIT}"
fi

# 线上修改已全部成功完成，离开事务保护
IN_LIVE_TRANSACTION=0
echo "[+] Phase 3 PASSED: All live modifications committed and health checks passed"

# ------------------------------------------------------------------------------
# 阶段 4: 提交成功与清理保留 (Commit & Finalize)
# ------------------------------------------------------------------------------
echo "[Phase 4/4] Commit success & cleanup..."

# 清理本次暂存备份
[ -n "${PREV_COMMIT_BACKUP:-}" ] && rm -f "${PREV_COMMIT_BACKUP}" 2>/dev/null || true
[ -n "${STATIC_BACKUP_DIR:-}" ] && rm -rf "${STATIC_BACKUP_DIR}" 2>/dev/null || true

# 历史备份保留策略：清理多余的旧二进制备份，防止占满磁盘
if [ -d "${TARGET_DIR}" ] && [ "${MAX_BACKUPS_RETAIN}" -gt 0 ]; then
  BACKUP_COUNT=$(ls -1 "${TARGET_DIR}"/probewatch.backup-* 2>/dev/null | wc -l || true)
  if [ "${BACKUP_COUNT}" -gt "${MAX_BACKUPS_RETAIN}" ]; then
    echo "[*] Pruning older binary backups (retaining newest ${MAX_BACKUPS_RETAIN} of ${BACKUP_COUNT})..."
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

release_lock

echo "=================================================="
echo " Safe Deployment Sequence Completed Successfully!"
echo "=================================================="
exit 0
