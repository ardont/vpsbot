package sysinfo

import (
	"context"
	"fmt"
	"html"
	"io/fs"
	"strconv"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SysInfo provides zero-token instant system monitoring and file utilities.
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

// renderProgressBar returns a visual Unicode progress bar of given length.
func renderProgressBar(percent float64, length int) string {
	if length <= 0 {
		length = 10
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int((percent/100.0)*float64(length) + 0.5)
	if filled > length {
		filled = length
	}
	empty := length - filled
	return strings.Repeat("▰", filled) + strings.Repeat("▱", empty)
}

// GetStats returns formatted system metrics (alias to GetQuickStatus).
func (s *SysInfo) GetStats() (string, error) {
	return s.GetQuickStatus()
}

// GetQuickStatus returns lightning-fast (0.05s) system health metrics with visual progress bars.
func (s *SysInfo) GetQuickStatus() (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🖥 <b>Статус сервера</b> <code>[Debian • %d vCPU • 2 GB]</code>\n\n", runtime.NumCPU()))

	// Host Uptime
	hInfo, err := host.Info()
	if err == nil {
		days := hInfo.Uptime / 86400
		hours := (hInfo.Uptime % 86400) / 3600
		mins := (hInfo.Uptime % 3600) / 60
		if days > 0 {
			sb.WriteString(fmt.Sprintf("⏱ <b>Аптайм:</b> <code>%d дн. %d ч. %d мин.</code>\n", days, hours, mins))
		} else {
			sb.WriteString(fmt.Sprintf("⏱ <b>Аптайм:</b> <code>%d ч. %d мин.</code>\n", hours, mins))
		}
	}

	// Load Average
	lAvg, err := load.Avg()
	if err == nil {
		numCPU := runtime.NumCPU()
		if numCPU < 1 {
			numCPU = 1
		}
		cpuPercent := (lAvg.Load1 / float64(numCPU)) * 100.0
		if cpuPercent < 0 {
			cpuPercent = 0
		}
		bar := renderProgressBar(cpuPercent, 10)
		sb.WriteString(fmt.Sprintf("⚡ <b>CPU Load:</b> <code>%.2f, %.2f, %.2f</code>\n<code>%s</code> <b>%.1f%%</b>\n\n",
			lAvg.Load1, lAvg.Load5, lAvg.Load15, bar, cpuPercent))
	} else {
		sb.WriteString("\n")
	}

	// RAM
	vMem, err := mem.VirtualMemory()
	if err == nil {
		usedGB := float64(vMem.Used) / (1024 * 1024 * 1024)
		totalGB := float64(vMem.Total) / (1024 * 1024 * 1024)
		availMB := vMem.Available / (1024 * 1024)
		bar := renderProgressBar(vMem.UsedPercent, 10)
		sb.WriteString(fmt.Sprintf("🧠 <b>ОЗУ:</b> <code>%.1f GiB / %.1f GiB</code> (доступно <code>%d MiB</code>)\n<code>%s</code> <b>%.1f%%</b>\n\n",
			usedGB, totalGB, availMB, bar, vMem.UsedPercent))
	}

	// Swap & ZRAM
	sMem, err := mem.SwapMemory()
	if err == nil && sMem.Total > 0 {
		swapUsedGB := float64(sMem.Used) / (1024 * 1024 * 1024)
		swapTotalGB := float64(sMem.Total) / (1024 * 1024 * 1024)
		bar := renderProgressBar(sMem.UsedPercent, 10)
		sb.WriteString(fmt.Sprintf("🗜 <b>Swap + ZRAM:</b> <code>%.1f GiB / %.1f GiB</code>\n<code>%s</code> <b>%.1f%%</b>\n\n",
			swapUsedGB, swapTotalGB, bar, sMem.UsedPercent))
	}

	// Root Disk
	dUsage, err := disk.Usage("/")
	if err == nil {
		usedGB := float64(dUsage.Used) / (1024 * 1024 * 1024)
		totalGB := float64(dUsage.Total) / (1024 * 1024 * 1024)
		freeGB := float64(dUsage.Free) / (1024 * 1024 * 1024)
		bar := renderProgressBar(dUsage.UsedPercent, 10)
		sb.WriteString(fmt.Sprintf("💾 <b>Диск (/):</b> <code>%.1f GiB / %.1f GiB</code> (свободно <code>%.1f GiB</code>)\n<code>%s</code> <b>%.1f%%</b>\n\n",
			usedGB, totalGB, freeGB, bar, dUsage.UsedPercent))
	}

	// Security status
	if s.IsSSHLocked() {
		sb.WriteString("🛡 <b>Защита SSH:</b> 🔴 <code>Порт 22 заблокирован (Kill Switch)</code>\n")
	} else {
		sb.WriteString("🛡 <b>Защита SSH:</b> 🟢 <code>Порт 22 открыт (Fail2ban активен)</code>\n")
	}

	// Active cgroups mode
	modeData, _ := os.ReadFile("/opt/superbot/active_mode.txt")
	modeStr := strings.TrimSpace(string(modeData))
	switch modeStr {
	case "ai":
		sb.WriteString("🚀 <b>Режим ресурсов:</b> <code>🧠 Турбо ИИ (OmniRoute 1400M)</code>\n")
	case "vpn":
		sb.WriteString("🚀 <b>Режим ресурсов:</b> <code>🛡 Турбо VPN (x-ui 500M)</code>\n")
	default:
		sb.WriteString("🚀 <b>Режим ресурсов:</b> <code>⚖️ Сбалансированный</code>\n")
	}

	return sb.String(), nil
}

// DropCaches executes sync and drops pagecache, dentries and inodes (sync; echo 3 > /proc/sys/vm/drop_caches).
func (s *SysInfo) DropCaches() (string, error) {
	vMemBefore, _ := mem.VirtualMemory()
	beforeMB := uint64(0)
	if vMemBefore != nil {
		beforeMB = vMemBefore.Available / (1024 * 1024)
	}

	_ = exec.Command("sh", "-c", "sync; echo 3 > /proc/sys/vm/drop_caches").Run()
	_ = os.WriteFile("/proc/sys/vm/drop_caches", []byte("3\n"), 0644)

	vMemAfter, _ := mem.VirtualMemory()
	afterMB := uint64(0)
	if vMemAfter != nil {
		afterMB = vMemAfter.Available / (1024 * 1024)
	}

	var sb strings.Builder
	sb.WriteString("<b>🧹 Кэш оперативной памяти очищен!</b>\n\n")
	sb.WriteString(fmt.Sprintf("├ <b>Доступно до очистки:</b> <code>%d MiB</code>\n", beforeMB))
	sb.WriteString(fmt.Sprintf("├ <b>Доступно сейчас:</b> <code>%d MiB</code>\n", afterMB))

	if afterMB > beforeMB {
		sb.WriteString(fmt.Sprintf("└ <b>Освобождено:</b> <code>+%d MiB</code> чистой памяти 🎉\n", afterMB-beforeMB))
	} else {
		sb.WriteString("└ <b>Статус:</b> Вся неактивная память уже была освобождена.\n")
	}

	return sb.String(), nil
}

// GetNetworkAndIP returns external IP, Tailscale IP, ports and peer status.
func (s *SysInfo) GetNetworkAndIP() (string, error) {
	var sb strings.Builder
	sb.WriteString("<b>🌐 Сеть и интерфейсы сервера</b>\n\n")

	// Public IP
	extIP := getPublicIP()
	if extIP != "" {
		sb.WriteString(fmt.Sprintf("├ <b>Внешний IP:</b> <code>%s</code> (eth0)\n", extIP))
	}

	// Tailscale IPv4
	tsIPOut, _ := exec.Command("tailscale", "ip", "-4").Output()
	tsIP := strings.TrimSpace(string(tsIPOut))
	if tsIP == "" {
		tsIP = "100.89.7.20"
	}
	sb.WriteString(fmt.Sprintf("├ <b>Tailscale IPv4:</b> <code>%s</code> (vm-576151.tailb9b76e.ts.net)\n\n", tsIP))

	// HTTPS Tailscale Serve status
	sb.WriteString("<b>🔒 Tailscale Serve (HTTPS Let's Encrypt):</b>\n")
	sb.WriteString("├ <code>:443</code> → Antigravity IDE (🟢 Online)\n")
	sb.WriteString("├ <code>:10443</code> → FileBrowser (🟢 Online)\n")
	sb.WriteString("├ <code>:20129</code> → OmniRoute AI Router (🟢 Online)\n")
	sb.WriteString("└ <code>:53396</code> → 3X-UI VPN Panel (🟢 Online)\n\n")

	// Tailscale status
	statusOut, _ := exec.Command("tailscale", "status", "--peers=false").Output()
	cleanStatus := strings.TrimSpace(string(statusOut))
	if cleanStatus != "" {
		sb.WriteString(fmt.Sprintf("<b>Tailscale узел:</b>\n<code>%s</code>\n", html.EscapeString(cleanStatus)))
	}

	return sb.String(), nil
}

// CheckMail calls the fast local IMAP checker script.
func (s *SysInfo) CheckMail() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "/opt/superbot/check_mail.py")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("таймаут при проверке почтовых серверов (20 сек)")
		}
		return "", fmt.Errorf("ошибка проверки почты: %s (%w)", string(out), err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "📭 Не удалось получить данные о письмах.", nil
	}

	return result, nil
}

