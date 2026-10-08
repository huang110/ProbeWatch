#!/usr/bin/env bash
# ==============================================================================
# ProbeWatch Read-Only Runtime Resource Profiler
# Inspects memory, CPU, goroutines/threads, file descriptors, WAL and database
# over multiple samples without altering any service configuration.
# ==============================================================================
set -euo pipefail

SERVICE="probewatch.service"
DB_PATH="/opt/probewatch/data/probewatch.db"
INTERVAL=2
SAMPLES=5

while [[ $# -gt 0 ]]; do
  case "$1" in
    --interval|-i)
      INTERVAL="$2"
      shift 2
      ;;
    --samples|-s)
      SAMPLES="$2"
      shift 2
      ;;
    --service)
      SERVICE="$2"
      shift 2
      ;;
    --db-path)
      DB_PATH="$2"
      shift 2
      ;;
    --help|-h)
      echo "Usage: $0 [--interval <seconds>] [--samples <count>] [--service <service_name>] [--db-path <path>]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if ! command -v systemctl >/dev/null 2>&1; then
  echo "[-] ERROR: systemctl not found" >&2
  exit 1
fi

MAIN_PID=$(systemctl show "${SERVICE}" -p MainPID --value 2>/dev/null || echo 0)
if [ -z "${MAIN_PID}" ] || [ "${MAIN_PID}" -le 0 ]; then
  echo "[-] ERROR: Service ${SERVICE} is not running or MainPID not found" >&2
  exit 1
fi

INITIAL_RESTARTS=$(systemctl show "${SERVICE}" -p NRestarts --value 2>/dev/null || echo 0)
MEM_HIGH_RAW=$(systemctl show "${SERVICE}" -p MemoryHigh --value 2>/dev/null || echo "infinity")

echo "================================================================================"
echo " ProbeWatch Runtime Resource Profiler"
echo " Service: ${SERVICE} (PID: ${MAIN_PID})"
echo " Samples: ${SAMPLES} | Interval: ${INTERVAL}s | DB: ${DB_PATH}"
echo "================================================================================"
printf "%-8s %-10s %-10s %-8s %-8s %-12s %-10s %-10s %-8s\n" \
  "SAMPLE" "RSS(MB)" "VSZ(MB)" "CPU%" "THREADS" "MEM_CUR(MB)" "DB(KB)" "WAL(KB)" "FDS"
echo "--------------------------------------------------------------------------------"

declare -a rss_arr=()
declare -a vsz_arr=()
declare -a cpu_arr=()
declare -a fd_arr=()
declare -a db_arr=()
declare -a wal_arr=()

for ((i = 1; i <= SAMPLES; i++)); do
  # Check if PID still exists or service restarted
  CURRENT_PID=$(systemctl show "${SERVICE}" -p MainPID --value 2>/dev/null || echo 0)
  if [ "${CURRENT_PID}" != "${MAIN_PID}" ]; then
    echo "[-] WARNING: Process PID changed from ${MAIN_PID} to ${CURRENT_PID} (service restarted!)" >&2
  fi

  # ps metrics: rss (KB), vsz (KB), %cpu, nlwp (threads)
  read -r ps_rss ps_vsz ps_cpu ps_nlwp < <(ps -p "${CURRENT_PID}" -o rss=,vsz=,%cpu=,nlwp= 2>/dev/null || echo "0 0 0.0 0")
  
  # systemd metrics
  mem_cur_raw=$(systemctl show "${SERVICE}" -p MemoryCurrent --value 2>/dev/null || echo 0)
  mem_cur_mb=$(awk -v b="${mem_cur_raw}" 'BEGIN {if (b ~ /^[0-9]+$/ && b > 0) printf "%.2f", b/1048576; else printf "0.00"}')

  # fd count
  fd_count=0
  if [ -d "/proc/${CURRENT_PID}/fd" ]; then
    fd_count=$(ls -1 "/proc/${CURRENT_PID}/fd" 2>/dev/null | wc -l || echo 0)
  fi

  # db and wal sizes
  db_size_kb=0
  if [ -f "${DB_PATH}" ]; then
    db_size_b=$(stat -c%s "${DB_PATH}" 2>/dev/null || echo 0)
    db_size_kb=$((db_size_b / 1024))
  fi
  wal_size_kb=0
  if [ -f "${DB_PATH}-wal" ]; then
    wal_size_b=$(stat -c%s "${DB_PATH}-wal" 2>/dev/null || echo 0)
    wal_size_kb=$((wal_size_b / 1024))
  fi

  rss_mb=$(awk -v k="${ps_rss}" 'BEGIN {printf "%.2f", k/1024}')
  vsz_mb=$(awk -v k="${ps_vsz}" 'BEGIN {printf "%.2f", k/1024}')

  rss_arr+=("${rss_mb}")
  vsz_arr+=("${vsz_mb}")
  cpu_arr+=("${ps_cpu}")
  fd_arr+=("${fd_count}")
  db_arr+=("${db_size_kb}")
  wal_arr+=("${wal_size_kb}")

  printf "%-8s %-10s %-10s %-8s %-8s %-12s %-10s %-10s %-8s\n" \
    "#${i}" "${rss_mb}" "${vsz_mb}" "${ps_cpu}" "${ps_nlwp}" "${mem_cur_mb}" "${db_size_kb}" "${wal_size_kb}" "${fd_count}"

  if [ "${i}" -lt "${SAMPLES}" ]; then
    sleep "${INTERVAL}"
  fi
