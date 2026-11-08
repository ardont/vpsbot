#!/usr/bin/env bash
# ==============================================================================
# setup_cgroups.sh — Настройка cgroups v2 slices для x-ui и omniroute
# ==============================================================================

set -euo pipefail

echo "==> [1/4] Проверка прав root..."
if [[ $EUID -ne 0 ]]; then
   echo "❌ Этот скрипт должен быть запущен с правами root (sudo)."
   exit 1
fi

echo "==> [2/4] Создание vpn.slice и ai.slice..."
cat << 'EOF' > /etc/systemd/system/vpn.slice
[Unit]
Description=VPN Services Slice (x-ui / 3x-ui)
Before=slices.target

[Slice]
CPUWeight=500
MemoryHigh=600M
EOF

cat << 'EOF' > /etc/systemd/system/ai.slice
[Unit]
Description=AI and Automation Slice (omniroute / antigravity / superbot)
Before=slices.target

[Slice]
CPUWeight=500
MemoryHigh=800M
EOF

echo "==> [3/4] Привязка x-ui.service к vpn.slice..."
mkdir -p /etc/systemd/system/x-ui.service.d
cat << 'EOF' > /etc/systemd/system/x-ui.service.d/override.conf
[Service]
Slice=vpn.slice
EOF

echo "==> [4/4] Привязка omniroute.service к ai.slice..."
mkdir -p /etc/systemd/system/omniroute.service.d
cat << 'EOF' > /etc/systemd/system/omniroute.service.d/override.conf
[Service]
Slice=ai.slice
EOF

echo "==> Перезагрузка конфигурации systemd..."
systemctl daemon-reload

# Мягкий перезапуск сервисов (если они активны)
if systemctl is-active --quiet x-ui; then
    echo "Перезапуск x-ui в новом vpn.slice..."
    systemctl restart x-ui
fi

if systemctl is-active --quiet omniroute; then
    echo "Перезапуск omniroute в новом ai.slice..."
    systemctl restart omniroute
fi

echo "✅ cgroups v2 slices успешно настроены и активированы!"