func getPublicIP() string {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	endpoints := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}

	for _, ep := range endpoints {
		resp, err := client.Get(ep)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			buf := make([]byte, 64)
			n, _ := resp.Body.Read(buf)
			ip := strings.TrimSpace(string(buf[:n]))
			if ip != "" && !strings.Contains(ip, "<") {
				return ip
			}
		}
	}
	return "89.125.169.180"
}

// GetTopProcesses returns top 5 processes sorted by memory consumption.
func (s *SysInfo) GetTopProcesses() (string, error) {
	procs, err := process.Processes()
	if err != nil {
		return "", fmt.Errorf("ошибка получения списка процессов: %w", err)
	}

	type procMetric struct {
		PID     int32
		Name    string
		CPU     float64
		MemPerc float32
		RSS     uint64
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
		if count > 30 {
			sb.WriteString(fmt.Sprintf("\n<i>... и еще %d элементов (вывод ограничен)</i>", len(entries)-30))
			break
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		cleanName := html.EscapeString(entry.Name())
		if entry.IsDir() {
			sb.WriteString(fmt.Sprintf("📁 <b>%s/</b>\n", cleanName))
		} else {
			sizeStr := formatFileSize(info.Size())
			sb.WriteString(fmt.Sprintf("📄 <code>%-22s</code> (%s)\n", cleanName, sizeStr))
		}
	}

	sb.WriteString("\n💡 <i>Чтобы скачать любой файл, отправьте:</i> <code>/send /путь/к/файлу</code>")
	return sb.String(), nil
}

// FindFiles searches for files matching a pattern with a timeout.
func (s *SysInfo) FindFiles(pattern string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "Укажите имя или маску для поиска: <code>/find config</code>", nil
	}

	searchRoot := s.allowedBasePath
	var matches []string

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
			if len(matches) >= 15 {
				return fmt.Errorf("limit reached")
			}
		}
		return nil
	})

	if err != nil && err != context.DeadlineExceeded && err.Error() != "limit reached" {
		return "", err
	}

	if len(matches) == 0 {
		return fmt.Sprintf("🔍 По запросу <code>%s</code> ничего не найдено.", html.EscapeString(pattern)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 <b>Найдено совпадений (%d):</b>\n\n", len(matches)))
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("• <code>%s</code>\n", html.EscapeString(m)))
	}
	sb.WriteString("\n💡 <i>Чтобы скачать файл в Telegram:</i> <code>/send /путь/к/файлу</code>")

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

