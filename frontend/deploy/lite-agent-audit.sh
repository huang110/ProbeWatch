#!/usr/bin/env bash
set -u

service_name="${LITE_AGENT_SERVICE:-lite-agent.service}"
unit="/etc/systemd/system/${service_name}"

echo '--- identity ---'
echo '--- service ---'
echo '--- effective unit ---'
echo '--- security properties ---'
echo '--- process ---'
echo '--- listeners ---'
echo '--- files ---'
echo '--- remote-control and update flags ---'
echo '--- old service residue ---'
