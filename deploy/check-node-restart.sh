#!/usr/bin/env bash
set -euo pipefail

service=sing-box-panel
uid=$(id -u "$service")
systemctl start polkit.service

# Polkit accepts action details only from a trusted caller. Check as root
# while binding the subject to a process that belongs to the panel account.
setpriv --reuid="$service" --regid="$service" --init-groups -- sleep 30 &
subject_pid=$!
trap 'kill "$subject_pid" 2>/dev/null || :; wait "$subject_pid" 2>/dev/null || :' EXIT

for ((attempt = 0; attempt < 5; attempt++)); do
    if subject_uid=$(awk '/^Uid:/ {print $2}' "/proc/$subject_pid/status" 2>/dev/null) &&
        [[ $subject_uid == "$uid" ]] &&
        start_time=$(awk '{print $22}' "/proc/$subject_pid/stat" 2>/dev/null) &&
        [[ $start_time =~ ^[0-9]+$ ]] &&
        timeout 3 /usr/bin/pkcheck --action-id org.freedesktop.systemd1.manage-units \
            --process "$subject_pid,$start_time,$uid" \
            --detail unit sing-box-panel-node.service --detail verb restart >/dev/null; then
        exit 0
    fi
    sleep 1
done
exit 1
