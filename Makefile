.PHONY: all build build-linux test clean deploy

APP_NAME := superbot
BUILD_DIR := bin
CMD_DIR := ./cmd/bot

all: build

build:
	go build -ldflags="-s -w" -o $(BUILD_DIR)/$(APP_NAME) $(CMD_DIR)

build-linux:
	@echo "==> Кросс-компиляция статического бинарника для Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(APP_NAME) $(CMD_DIR)
	@echo "✅ Бинарник собран: $(BUILD_DIR)/$(APP_NAME)"

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)

# Быстрая заливка на VPS (требует SSH_HOST в окружении: make deploy SSH_HOST=root@your-vps)
deploy: build-linux
	@if [ -z "$(SSH_HOST)" ]; then echo "❌ Укажите SSH_HOST, например: make deploy SSH_HOST=root@1.2.3.4"; exit 1; fi
	scp $(BUILD_DIR)/$(APP_NAME) $(SSH_HOST):/usr/local/bin/$(APP_NAME)
	ssh $(SSH_HOST) "systemctl restart superbot && systemctl status superbot"