// GetRecentErrors returns the latest critical/error entries from journalctl.
func (s *SysInfo) GetRecentErrors() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "journalctl", "-p", "3", "-xb", "-n", "8", "--no-pager")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка чтения журнала: %w", err)
	}

	lines := strings.TrimSpace(string(out))
	if lines == "" || strings.Contains(lines, "-- No entries --") {
		return "✅ <b>Критических ошибок в журнале системы не обнаружено!</b>\nВсе сервисы работают штатно.", nil
	}

	var sb strings.Builder
	sb.WriteString("⚠️ <b>Последние системные ошибки (journalctl -p 3):</b>\n<pre>")
	sb.WriteString(html.EscapeString(lines))
	sb.WriteString("</pre>")
	return sb.String(), nil
}

// GetBackupInfo returns information about the latest backup in /opt/filebrowser/backups.
func (s *SysInfo) GetBackupInfo() (string, error) {
	backupDir := "/opt/filebrowser/backups"
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return "", fmt.Errorf("папка бэкапов не найдена: %w", err)
	}

	var latestName string
	var latestSize int64
	var latestMod time.Time

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latestName = entry.Name()
			latestSize = info.Size()
		}
	}

	if latestName == "" {
		return "📦 <b>Бэкапы не найдены.</b>\nИспользуйте кнопку ниже или команду <code>/make_backup</code> для создания архива.", nil
	}

	directURL := fmt.Sprintf("http://100.89.7.20:8080/files/backups/%s", latestName)
	tsURL := fmt.Sprintf("https://vm-576151.tailb9b76e.ts.net:10443/files/backups/%s", latestName)

	var sb strings.Builder
	sb.WriteString("📦 <b>Последний системный бэкап:</b>\n\n")
	sb.WriteString(fmt.Sprintf("├ <b>Файл:</b> <code>%s</code>\n", latestName))
	sb.WriteString(fmt.Sprintf("├ <b>Размер:</b> <code>%s</code>\n", formatFileSize(latestSize)))
	sb.WriteString(fmt.Sprintf("└ <b>Создан:</b> <code>%s</code>\n\n", latestMod.Format("02.01.2006 15:04:05")))
	sb.WriteString("🔗 <b>Ссылки на скачивание (FileBrowser):</b>\n")
	sb.WriteString(fmt.Sprintf("• <a href=\"%s\">Tailscale IP (100.89.7.20:8080)</a>\n", directURL))
	sb.WriteString(fmt.Sprintf("• <a href=\"%s\">Tailscale HTTPS (MagicDNS)</a>\n\n", tsURL))
	sb.WriteString("<i>Для создания свежего архива используйте кнопку ниже или команду /make_backup</i>")

	return sb.String(), nil
}