done

echo "================================================================================"
echo " Runtime Resource Profiling Summary"
echo "================================================================================"

# Compute min, max, avg via awk
stats() {
  local name="$1"
  shift
  local vals=("$@")
  printf '%s\n' "${vals[@]}" | awk -v label="${name}" '
    BEGIN { min=99999999; max=-1; sum=0; count=0 }
    {
      val = $1 + 0
      if (val < min) min = val
      if (val > max) max = val
      sum += val
      count++
      arr[count] = val
    }
    END {
      if (count > 0) {
        avg = sum / count
        delta = arr[count] - arr[1]
        printf "  %-12s: Min=%.2f | Max=%.2f | Avg=%.2f | Delta=%.2f\n", label, min, max, avg, delta
      }
    }
  '
}

stats "RSS (MB)" "${rss_arr[@]}"
stats "VSZ (MB)" "${vsz_arr[@]}"
stats "CPU (%)" "${cpu_arr[@]}"
stats "Open FDs" "${fd_arr[@]}"
stats "DB (KB)" "${db_arr[@]}"
stats "WAL (KB)" "${wal_arr[@]}"

FINAL_RESTARTS=$(systemctl show "${SERVICE}" -p NRestarts --value 2>/dev/null || echo 0)
MEM_PEAK_RAW=$(systemctl show "${SERVICE}" -p MemoryPeak --value 2>/dev/null || echo 0)
MEM_PEAK_MB=$(awk -v b="${MEM_PEAK_RAW}" 'BEGIN {if (b ~ /^[0-9]+$/ && b > 0) printf "%.2f", b/1048576; else printf "N/A"}')

echo "--------------------------------------------------------------------------------"
echo " Health & Stability Assertions:"
if [ "${FINAL_RESTARTS}" -eq "${INITIAL_RESTARTS}" ]; then
  echo "  [+] Process Stability: NO restarts occurred (NRestarts=${FINAL_RESTARTS})"
else
  echo "  [-] Process Stability: Service restarted during profiling! (Before=${INITIAL_RESTARTS}, After=${FINAL_RESTARTS})"
fi

echo "  [+] systemd MemoryPeak: ${MEM_PEAK_MB} MB (MemoryHigh=${MEM_HIGH_RAW})"

# Check continuous growth between first and last sample
FIRST_RSS="${rss_arr[0]}"
LAST_RSS="${rss_arr[-1]}"
RSS_DIFF=$(awk -v f="${FIRST_RSS}" -v l="${LAST_RSS}" 'BEGIN {printf "%.2f", l - f}')
if (( $(awk -v d="${RSS_DIFF}" 'BEGIN {print (d > 10.0) ? 1 : 0}') )); then
  echo "  [!] Memory Trend: Noticeable upward growth detected (+${RSS_DIFF} MB)"
else
  echo "  [+] Memory Trend: Stable within baseline envelope (Delta: ${RSS_DIFF} MB)"
fi

FIRST_WAL="${wal_arr[0]}"
LAST_WAL="${wal_arr[-1]}"
if [ "${LAST_WAL}" -gt 51200 ]; then
  echo "  [!] WAL Trend: WAL size is elevated (${LAST_WAL} KB > 50 MB)"
else
  echo "  [+] WAL Trend: WAL file healthy (${LAST_WAL} KB <= 50 MB)"
fi
echo "================================================================================"
