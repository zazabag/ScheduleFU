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
	// Fonts — параметр family для Google Fonts; пусто — системный шрифт.
	Fonts string
	// Dark — оформление задумано тёмным: при системной теме без JavaScript
	// оно показывается таким, каким нарисовано.
	Dark bool
}

// Skins — восемь оформлений мудборда «итерация 3», в порядке выбора.
var Skins = []Skin{
	{ID: "grid", Name: "Сетка", Note: "Редакционная типографика: крупная дата, таблица пар", Fonts: "Inter:wght@400;500;600;700;900"},
	{ID: "night", Name: "Ночной таймер", Note: "Тёмная тема и обратный отсчёт до конца пары", Fonts: "Inter:wght@400;500;600;700", Dark: true},
	{ID: "board", Name: "Табло", Note: "Отправления с вокзала: моноширинный шрифт, статусы рейсов", Fonts: "JetBrains+Mono:wght@400;500;700&family=Inter:wght@400;600;800", Dark: true},
	{ID: "cover", Name: "Обложка", Note: "День как журнал: синяя обложка и антиква", Fonts: "Playfair+Display:ital,wght@0,400;0,700;0,900;1,400;1,700&family=Inter:wght@400;500;600"},
	{ID: "player", Name: "Плеер", Note: "Пара как трек: пластинка, полоса, «далее»", Fonts: "Inter:wght@400;500;600;700", Dark: true},
	{ID: "plan", Name: "План корпуса", Note: "Куда идти: схема аудиторий и маршрут между парами", Fonts: "Inter:wght@400;500;600;700"},
	{ID: "stickers", Name: "Стикеры", Note: "Коллаж из наклеек — пары как стикеры на оранжевом", Fonts: "Rubik:wght@400;500;700;900"},
	{ID: "map", Name: "Карта дня", Note: "Маршрут с остановками: пары как точки на дороге", Fonts: "Nunito:wght@400;600;700;800"},
}

const defaultSkin = "grid"

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
