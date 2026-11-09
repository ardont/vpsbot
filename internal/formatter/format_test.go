package formatter

import (
	"strings"
	"testing"
)

func TestConvertMarkdownToTelegramHTML(t *testing.T) {
	input := `### 1. Тестовый заголовок

Вот **жирный** текст и *курсив*, а также ` + "`код`" + `.
Файл с информацией: [os-release](file:///etc/os-release).
Еще ссылка: [Google](https://google.com).

` + "```bash\necho \"Hello world\"\n```" + `

- Пункт 1
- Пункт 2
`

	res := ConvertMarkdownToTelegramHTML(input)

	if !strings.Contains(res.HTMLText, "<b>1. Тестовый заголовок</b>") {
		t.Errorf("Expected bold header, got: %s", res.HTMLText)
	}
	if !strings.Contains(res.HTMLText, "<b>жирный</b>") {
		t.Errorf("Expected bold text, got: %s", res.HTMLText)
	}
	if !strings.Contains(res.HTMLText, "<code>код</code>") {
		t.Errorf("Expected inline code, got: %s", res.HTMLText)
	}
	if !strings.Contains(res.HTMLText, "<pre>echo &#34;Hello world&#34;</pre>") {
		t.Errorf("Expected preformatted code, got: %s", res.HTMLText)
	}
	if len(res.Files) != 1 || res.Files[0] != "/etc/os-release" {
		t.Errorf("Expected detected file /etc/os-release, got: %v", res.Files)
	}
}

func TestPrepareFileForSending(t *testing.T) {
	path, isZip, err := PrepareFileForSending("/etc/os-release")
	if err != nil {
		t.Fatalf("Unexpected error for /etc/os-release: %v", err)
	}
	if isZip {
		t.Errorf("Expected isZip=false for normal file")
	}
	if path != "/etc/os-release" {
		t.Errorf("Expected path=/etc/os-release, got %s", path)
	}
}
