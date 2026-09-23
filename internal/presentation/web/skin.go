package web

import (
	"net/http"
)

// Skin — оформление. Каркас экранов один на всех: разделы, места кнопок,
// порядок блоков. Оформление меняет только то, как это выглядит — и живёт
// в своём CSS-файле. Так студент выбирает внешний вид, а мы держим одну
// логику и одни шаблоны.
type Skin struct {
	ID   string
	Name string
	Note string
	// Fonts — семейства из static/fonts (tools/fetch_fonts.py). Свои, а не
	// с Google: стили с fonts.googleapis.com держали отрисовку по 4–5 с на
	// мобильной сети (замер с телефона, 23.09.2026).
	Fonts []string
	// Dark — оформление задумано тёмным: при системной теме без JavaScript
	// оно показывается таким, каким нарисовано.
	Dark bool
}

// Skins — восемь оформлений мудборда «итерация 3», в порядке выбора.
var Skins = []Skin{
	{ID: "grid", Name: "Сетка", Note: "Редакционная типографика: крупная дата, таблица пар", Fonts: []string{"inter"}},
	{ID: "night", Name: "Ночной таймер", Note: "Тёмная тема и обратный отсчёт до конца пары", Fonts: []string{"inter"}, Dark: true},
	{ID: "board", Name: "Табло", Note: "Отправления с вокзала: моноширинный шрифт, статусы рейсов", Fonts: []string{"jetbrains-mono", "inter"}, Dark: true},
	{ID: "cover", Name: "Обложка", Note: "День как журнал: синяя обложка и антиква", Fonts: []string{"playfair-display", "inter"}},
	{ID: "player", Name: "Плеер", Note: "Пара как трек: пластинка, полоса, «далее»", Fonts: []string{"inter"}, Dark: true},
	{ID: "plan", Name: "План корпуса", Note: "Куда идти: схема аудиторий и маршрут между парами", Fonts: []string{"inter"}},
	{ID: "stickers", Name: "Стикеры", Note: "Коллаж из наклеек — пары как стикеры на оранжевом", Fonts: []string{"rubik"}},
	{ID: "map", Name: "Карта дня", Note: "Маршрут с остановками: пары как точки на дороге", Fonts: []string{"nunito"}},
}

const defaultSkin = "night"

// SkinByID возвращает оформление; неизвестное — по умолчанию, потому что
// значение приходит из cookie и может быть чем угодно.
func SkinByID(id string) Skin {
	for _, s := range Skins {
		if s.ID == id {
			return s
		}
	}
	return SkinByID(defaultSkin)
}

// Theme — светлая, тёмная или как в системе.
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// ParseTheme принимает только три известных значения.
func ParseTheme(v string) Theme {
	switch Theme(v) {
	case ThemeLight, ThemeDark:
		return Theme(v)
	}
	return ThemeSystem
}

const (
	skinCookie  = "schedulefu_skin"
	themeCookie = "schedulefu_theme"
)

// Look — что выбрал посетитель, в виде, готовом для шаблона.
type Look struct {
	Skin  Skin
	Theme Theme
	// Resolved — тема, которую страница показывает без JavaScript: для
	// системной берётся замысел оформления, скрипт потом уточнит.
	Resolved Theme
}

func lookFrom(r *http.Request) Look {
	l := Look{Skin: SkinByID(cookieValue(r, skinCookie)), Theme: ParseTheme(cookieValue(r, themeCookie))}
	l.Resolved = l.Theme
	if l.Theme == ThemeSystem {
		l.Resolved = ThemeLight
		if l.Skin.Dark {
			l.Resolved = ThemeDark
		}
	}
	return l
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func setLookCookies(w http.ResponseWriter, skin string, theme Theme, secure bool) {
	for name, value := range map[string]string{skinCookie: SkinByID(skin).ID, themeCookie: string(theme)} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/",
			MaxAge: 365 * 24 * 60 * 60, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	}
}
