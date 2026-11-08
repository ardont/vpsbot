# 🤖 SuperBot (VPS Hybrid Daemon)

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20AMD64-FCC624?style=flat&logo=linux)](https://ubuntu.com)
[![Architecture](https://img.shields.io/badge/Architecture-Hybrid%20AI%20%2B%20Sysadmin-6f42c1?style=flat)]()
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Высокопроизводительный гибридный демон на Go для удаленного администрирования VPS-сервера (2 ГБ ОЗУ) через Telegram с динамическим управлением ресурсами (cgroups v2) и интеграцией с ИИ-агентом Antigravity через Model Context Protocol (MCP).

---

## 🏗️ Архитектура системы

Система разделена на три изолированных контура:

```
[Пользователь] 
       │ (Telegram)
       ▼
 ┌─────────────┐   Системные команды (/stats, /top, /net, /ls, /find, /send, /mode)
 │  SuperBot   │ ─────────────────────────────────────────────────────────────► [Linux Kernel / cgroups v2]
 │    (Go)     │                                                                 ├─ vpn.slice (x-ui)
 └─────────────┘                                                                 └─ ai.slice (omniroute, bot)
       │ (Обычный текст без слэша)
       ▼
 ┌─────────────┐
 │  agy-pilot  │ (Локальный мост :43427)
 └─────────────┘
       │
       ▼
 ┌─────────────┐     MCP Stdio      ┌──────────────────────────────────────────────┐
 │ Antigravity │ ─────────────────► │ 1. google-mcp (Gmail & Google Calendar)      │
 │ (AI Engine) │ ◄───────────────── │ 2. mcp-telegram (Отправка файлов и отчетов)  │
 └─────────────┘                    └──────────────────────────────────────────────┘
```

---

## ⚡ Функциональные возможности

### 1. Системный мониторинг (0 токенов, мгновенный отклик, ~15 МБ RAM)
* `/stats` — метрики CPU, LoadAvg, RAM (всего / свободно / %), ZRAM / Swap, Диск `/` и Uptime.
* `/top` — топ-5 процессов сервера по потреблению физической памяти (RSS).
* `/net` — сетевой трафик (TX/RX в МБ, число пакетов) и активные сетевые интерфейсы.
* `/ls <путь>` — интерактивный просмотр содержимого директорий с размерами файлов и бейджами типов.
* `/find <маска>` — безопасный поиск файлов с защитой от зависания (таймаут 4 сек, пропуск `/proc`, `/sys`).
* `/send <путь>` — скачивание любого файла сервера прямо в Telegram (до 50 МБ).

### 2. Динамическое управление приоритетами (cgroups v2)
Мгновенное перераспределение процессорного времени (`CPUWeight`) и мягких лимитов памяти (`MemoryHigh`):
* `/mode vpn` — **Режим «Турбо VPN»**: `x-ui.service` получает 90% CPU и 1.2 ГБ памяти. Агент урезан.
* `/mode ai` — **Режим «Супер-Агент»**: Antigravity/OmniRoute получают 80% CPU и 1.3 ГБ RAM для сложных вычислений.
* `/mode auto` — **Сбалансированный режим** (50/50) для повседневной работы.
* `/vpn_restart` — быстрый рестарт VPN-сервиса `x-ui.service`.
* `/ai_restart` — быстрый рестарт AI/Gateway сервиса `omniroute.service`.

### 3. Интеллектуальный агент Antigravity & MCP
* Обычные текстовые запросы (например: *"Проверь почту за сегодня и пришли мне сводку"*) автоматически уходят в Antigravity.
* **Google Workspace MCP:** агент умеет читать Gmail, отвечать на письма и работать с Календарем.
* **Telegram MCP:** агент может отправить файл или отчет в Telegram:
  * Через MTProto User API ([Prgebish/mcp-telegram](https://github.com/Prgebish/mcp-telegram)) — от имени вашего аккаунта.
  * Через локальный HTTP API SuperBot (`POST http://127.0.0.1:43428/api/send`) — быстро и без риска блокировки.

---

## 🚀 Быстрый старт на VPS

### Шаг 1. Клонирование репозитория
```bash
git clone https://github.com/ardont/vpsbot.git /opt/superbot-src
cd /opt/superbot-src
```

### Шаг 2. Автоматическая установка окружения
Запустите единый установочный скрипт:
```bash
sudo bash scripts/install.sh
```
Скрипт автоматически:
1. Настроит сжатый пул **ZRAM 1 ГБ (zstd)** с высоким приоритетом 100 и `vm.swappiness=150`.
2. Создаст слайсы **cgroups v2** (`vpn.slice` и `ai.slice`) и привяжет к ним `x-ui` и `omniroute`.
3. Установит systemd-сервис `superbot.service` с лимитом `MemoryMax=100M`.

### Шаг 3. Заполнение секретов (`.env`)
Отредактируйте созданный файл:
```bash
sudo nano /opt/superbot/.env
```
Укажите ваши значения:
```ini
TG_BOT_TOKEN=1234567890:ABCdefGHIjklMNOpqrsTUVwxyz
TG_ADMIN_ID=5342950903
```
Убедитесь, что права на файл защищены:
```bash
sudo chmod 600 /opt/superbot/.env
```

### Шаг 4. Запуск и проверка
```bash
sudo systemctl start superbot
sudo systemctl status superbot
```
Просмотр логов в реальном времени:
```bash
journalctl -u superbot -f
```

---

## 🛠️ Сборка из исходников (Make)

```bash
# Локальная сборка
make build

# Кросс-компиляция статического бинарника для Linux AMD64
make build-linux

# Запуск тестов
make test

# Деплой на боевой VPS одной командой
make deploy SSH_HOST=root@YOUR_VPS_IP
```

---

## 🔌 Интеграция с MCP (Model Context Protocol)

Подробное руководство по подключению `google-mcp` и `mcp-telegram` доступно в [mcp/README.md](mcp/README.md).

### Конфигурация для Antigravity (`mcp_config.json`):
```json
{
  "mcpServers": {
    "google": {
      "command": "/opt/superbot/google-mcp-linux-amd64",
      "args": ["--preset", "everyday"],
      "env": {
        "GOOGLE_CLIENT_ID": "${GOOGLE_CLIENT_ID}",
        "GOOGLE_CLIENT_SECRET": "${GOOGLE_CLIENT_SECRET}",
        "GOOGLE_REDIRECT_URI": "http://localhost:4100/oauth2callback"
      }
    },
    "telegram": {
      "command": "mcp-telegram",
      "env": {
        "TG_APP_ID": "${TG_APP_ID}",
        "TG_APP_HASH": "${TG_APP_HASH}"
      }
    }
  }
}
```

---

## 🛡️ Безопасность

* **Strict Whitelist:** Бот реагирует **исключительно** на запросы от указанного `TG_ADMIN_ID`. Запросы от всех остальных пользователей молча отбрасываются (Stealth Mode).
* **Безопасность файловой системы:** Системные каталоги `/proc`, `/sys`, `/dev` исключены из поискового индекса; файлы для отправки ограничены лимитом 50 МБ.
* **Изоляция systemd:** `superbot.service` работает с `NoNewPrivileges=true` и жестким лимитом памяти `MemoryMax=100M`.
