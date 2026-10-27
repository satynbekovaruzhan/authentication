package templating

import (
	"fmt"
	"html/template"
	"path/filepath"

	"forum-authentication/internal/models"
)

// Load читает все страничные шаблоны вместе с layout-файлом один раз
// и возвращает кэш: имя страницы -> готовый *template.Template.
// templatesDir — каталог с HTML-шаблонами.
func Load(templatesDir string) (map[string]*template.Template, error) {
	const layoutName = "layout.html"

	pages := []string{"index", "login", "register", "post_view", "messages", "error"}

	layout := filepath.Join(templatesDir, layoutName)
	cache := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		ts, err := template.New(layoutName).Funcs(funcs).ParseFiles(layout, filepath.Join(templatesDir, page+".html"))
		if err != nil {
			return nil, fmt.Errorf("templating: parse %q: %w", page, err)
		}
		cache[page] = ts
	}
	return cache, nil
}

// funcs — вспомогательные функции для шаблонов.
var funcs = template.FuncMap{
	"onlineCount": onlineCount,
}

// onlineCount считает количество пользователей, находящихся в сети.
func onlineCount(contacts []models.Contact) int {
	count := 0
	for _, c := range contacts {
		if c.Online {
			count++
		}
	}
	return count
}
