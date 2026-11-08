package sysinfo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SysInfo provides system monitoring and file management utilities.
type SysInfo struct {
	allowedBasePath string
}

// New creates a new SysInfo manager.
func New(allowedBasePath string) *SysInfo {
	if allowedBasePath == "" {
		allowedBasePath = "/"
	}
	return &SysInfo{
		allowedBasePath: filepath.Clean(allowedBasePath),
	}
}

// GetStats returns formatted system metrics: CPU, RAM, ZRAM/Swap, Disk, and Uptime.
func (s *SysInfo) GetStats() (string, error) {
	var sb strings.Builder
	sb.WriteString("<b>📊 Системные метрики VPS:</b>\n\n")

	// Host Uptime & Platform
	hInfo, err := host.Info()
	if err == nil {
		uptimeHours := hInfo.Uptime / 3600
		uptimeMinutes := (hInfo.Uptime % 3600) / 60
		sb.WriteString(fmt.Sprintf("⏱ <b>Uptime:</b> <code>%dч %dмин</code> (ОС: %s %s)\n", uptimeHours, uptimeMinutes, hInfo.Platform, hInfo.PlatformVersion))
	}

	// CPU Load
	cpuPercents, err := cpu.Percent(200*time.Millisecond, false)
	if err == nil && len(cpuPercents) > 0 {
		sb.WriteString(fmt.Sprintf("⚙️ <b>CPU:</b> <code>%.1f%%</code>", cpuPercents[0]))
	}
	lAvg, err := load.Avg()
	if err == nil {
		sb.WriteString(fmt.Sprintf(" | <b>LoadAvg:</b> <code>%.2f, %.2f, %.2f</code>\n\n", lAvg.Load1, lAvg.Load5, lAvg.Load15))
	} else {
		sb.WriteString("\n\n")
	}

	// RAM
	vMem, err := mem.VirtualMemory()
	if err == nil {
		usedMB := vMem.Used / (1024 * 1024)
		totalMB := vMem.Total / (1024 * 1024)
		freeMB := vMem.Available / (1024 * 1024)
		sb.WriteString(fmt.Sprintf("💾 <b>RAM:</b> <code>%d МБ / %d МБ</code> (<b>%.1f%%</b>, свободно: %d МБ)\n", usedMB, totalMB, vMem.UsedPercent, freeMB))
	}

	// Swap & ZRAM
	sMem, err := mem.SwapMemory()
	if err == nil && sMem.Total > 0 {
		swapUsedMB := sMem.Used / (1024 * 1024)
		swapTotalMB := sMem.Total / (1024 * 1024)
		sb.WriteString(fmt.Sprintf("📦 <b>Swap / ZRAM:</b> <code>%d МБ / %d МБ</code> (<b>%.1f%%</b>)\n", swapUsedMB, swapTotalMB, sMem.UsedPercent))
	}

	// Root Disk
	dUsage, err := disk.Usage("/")
	if err == nil {
		usedGB := float64(dUsage.Used) / (1024 * 1024 * 1024)
		totalGB := float64(dUsage.Total) / (1024 * 1024 * 1024)
		sb.WriteString(fmt.Sprintf("💽 <b>Диск (/):</b> <code>%.1f ГБ / %.1f ГБ</code> (<b>%.1f%%</b>)\n", usedGB, totalGB, dUsage.UsedPercent))
	}

	return sb.String(), nil
}

type procMetric struct {
	PID     int32
	Name    string
	CPU     float64
	MemPerc float32
	RSS     uint64
}

// GetTopProcesses returns top 5 processes sorted by memory consumption.
func (s *SysInfo) GetTopProcesses() (string, error) {
	procs, err := process.Processes()
	if err != nil {
		return "", fmt.Errorf("ошибка получения списка процессов: %w", err)
	}

	var metrics []procMetric
	for _, p := range procs {
		memP, err := p.MemoryPercent()
		if err != nil {
			continue
		}
		memInfo, err := p.MemoryInfo()
		if err != nil || memInfo == nil {
			continue
		}
		name, _ := p.Name()
		cpuP, _ := p.CPUPercent()

		metrics = append(metrics, procMetric{
			PID:     p.Pid,
			Name:    name,
			CPU:     cpuP,
			MemPerc: memP,
			RSS:     memInfo.RSS,
		})
	}

	// Sort by RSS/Memory descending
	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].RSS > metrics[j].RSS
	})

	limit := 5
	if len(metrics) < limit {
		limit = len(metrics)
	}

	var sb strings.Builder
	sb.WriteString("<b>🔥 Топ-5 процессов по потреблению RAM:</b>\n\n")
	sb.WriteString("<pre>")
	sb.WriteString(fmt.Sprintf("%-6s %-16s %-8s %s\n", "PID", "NAME", "RAM (МБ)", "%MEM"))
	sb.WriteString("----------------------------------------\n")
	for i := 0; i < limit; i++ {
		p := metrics[i]
		rssMB := p.RSS / (1024 * 1024)
		pName := p.Name
		if len(pName) > 15 {
			pName = pName[:14] + "…"
		}
		sb.WriteString(fmt.Sprintf("%-6d %-16s %-8d %.1f%%\n", p.PID, pName, rssMB, p.MemPerc))
	}
	sb.WriteString("</pre>")

	return sb.String(), nil
}

