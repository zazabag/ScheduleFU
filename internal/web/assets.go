package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

// assetVersions хранит отпечаток каждого встроенного файла.
//
// Отпечаток попадает в адрес (style.css?v=1a2b3c), поэтому браузеру можно
// разрешить держать файл в кэше сколь угодно долго: при изменении файла
// меняется адрес, и старый кэш просто перестаёт использоваться. Без этого
// приходилось бы каждый раз спрашивать сервер, не изменилось ли что-то.
type assetVersions struct {
	once     sync.Once
	versions map[string]string
}

var assets assetVersions

func (a *assetVersions) load() {
	a.once.Do(func() {
		a.versions = map[string]string{}
		_ = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			body, err := staticFS.ReadFile(p)
			if err != nil {
				return nil
			}
			sum := sha256.Sum256(body)
			a.versions[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:])[:10]
			return nil
		})
	})
}

// AssetURL возвращает адрес файла с отпечатком содержимого.
func AssetURL(name string) string {
	assets.load()
	if v, ok := assets.versions[name]; ok {
		return "/static/" + name + "?v=" + v
	}
	return "/static/" + name
}

// staticHandler отдаёт встроенные файлы с правильным кэшированием.
func staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			// Адрес содержит отпечаток: содержимое по нему не изменится
			// никогда, и переспрашивать незачем.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Без отпечатка — обычный короткий кэш с проверкой.
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		// Service worker обязан отдаваться свежим: иначе браузер будет
		// месяцами работать по старому сценарию кэширования.
		if path.Base(r.URL.Path) == "sw.js" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// noStore помечает ответ как непригодный для кэширования.
//
// Страницы отвечают на вопрос «что свободно прямо сейчас»: ответ верен
// ровно в момент запроса и через минуту уже неточен. Отдать его из кэша
// значит соврать.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
}
