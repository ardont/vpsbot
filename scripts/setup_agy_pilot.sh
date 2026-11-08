#!/usr/bin/env bash
# ==============================================================================
# setup_agy_pilot.sh — Установка моста agy-pilot (Telegram <-> Antigravity)
# ==============================================================================

set -euo pipefail

echo "=================================================="
echo "          Установка моста agy-pilot (Go)          "
echo "=================================================="

INSTALL_DIR="/opt/superbot/agy-pilot"
REPO_URL="https://github.com/donileong/agy-pilot.git"

if ! command -v git >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -y -qq git
fi

if ! command -v go >/dev/null 2>&1; then
    echo "⚠️ Go не установлен на сервере. Рекомендуется установить Go или скомпилировать agy-pilot локально."
fi

echo "==> [1/3] Клонирование репозитория agy-pilot..."
if [[ -d "$INSTALL_DIR" ]]; then
    echo "Директория $INSTALL_DIR уже существует. Обновление..."
    git -C "$INSTALL_DIR" pull || true
else
    git clone "$REPO_URL" "$INSTALL_DIR"
fi

echo "==> [2/3] Сборка бинарника agy-pilot..."
if command -v go >/dev/null 2>&1; then
    (cd "$INSTALL_DIR" && go build -ldflags="-s -w" -o agy-pilot .)
    echo "✅ Бинарник успешно собран в $INSTALL_DIR/agy-pilot"
fi

echo "==> [3/3] Настройка systemd-сервиса (ai.slice)..."
cat << 'EOF' > /etc/systemd/system/agy-pilot.service
[Unit]
Description=Antigravity Pilot Telegram Bridge Daemon
After=network.target superbot.service

[Service]
Type=simple
User=root
Slice=ai.slice
WorkingDirectory=/opt/superbot/agy-pilot
ExecStart=/opt/superbot/agy-pilot/agy-pilot
Restart=always
RestartSec=5
MemoryHigh=120M
MemoryMax=150M
EnvironmentFile=-/opt/superbot/.env

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable agy-pilot.service

echo "✅ Мост agy-pilot настроен и добавлен в автозагрузку!"
echo "Запуск: systemctl start agy-pilot"