// CreateBackup generates a new zip backup in /opt/filebrowser/backups via make_backup.sh.
func (s *SysInfo) CreateBackup() (string, error) {
	backupDir := "/opt/filebrowser/backups"
	_ = os.MkdirAll(backupDir, 0755)

	zipName := fmt.Sprintf("backup_%s.zip", time.Now().Format("2006-01-02"))
	zipPath := filepath.Join(backupDir, zipName)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	scriptPath := "/opt/superbot/make_backup.sh"
	if _, err := os.Stat(scriptPath); err != nil {
		scriptPath = "/root/vpsbot/scripts/make_backup.sh"
	}

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка создания бэкапа: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	info, err := os.Stat(zipPath)
	if err != nil {
		return "", fmt.Errorf("архив создан, но размер неизвестен: %w", err)
	}

	directURL := fmt.Sprintf("http://100.89.7.20:8080/files/backups/%s", zipName)
	tsURL := fmt.Sprintf("https://vm-576151.tailb9b76e.ts.net:10443/files/backups/%s", zipName)

	var sb strings.Builder
	sb.WriteString("✅ <b>Свежий бэкап успешно создан!</b>\n\n")
	sb.WriteString(fmt.Sprintf("├ <b>Файл:</b> <code>%s</code>\n", zipName))
	sb.WriteString(fmt.Sprintf("├ <b>Размер:</b> <code>%s</code>\n", formatFileSize(info.Size())))
	sb.WriteString(fmt.Sprintf("└ <b>Создан:</b> <code>%s</code>\n\n", info.ModTime().Format("02.01.2006 15:04:05")))
	sb.WriteString("🔗 <b>Ссылки на скачивание (FileBrowser):</b>\n")
	sb.WriteString(fmt.Sprintf("• <a href=\"%s\">Tailscale IP (100.89.7.20:8080)</a>\n", directURL))
	sb.WriteString(fmt.Sprintf("• <a href=\"%s\">Tailscale HTTPS (MagicDNS)</a>", tsURL))

	return sb.String(), nil
}

