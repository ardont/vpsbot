#!/usr/bin/env bash
# ==============================================================================
# setup_agy_pilot.sh — Сборка и настройка моста agy-pilot (Go)
# Antigravity <-> SuperBot Bridge
# ==============================================================================

set -euo pipefail

echo "=================================================="
echo "          Установка моста agy-pilot (Go)          "
echo "=================================================="

INSTALL_DIR="/opt/superbot/agy-pilot"
mkdir -p "$INSTALL_DIR"

echo "==> [1/2] Сборка бинарника agy-pilot..."
(cd /root/vpsbot && go build -ldflags="-s -w" -o "$INSTALL_DIR/agy-pilot" ./cmd/agy-pilot)
chmod +x "$INSTALL_DIR/agy-pilot"
echo "✅ Бинарник успешно собран в $INSTALL_DIR/agy-pilot"

echo "==> [2/2] Настройка systemd-сервиса (ai.slice)..."
cat << 'EOF' > /etc/systemd/system/agy-pilot.service
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
