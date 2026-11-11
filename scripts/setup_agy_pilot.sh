#!/usr/bin/env bash
# ==============================================================================
# Настройка и запуск демона agy-pilot (Antigravity Pilot Bridge)
# ==============================================================================

set -euo pipefail

echo "=================================================="
echo "    Настройка Antigravity Pilot Bridge (Go)       "
echo "=================================================="

INSTALL_DIR="/opt/superbot/agy-pilot"
mkdir -p "$INSTALL_DIR"

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "==> [1/3] Компиляция бинарника agy-pilot..."
(cd "$PROJECT_ROOT" && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$INSTALL_DIR/agy-pilot" ./cmd/agy-pilot)
chmod +x "$INSTALL_DIR/agy-pilot"
echo "✅ Бинарник успешно собран в $INSTALL_DIR/agy-pilot"

echo "==> [2/3] Настройка systemd-сервиса agy-pilot..."
SERVICE_FILE="/etc/systemd/system/agy-pilot.service"

cat << 'EOF' > "$SERVICE_FILE"
[Unit]
Description=Antigravity Pilot Bridge Daemon
After=network.target superbot.service

[Service]
Type=simple
User=root
Slice=ai.slice
WorkingDirectory=/opt/superbot/agy-pilot
ExecStart=/opt/superbot/agy-pilot/agy-pilot
Restart=always
RestartSec=5
MemoryHigh=60M
MemoryMax=80M
EnvironmentFile=-/opt/superbot/.env

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now agy-pilot.service

echo "✅ Мост agy-pilot настроен и запущен!"
systemctl status agy-pilot --no-pager