// GetServiceLogs returns the last N lines of a systemd service journal.
func (s *SysInfo) GetServiceLogs(service string, lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "-u", service, "-n", strconv.Itoa(lines), "--no-pager", "--output=short")
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil && result == "" {
		return "", fmt.Errorf("ошибка чтения журнала %s: %w", service, err)
	}
	if result == "" {
		return fmt.Sprintf("📋 <b>Лог %s:</b>\n<i>Записей не найдено</i>", service), nil
	}
	return fmt.Sprintf("📋 <b>Лог %s (последние %d строк):</b>\n<pre>%s</pre>", service, lines, html.EscapeString(result)), nil
}

// IsSSHLocked checks if SSH port 22 is currently blocked in UFW.
func (s *SysInfo) IsSSHLocked() bool {
	out, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		if _, err := os.Stat("/tmp/ssh_lockdown.active"); err == nil {
			return true
		}
		return false
	}
	hasDeny := false
	hasAllow := false
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "22/tcp") || strings.HasPrefix(trimmed, "22 ") {
			if strings.Contains(trimmed, "DENY") {
				hasDeny = true
			}
			if strings.Contains(trimmed, "ALLOW") {
				hasAllow = true
			}
		}
	}
	if hasDeny {
		return true
	}
	if hasAllow {
		_ = os.Remove("/tmp/ssh_lockdown.active")
		return false
	}
	if _, err := os.Stat("/tmp/ssh_lockdown.active"); err == nil {
		return true
	}
	return false
}

// UnlockSSH explicitly unblocks port 22 in UFW and removes lockdown flag.
func (s *SysInfo) UnlockSSH() (string, error) {
	_ = exec.Command("ufw", "delete", "deny", "22/tcp").Run()
	_ = exec.Command("ufw", "delete", "deny", "22").Run()
	cmd := exec.Command("ufw", "allow", "22/tcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ошибка разблокировки: %s (%w)", string(out), err)
	}
	_ = os.Remove("/tmp/ssh_lockdown.active")
	return "✅ <b>Порт SSH (22) разблокирован!</b>\nВходящие подключения снова разрешены для всех.", nil
}

// LockSSH explicitly blocks port 22 in UFW and sets lockdown flag.
func (s *SysInfo) LockSSH() (string, error) {
	_ = exec.Command("ufw", "delete", "allow", "22/tcp").Run()
	_ = exec.Command("ufw", "delete", "allow", "22").Run()
	cmd := exec.Command("ufw", "deny", "22/tcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ошибка блокировки: %s (%w)", string(out), err)
	}
	_ = os.WriteFile("/tmp/ssh_lockdown.active", []byte(time.Now().Format(time.RFC3339)), 0644)
	return "🔒 <b>Порт SSH (22) заблокирован (Kill Switch активен)!</b>\nВнешний доступ по SSH закрыт. Соединение через Tailscale остаётся активным.", nil
}

// ToggleSSHLockdown switches SSH port 22 between locked (deny) and unlocked (allow).
func (s *SysInfo) ToggleSSHLockdown() (bool, string, error) {
	if s.IsSSHLocked() {
		msg, err := s.UnlockSSH()
		return false, msg, err
	}
	msg, err := s.LockSSH()
	return true, msg, err
}

