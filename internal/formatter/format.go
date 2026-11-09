package formatter

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	codeBlockRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_-]*)\\r?\\n(.*?)```")
	inlineCodeRe = regexp.MustCompile("`([^`\\n]+)`")
	mdLinkRe    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\)]+)\)`)
	fileLinkRe  = regexp.MustCompile(`\[([^\]]+)\]\(file://([^\)]+)\)`)
	rawFileRe   = regexp.MustCompile(`file://([^\s\)\],]+)`)
	boldRe1     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	boldRe2     = regexp.MustCompile(`__([^_]+)__`)
	italicRe1   = regexp.MustCompile(`\*([^*\n]+)\*`)
	italicRe2   = regexp.MustCompile(`_([^_\n]+)_`)
	strikeRe    = regexp.MustCompile(`~~([^~]+)~~`)
	headerRe    = regexp.MustCompile(`(?m)^#{1,4}\s+(.+)$`)
	hrRe        = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
	bulletRe    = regexp.MustCompile(`(?m)^[ \t]*[-*+][ \t]+`)
	quoteRe     = regexp.MustCompile(`(?m)^>[ \t]*(.+)$`)
)

// Result holds the formatted HTML text and any detected server file attachments.
type Result struct {
	HTMLText string
	Files    []string
}

// ConvertMarkdownToTelegramHTML converts Markdown text from Antigravity/LLMs
// to Telegram-safe HTML and extracts local file paths to be sent as attachments.
func ConvertMarkdownToTelegramHTML(md string) Result {
	var placeholders []string
	addPlaceholder := func(content string) string {
		idx := len(placeholders)
		key := fmt.Sprintf("XZFMTBLOCK%dZX", idx)
		placeholders = append(placeholders, content)
		return key
	}

	// 1. Extract fenced code blocks
	text := codeBlockRe.ReplaceAllStringFunc(md, func(match string) string {
		sub := codeBlockRe.FindStringSubmatch(match)
		code := ""
		if len(sub) > 2 {
			code = sub[2]
		}
		escapedCode := html.EscapeString(strings.TrimRight(code, "\r\n"))
		return addPlaceholder(fmt.Sprintf("<pre>%s</pre>", escapedCode))
	})

	// 2. Extract inline code
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := inlineCodeRe.FindStringSubmatch(match)
		c := ""
		if len(sub) > 1 {
			c = sub[1]
		}
		return addPlaceholder(fmt.Sprintf("<code>%s</code>", html.EscapeString(c)))
	})

	// 3. Detect and extract file links
	var detectedFiles []string
	seenFiles := make(map[string]bool)

	addFile := func(p string) {
		clean := filepath.Clean(p)
		if !seenFiles[clean] {
			seenFiles[clean] = true
			detectedFiles = append(detectedFiles, clean)
		}
	}

	text = fileLinkRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := fileLinkRe.FindStringSubmatch(match)
		if len(sub) > 2 {
			label := sub[1]
			path := sub[2]
			addFile(path)
			return fmt.Sprintf("<b>📄 %s</b> (<code>%s</code>)", html.EscapeString(label), html.EscapeString(path))
		}
		return match
	})

	text = rawFileRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := rawFileRe.FindStringSubmatch(match)
		if len(sub) > 1 {
			path := sub[1]
			addFile(path)
			return fmt.Sprintf("<code>%s</code>", html.EscapeString(path))
		}
		return match
	})

	// 4. Web links
	text = mdLinkRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := mdLinkRe.FindStringSubmatch(match)
		if len(sub) > 2 {
			label := html.EscapeString(sub[1])
			url := html.EscapeString(sub[2])
			return fmt.Sprintf("<a href=\"%s\">%s</a>", url, label)
		}
		return match
	})

	// 5. Headers
	text = headerRe.ReplaceAllString(text, "<b>$1</b>")

	// 6. Horizontal Rules
	text = hrRe.ReplaceAllString(text, "──────────────")

	// 7. Bullets
	text = bulletRe.ReplaceAllString(text, "• ")

	// 8. Quotes
	text = quoteRe.ReplaceAllString(text, "<blockquote>$1</blockquote>")

	// 9. Bold
	text = boldRe1.ReplaceAllString(text, "<b>$1</b>")
	text = boldRe2.ReplaceAllString(text, "<b>$1</b>")

	// 10. Italic
	text = italicRe1.ReplaceAllString(text, "<i>$1</i>")
	text = italicRe2.ReplaceAllString(text, "<i>$1</i>")

	// 11. Strikethrough
	text = strikeRe.ReplaceAllString(text, "<s>$1</s>")

	// 12. Restore placeholders
	for i, ph := range placeholders {
		key := fmt.Sprintf("XZFMTBLOCK%dZX", i)
		text = strings.ReplaceAll(text, key, ph)
	}

	return Result{
		HTMLText: strings.TrimSpace(text),
		Files:    detectedFiles,
	}
}

// PrepareFileForSending validates a path, zipping it if it's a directory,
// and returns the path to send along with whether it is a temporary zip.
func PrepareFileForSending(rawPath string) (string, bool, error) {
	cleanPath := filepath.Clean(rawPath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return "", false, fmt.Errorf("файл или папка не найдена: %s", cleanPath)
	}

	if info.IsDir() {
		// Zip the directory
		dirName := filepath.Base(cleanPath)
		zipPath := filepath.Join("/tmp", fmt.Sprintf("%s.zip", dirName))
		_ = os.Remove(zipPath)

		cmd := exec.Command("zip", "-r", "-q", zipPath, ".")
		cmd.Dir = cleanPath
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", false, fmt.Errorf("ошибка создания ZIP архива: %s (%w)", string(out), err)
		}

		zipInfo, err := os.Stat(zipPath)
		if err != nil {
			return "", false, fmt.Errorf("не удалось создать ZIP архив: %w", err)
		}

		const maxZipSize = 45 * 1024 * 1024 // 45MB
		if zipInfo.Size() > maxZipSize {
			_ = os.Remove(zipPath)
			return "", false, fmt.Errorf("архив папки (%.1f MB) превышает лимит отправки Telegram (45 MB)", float64(zipInfo.Size())/(1024*1024))
		}

		return zipPath, true, nil
	}

	const maxFileSize = 45 * 1024 * 1024 // 45MB
	if info.Size() > maxFileSize {
		return "", false, fmt.Errorf("размер файла (%.1f MB) превышает лимит отправки Telegram (45 MB)", float64(info.Size())/(1024*1024))
	}

	return cleanPath, false, nil
}
