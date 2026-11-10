#!/usr/bin/env bash
# Antigravity 24/7 Keepalive & Watchdog
# Ensures Antigravity IDE server never terminates on client disconnect
# and restarts automatically if the VPS is rebooted.

TOKEN_FILE="/root/.antigravity-ide-server/.9e3a5da5d6731effd57d47d88bbe5df14ac6ccdd.token"
BIN="/root/.antigravity-ide-server/bin/2.6.0-9e3a5da5d6731effd57d47d88bbe5df14ac6ccdd/bin/antigravity-ide-server"

echo "[keeper] Started Antigravity 24/7 Watchdog (30-day session preservation)"

while true; do
    SERVER_PID=$(pgrep -f "server-main.js" | head -n 1)
    if [ -n "$SERVER_PID" ]; then
        PORT=$(ss -tlpn | grep "pid=${SERVER_PID}," | awk '{print $4}' | awk -F':' '{print $NF}' | head -n 1)
        if [ -n "$PORT" ]; then
            curl -s "http://127.0.0.1:${PORT}/delay-shutdown" > /dev/null 2>&1
        fi
    else
        if [ -x "$BIN" ] && [ -f "$TOKEN_FILE" ]; then
            echo "[keeper] antigravity-ide-server not running, launching..."
            "$BIN" --start-server --host=127.0.0.1 --port=0 \
                --connection-token-file "$TOKEN_FILE" \
                --reconnection-grace-time 2592000 \
                --telemetry-level off --accept-server-license-terms > /root/.antigravity-ide-server/keeper.log 2>&1 &
        fi
    fi
    sleep 30
done