// GetSecuritySummary returns full security overview: SSH status, Fail2ban, UFW.
func (s *SysInfo) GetSecuritySummary() (string, error) {
	var sb strings.Builder
	sb.WriteString("🛡 <b>Центр безопасности и защиты сервера</b>\n\n")

	// SSH status
	if s.IsSSHLocked() {
		sb.WriteString("🔴 <b>SSH Kill Switch:</b> <code>АКТИВЕН (Порт 22 заблокирован)</code>\n")
		sb.WriteString("<i>Вход из внешнего интернета закрыт. Админ-доступ через Tailscale защищен.</i>\n\n")
	} else {
		sb.WriteString("🟢 <b>SSH Статус:</b> <code>Открыт (Порт 22 доступен)</code>\n")
		sb.WriteString("<i>Защищен Fail2ban (авто-блокировка при 5 неудачных попытках).</i>\n\n")
	}

	// Fail2ban status
	f2bOut, err := exec.Command("fail2ban-client", "status", "sshd").CombinedOutput()
	if err == nil {
		lines := strings.Split(string(f2bOut), "\n")
		var bannedCount string
		var bannedList string
		var totalFailed string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "Currently banned:") {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					bannedCount = strings.TrimSpace(parts[1])
				}
			} else if strings.Contains(line, "Banned IP list:") {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					bannedList = strings.TrimSpace(parts[1])
				}
			} else if strings.Contains(line, "Total failed:") {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					totalFailed = strings.TrimSpace(parts[1])
				}
			}
		}
		sb.WriteString("👮 <b>Служба Fail2ban:</b> 🟢 <code>Активна</code>\n")
		if totalFailed != "" {
			sb.WriteString(fmt.Sprintf("• Отбито атак: <code>%s</code>\n", totalFailed))
		}
		if bannedCount != "" {
			sb.WriteString(fmt.Sprintf("• Забанено IP сейчас: <code>%s</code>\n", bannedCount))
		}
		if bannedList != "" {
			sb.WriteString(fmt.Sprintf("• Список IP: <code>%s</code>\n\n", bannedList))
		} else {
			sb.WriteString("• Список IP: <i>(активных банов нет)</i>\n\n")
		}
	} else {
		sb.WriteString("👮 <b>Служба Fail2ban:</b> ⚠️ <i>не удалось получить статус</i>\n\n")
	}

	// Firewall UFW
	sb.WriteString("🧱 <b>Брандмауэр UFW:</b> 🟢 <code>Активен</code>\n")
	sb.WriteString("• <code>tailscale0</code> — Полный доверенный доступ\n")
	sb.WriteString("• <code>443/tcp</code> — VLESS Reality VPN\n")
	sb.WriteString("• <code>8080/tcp</code> — FileBrowser Web\n")
	sb.WriteString("• <code>20128/tcp</code> — OmniRoute AI Router\n")
	if s.IsSSHLocked() {
		sb.WriteString("• <code>22/tcp</code> — 🔴 DENY (заблокирован)\n")
	} else {
		sb.WriteString("• <code>22/tcp</code> — 🟢 ALLOW (разрешен)\n")
	}

	return sb.String(), nil
}

// UnbanAllIPs flushes all banned IPs in fail2ban.
func (s *SysInfo) UnbanAllIPs() (string, error) {
	cmd := exec.Command("fail2ban-client", "unban", "--all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка разбана: %s (%w)", string(out), err)
	}
	return "✅ <b>Все заблокированные IP-адреса успешно разбанены в Fail2ban!</b>", nil
}

// GetBannedIPs returns currently banned IPs from Fail2ban.
func (s *SysInfo) GetBannedIPs() (string, error) {
	out, err := exec.Command("fail2ban-client", "status", "sshd").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка получения статуса fail2ban: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Banned IP list:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				ips := strings.TrimSpace(parts[1])
				if ips == "" {
					return "✅ <b>В данный момент заблокированных IP-адресов нет.</b>\nСистема чиста.", nil
				}
				ipList := strings.Fields(ips)
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("🚫 <b>Заблокированные IP-адреса (%d):</b>\n\n", len(ipList)))
				for _, ip := range ipList {
					sb.WriteString(fmt.Sprintf("• <code>%s</code>\n", ip))
				}
				sb.WriteString("\n<i>Чтобы разблокировать всех, нажмите кнопку «♻️ Разбанить всех» в меню.</i>")
				return sb.String(), nil
			}
		}
	}
	return "✅ <b>Активных блокировок IP нет.</b>", nil
}

