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
		vpnWeight = "700"
		vpnMemHigh = "500M"
		aiWeight = "400"
		aiMemHigh = "950M"
		modeTitle = "🚀 РЕЖИМ: ТУРБО VPN"
		desc = "VPN (x-ui) получил приоритет сети и CPU. Сервисы ИИ защищены от троттлинга (950M RAM, 40% CPU)."

	case ModeAI:
		vpnWeight = "250"
		vpnMemHigh = "350M"
		aiWeight = "900"
		aiMemHigh = "1400M"
		modeTitle = "🧠 РЕЖИМ: СУПЕР-АГЕНТ (AI)"
		desc = "Antigravity/OmniRoute получил максимальный приоритет CPU/RAM. VPN сохраняет гарантированные ресурсы (350M)."

	case ModeBalanced, "balanced":
		vpnWeight = "500"
		vpnMemHigh = "450M"
		aiWeight = "600"
		aiMemHigh = "1100M"
		modeTitle = "⚖️ РЕЖИМ: СБАЛАНСИРОВАННЫЙ"
		desc = "Ресурсы сервера оптимально распределены между VPN-трафиком (x-ui 450M) и агентом (AI 1100M)."

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
