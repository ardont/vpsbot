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

func TestAdvancedMarkdownFormatting(t *testing.T) {
	input := `### Заголовок
Ссылка с кириллицей: [Яндекс](https://market.yandex.ru/search?text=ноутбук+Acer).
Ссылка с подчеркиваниями: [Подчеркивания](https://example.com/test_url_with_underscores).
Сравнение: 5 < 10 и Q&A сессия.
*Курсив* и **Жирный** и ` + "`код`" + `.
`
	res := ConvertMarkdownToTelegramHTML(input)

	// Check that cyrillic URL is percent-encoded
	if !strings.Contains(res.HTMLText, "%D0%BD%D0%BE%D1%83%D1%82%D0%B1%D1%83%D0%BA") {
		t.Errorf("Expected percent-encoded URL, got: %s", res.HTMLText)
	}
	// Check that underscore URL did not get <i> inside href
	if strings.Contains(res.HTMLText, "<i>") && strings.Contains(res.HTMLText, "<a href=") {
		if strings.Contains(res.HTMLText, "<a href=\"https://example.com/test<i>") {
			t.Errorf("URL was mangled by italic regex: %s", res.HTMLText)
		}
	}
	// Check that & got escaped
	if !strings.Contains(res.HTMLText, "Q&amp;A") {
		t.Errorf("Expected Q&amp;A, got: %s", res.HTMLText)
	}
	// Check that < got escaped
	if !strings.Contains(res.HTMLText, "5 &lt; 10") {
		t.Errorf("Expected 5 &lt; 10, got: %s", res.HTMLText)
	}
}
