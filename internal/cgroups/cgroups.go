package cgroups

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Mode represents an operating resource allocation profile.
type Mode string

const (
	ModeVPN      Mode = "vpn"
	ModeAI       Mode = "ai"
	ModeBalanced Mode = "auto"
)

// Manager manages dynamic priority allocations using cgroups v2 / systemd slices.
type Manager struct {
	vpnService string
	aiService  string
}

// New creates a new cgroups Manager.
func New(vpnService, aiService string) *Manager {
	if vpnService == "" {
		vpnService = "x-ui.service"
	}
	if aiService == "" {
		aiService = "omniroute.service"
	}
	return &Manager{
		vpnService: vpnService,
		aiService:  aiService,
	}
}

// SetMode dynamically changes resource weights for vpn.slice and ai.slice.
func (m *Manager) SetMode(mode Mode) (string, error) {
	var (
		vpnWeight  string
		vpnMemHigh string
		aiWeight   string
		aiMemHigh  string
		modeTitle  string
		desc       string
	)

	switch mode {
	case ModeVPN:
		vpnWeight = "900"
		vpnMemHigh = "1200M"
		aiWeight = "50"
		aiMemHigh = "150M"
		modeTitle = "🚀 РЕЖИМ: ТУРБО VPN"
		desc = "VPN (x-ui) получил максимальный приоритет процессора и памяти. Фоновые процессы ИИ ограничены."

	case ModeAI:
		vpnWeight = "100"
		vpnMemHigh = "300M"
		aiWeight = "800"
		aiMemHigh = "1300M"
		modeTitle = "🧠 РЕЖИМ: СУПЕР-АГЕНТ (AI)"
		desc = "Antigravity/OmniRoute получил максимальный приоритет CPU/RAM. VPN в энергосберегающем режиме."

	case ModeBalanced, "balanced":
		vpnWeight = "500"
		vpnMemHigh = "600M"
		aiWeight = "500"
		aiMemHigh = "800M"
		modeTitle = "⚖️ РЕЖИМ: СБАЛАНСИРОВАННЫЙ"
		desc = "Ресурсы сервера поровну распределены между VPN-трафиком (x-ui) и агентом."

	default:
		return "", fmt.Errorf("неизвестный режим '%s'. Доступные: vpn, ai, auto", mode)
	}

	if runtime.GOOS != "linux" {
		return fmt.Sprintf("⚠️ Переключение приоритетов (%s) работает только на Linux c cgroups v2.", mode), nil
	}

	// Apply properties to vpn.slice
	if err := exec.Command("systemctl", "set-property", "vpn.slice", fmt.Sprintf("CPUWeight=%s", vpnWeight), fmt.Sprintf("MemoryHigh=%s", vpnMemHigh)).Run(); err != nil {
		return "", fmt.Errorf("ошибка настройки vpn.slice: %w", err)
	}

	// Apply properties to ai.slice
	if err := exec.Command("systemctl", "set-property", "ai.slice", fmt.Sprintf("CPUWeight=%s", aiWeight), fmt.Sprintf("MemoryHigh=%s", aiMemHigh)).Run(); err != nil {
		return "", fmt.Errorf("ошибка настройки ai.slice: %w", err)
	}

	return fmt.Sprintf(
		"<b>%s</b>\n\n"+
			"%s\n\n"+
			"<b>🛡️ vpn.slice:</b> CPUWeight=%s, MemoryHigh=%s\n"+
			"<b>🧠 ai.slice:</b> CPUWeight=%s, MemoryHigh=%s",
		modeTitle, desc, vpnWeight, vpnMemHigh, aiWeight, aiMemHigh,
	), nil
}

// RestartVPN restarts the configured VPN service (e.g. x-ui.service).
func (m *Manager) RestartVPN() (string, error) {
	if runtime.GOOS != "linux" {
		return "⚠️ Перезапуск сервисов доступен только на Linux сервере.", nil
	}

	cmd := exec.Command("systemctl", "restart", m.vpnService)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка перезапуска %s: %s (%w)", m.vpnService, string(out), err)
	}

	statusCmd := exec.Command("systemctl", "is-active", m.vpnService)
	statusOut, _ := statusCmd.Output()
	statusStr := strings.TrimSpace(string(statusOut))

	return fmt.Sprintf("🔄 Сервис <b>%s</b> перезапущен.\nСтатус: <code>%s</code>", m.vpnService, statusStr), nil
}

// RestartAI restarts the AI/Gateway service (e.g. omniroute.service).
func (m *Manager) RestartAI() (string, error) {
	if runtime.GOOS != "linux" {
		return "⚠️ Перезапуск сервисов доступен только на Linux сервере.", nil
	}

	cmd := exec.Command("systemctl", "restart", m.aiService)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка перезапуска %s: %s (%w)", m.aiService, string(out), err)
	}

	statusCmd := exec.Command("systemctl", "is-active", m.aiService)
	statusOut, _ := statusCmd.Output()
	statusStr := strings.TrimSpace(string(statusOut))

	return fmt.Sprintf("🔄 Сервис <b>%s</b> перезапущен.\nСтатус: <code>%s</code>", m.aiService, statusStr), nil
}