// GetNetStats returns network traffic and interface metrics.
func (s *SysInfo) GetNetStats() (string, error) {
	ioCounters, err := psnet.IOCounters(false)
	if err != nil || len(ioCounters) == 0 {
		return "", fmt.Errorf("не удалось получить сетевую статистику: %w", err)
	}

	total := ioCounters[0]
	sentMB := float64(total.BytesSent) / (1024 * 1024)
	recvMB := float64(total.BytesRecv) / (1024 * 1024)

	var sb strings.Builder
	sb.WriteString("<b>🌐 Сетевая активность (Все интерфейсы):</b>\n\n")
	sb.WriteString(fmt.Sprintf("⬆️ <b>Отправлено:</b> <code>%.2f МБ</code> (пакетов: %d)\n", sentMB, total.PacketsSent))
	sb.WriteString(fmt.Sprintf("⬇️ <b>Получено:</b> <code>%.2f МБ</code> (пакетов: %d)\n\n", recvMB, total.PacketsRecv))

	interfaces, err := psnet.Interfaces()
	if err == nil {
		sb.WriteString("<b>Сетевые интерфейсы:</b>\n<pre>")
		for _, iface := range interfaces {
			if strings.HasPrefix(iface.Name, "lo") {
				continue
			}
			var addrs []string
			for _, a := range iface.Addrs {
				if !strings.Contains(a.Addr, ":") { // IPv4
					addrs = append(addrs, a.Addr)
				}
			}
			sb.WriteString(fmt.Sprintf("• %-10s %s\n", iface.Name, strings.Join(addrs, ", ")))
		}
		sb.WriteString("</pre>")
	}

	return sb.String(), nil
}

// ListFiles lists directory contents with file size and type.
func (s *SysInfo) ListFiles(targetPath string) (string, error) {
	if targetPath == "" {
		targetPath = "."
	}

	cleanPath := filepath.Clean(targetPath)
	if !filepath.IsAbs(cleanPath) {
		cwd, err := os.Getwd()
		if err == nil {
			cleanPath = filepath.Join(cwd, cleanPath)
		}
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения папки %s: %w", cleanPath, err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📁 <b>Содержимое:</b> <code>%s</code>\n\n", cleanPath))

	if len(entries) == 0 {
		sb.WriteString("<i>(Папка пуста)</i>")
		return sb.String(), nil
	}

	count := 0
	for _, entry := range entries {
		count++
		if count > 40 {
			sb.WriteString(fmt.Sprintf("\n<i>... и еще %d элементов (вывод ограничен)</i>", len(entries)-40))
			break
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if entry.IsDir() {
			sb.WriteString(fmt.Sprintf("📁 <b>%s/</b>\n", entry.Name()))
		} else {
			sizeStr := formatFileSize(info.Size())
			sb.WriteString(fmt.Sprintf("📄 <code>%-24s</code> (%s)\n", entry.Name(), sizeStr))
		}
	}

	return sb.String(), nil
}

// FindFiles searches for files matching a pattern with a timeout.
func (s *SysInfo) FindFiles(pattern string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "Укажите имя или маску для поиска: <code>/find config</code>", nil
	}

	searchRoot := s.allowedBasePath
	var matches []string

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if name == "proc" || name == "sys" || name == "dev" || name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
		}

		if strings.Contains(strings.ToLower(d.Name()), strings.ToLower(pattern)) {
			matches = append(matches, path)
			if len(matches) >= 20 {
				return fmt.Errorf("limit reached")
			}
		}
		return nil
	})

	if err != nil && err != context.DeadlineExceeded && err.Error() != "limit reached" {
		return "", err
	}

	if len(matches) == 0 {
		return fmt.Sprintf("🔍 По запросу <code>%s</code> ничего не найдено.", pattern), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 <b>Найдено (%d):</b>\n\n", len(matches)))
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("• <code>%s</code>\n", m))
	}
	if len(matches) >= 20 {
		sb.WriteString("\n<i>(Показаны первые 20 результатов)</i>")
	}

	return sb.String(), nil
}

// ValidateFileForSending verifies a file path before sending via Telegram.
func (s *SysInfo) ValidateFileForSending(filePath string) (string, error) {
	cleanPath := filepath.Clean(filePath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return "", fmt.Errorf("файл не найден: %s", cleanPath)
	}
	if info.IsDir() {
		return "", fmt.Errorf("указанный путь является директорией: %s", cleanPath)
	}

	const maxFileSize = 50 * 1024 * 1024 // 50MB
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("размер файла (%s) превышает лимит Telegram (50 МБ)", formatFileSize(info.Size()))
	}

	return cleanPath, nil
}

func formatFileSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
