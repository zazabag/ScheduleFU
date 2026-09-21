package web

import (
	"net/http"
)

// settings — четвёртый раздел: внешний вид, уведомления, календарь,
// закреплённое расписание. Всё здесь работает формами без JavaScript;
// скрипт только добавляет кнопку уведомлений, которую браузер без него
// не покажет.
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "некорректная форма", http.StatusBadRequest)
			return
		}
		look := lookFrom(r)
		skin, theme := look.Skin.ID, look.Theme
		if v := r.PostForm.Get("skin"); v != "" {
			skin = v
		}
		if v := r.PostForm.Get("theme"); v != "" {
			theme = ParseTheme(v)
		}
		setLookCookies(w, skin, theme, secure)
		if r.PostForm.Get("unpin") == "1" {
			ClearSubjectCookie(w, secure)
		}
		// После сохранения — на чистый адрес, чтобы «назад» не повторяло POST.
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	pinned := SubjectFromCookie(r)
	data := map[string]any{"Title": "Настройки", "Tab": "settings", "Freshness": s.freshness(r),
		"Skins": Skins, "Themes": []struct{ ID, Label string }{
			{"system", "Как в системе"}, {"light", "Светлая"}, {"dark", "Тёмная"},
		}}
	if !pinned.IsZero() {
		label := pinned.Group
		if pinned.LecturerOid != 0 {
			label = s.d.Schedule.LecturerName(r.Context(), pinned.LecturerOid, nil)
		}
		data["Pinned"], data["PinnedLabel"], data["PinnedQuery"], data["PinnedKey"] = true, label, pinned.Query(), pinned.Key()
		data["PinnedIsLecturer"] = pinned.LecturerOid != 0
	}
	data["PushEnabled"] = s.d.Notify != nil && s.d.Notify.Enabled()
	data["Host"] = r.Host
	s.render(w, r, "settings", data)
}