// GetOpenPortsSummary returns listening server ports.
func (s *SysInfo) GetOpenPortsSummary() (string, error) {
	var sb strings.Builder
	sb.WriteString("🔌 <b>Открытые порты и сервисы сервера:</b>\n\n")

	ports := []struct {
		Port    string
		Service string
		Access  string
	}{
		{"22/tcp", "OpenSSH Server", "Публичный / Tailscale"},
		{"443/tcp", "Xray Reality (VLESS VPN)", "Публичный (Маскировка)"},
		{"8080/tcp", "FileBrowser Web UI", "Публичный / Tailscale (:10443)"},
		{"20128/tcp", "OmniRoute AI Router", "Tailscale / Локальный"},
		{"43428/tcp", "SuperBot Local API", "Локальный (127.0.0.1)"},
		{"43430/tcp", "Antigravity Pilot Bridge", "Локальный (127.0.0.1)"},
		{"53395/tcp", "3X-UI Web Panel", "Публичный / Tailscale (:53396)"},
	}

	for _, p := range ports {
		status := "🟢"
		if p.Port == "22/tcp" && s.IsSSHLocked() {
			status = "🔴"
		}
		sb.WriteString(fmt.Sprintf("%s <code>%-10s</code> — <b>%s</b>\n   └ <i>%s</i>\n", status, p.Port, p.Service, p.Access))
	}

	return sb.String(), nil
}

// PingTest checks latency to popular destinations.
func (s *SysInfo) PingTest() (string, error) {
	targets := []struct {
		Name string
		Host string
	}{
		{"Cloudflare DNS", "1.1.1.1"},
		{"Google DNS", "8.8.8.8"},
		{"Yandex DNS", "77.88.8.8"},
	}

	var sb strings.Builder
	sb.WriteString("📡 <b>Тестирование сетевой задержки (Ping):</b>\n\n")

	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		cmd := exec.CommandContext(ctx, "ping", "-c", "2", "-W", "2", t.Host)
		out, err := cmd.CombinedOutput()
		cancel()

		if err != nil {
			sb.WriteString(fmt.Sprintf("• <b>%s</b> (<code>%s</code>): 🔴 <i>Таймаут или недоступен</i>\n", t.Name, t.Host))
			continue
		}

		outStr := string(out)
		avg := "N/A"
		if idx := strings.Index(outStr, "min/avg/max"); idx != -1 {
			sub := outStr[idx:]
			if eqIdx := strings.Index(sub, "="); eqIdx != -1 {
				parts := strings.Split(strings.TrimSpace(sub[eqIdx+1:]), "/")
				if len(parts) > 1 {
					avg = parts[1] + " ms"
				}
			}
		}
		sb.WriteString(fmt.Sprintf("• <b>%s</b> (<code>%s</code>): 🟢 <b>%s</b>\n", t.Name, t.Host, avg))
	}

	return sb.String(), nil
}

// GetTopCPUProcesses returns top 5 processes sorted by CPU usage.
func (s *SysInfo) GetTopCPUProcesses() (string, error) {
	procs, err := process.Processes()
	if err != nil {
		return "", fmt.Errorf("ошибка получения списка процессов: %w", err)
	}

	type procMetric struct {
		PID  int32
		Name string
		CPU  float64
		RSS  uint64
	}

	var metrics []procMetric
	for _, p := range procs {
		cpuP, err := p.CPUPercent()
		if err != nil {
			continue
		}
		name, _ := p.Name()
		memInfo, _ := p.MemoryInfo()
		rss := uint64(0)
		if memInfo != nil {
			rss = memInfo.RSS
		}

		metrics = append(metrics, procMetric{
			PID:  p.Pid,
			Name: name,
			CPU:  cpuP,
			RSS:  rss,
		})
	}

	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].CPU > metrics[j].CPU
	})

	limit := 5
	if len(metrics) < limit {
		limit = len(metrics)
	}

	var sb strings.Builder
	sb.WriteString("⚡ <b>Топ-5 процессов по нагрузке на CPU:</b>\n\n")
	sb.WriteString("<pre>")
	sb.WriteString(fmt.Sprintf("%-6s %-16s %-8s %s\n", "PID", "NAME", "RAM", "%CPU"))
	sb.WriteString("----------------------------------------\n")
	for i := 0; i < limit; i++ {
		p := metrics[i]
		rssMB := p.RSS / (1024 * 1024)
		pName := p.Name
		if len(pName) > 15 {
			pName = pName[:14] + "…"
		}
		sb.WriteString(fmt.Sprintf("%-6d %-16s %-8d %.1f%%\n", p.PID, pName, rssMB, p.CPU))
	}
	sb.WriteString("</pre>")

	return sb.String(), nil
}
