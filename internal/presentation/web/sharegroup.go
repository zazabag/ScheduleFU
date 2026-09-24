package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"rsc.io/qr"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// ─── ссылка и QR на расписание группы ────────────────────────────────────────

// groupLink — короткий адрес расписания группы: /g/ПИ24-1. Открывает
// расписание и сразу закрепляет его: староста кидает ссылку в чат, и у
// каждого группа выбрана одним касанием, без поиска.
func (s *Server) groupLink(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if !schedule.IsGroupName(name) {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/schedule?"+sched.GroupSubject(name).Query()+"&pin=1", http.StatusFound)
}

// shareLink — абсолютная ссылка на расписание: её кладут в QR и в чат, где
// относительная не работает. Адрес — из настройки stand.origin: за прокси
// и в предпросмотре запрос приходит на 127.0.0.1, и QR с таким адресом у
// студента не откроется. Без настройки — из запроса.
func (s *Server) shareLink(r *http.Request, subj sched.Subject) string {
	origin := strings.TrimRight(s.d.Origin, "/")
	if origin == "" {
		scheme := "http"
		if httpx.IsSecure(r) {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	path := "/schedule?" + subj.Query() + "&pin=1"
	if subj.Kind == sched.SubjectGroup {
		path = "/g/" + url.PathEscape(subj.Group)
	}
	return origin + path
}

// readable — ссылка для глаз: «/g/ПИ24-1», а не «/g/%D0%9F…». В QR и в
// «Отправить» уходит закодированная: её одинаково понимают все мессенджеры.
func readable(link string) string {
	if s, err := url.PathUnescape(link); err == nil {
		return s
	}
	return link
}

// qrSVG рисует QR-код путём из квадратов, с полем в четыре модуля — без
// него камеры телефонов читают код хуже. Уровень коррекции M: код
// показывают с проектора и печатают, небольшие потери он переживает.
func qrSVG(text string) (template.HTML, int, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", 0, err
	}
	const quiet = 4
	var b strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				b.WriteString("M" + strconv.Itoa(x+quiet) + " " + strconv.Itoa(y+quiet) + "h1v1h-1z")
			}
		}
	}
	size := code.Size + 2*quiet
	// Путь собран из чисел, текста источника в нём нет — разметке можно
	// доверять.
	svg := `<svg class="qr-svg" viewBox="0 0 ` + strconv.Itoa(size) + ` ` + strconv.Itoa(size) +
		`" shape-rendering="crispEdges" role="img" aria-label="QR-код ссылки"><rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="` +
		b.String() + `"/></svg>`
	return template.HTML(svg), size, nil
}

// shareGroup — экран «Поделиться»: большой QR для проектора, ссылка для
// чата и QR картинкой для печати.
func (s *Server) shareGroup(w http.ResponseWriter, r *http.Request) {
	subj, err := sched.SubjectFromValues(r.URL.Query())
	if err != nil || subj.IsZero() {
		subj = SubjectFromCookie(r)
	}
	if subj.IsZero() {
		http.Redirect(w, r, "/groups", http.StatusSeeOther)
		return
	}
	link := s.shareLink(r, subj)
	svg, _, err := qrSVG(link)
	if err != nil {
		http.Error(w, "не удалось собрать QR-код", http.StatusInternalServerError)
		return
	}
	label := subj.Group
	if subj.Kind == sched.SubjectLecturer {
		label = s.d.Schedule.LecturerName(r.Context(), subj.LecturerOid, nil)
	}
	s.render(w, r, "sharegroup", map[string]any{
		"Title": "Поделиться — " + label, "Tab": "schedule", "Label": label, "Link": link, "LinkText": readable(link),
		"IsLecturer": subj.Kind == sched.SubjectLecturer, "QR": svg,
		"PNG":  template.URL("/share/qr.png?" + subj.Query()),
		"Back": template.URL("/schedule?" + subj.Query()),
	})
}

// shareQRPNG — тот же QR картинкой: распечатать и повесить в аудитории.
func (s *Server) shareQRPNG(w http.ResponseWriter, r *http.Request) {
	subj, err := sched.SubjectFromValues(r.URL.Query())
	if err != nil || subj.IsZero() {
		http.NotFound(w, r)
		return
	}
	code, err := qr.Encode(s.shareLink(r, subj), qr.M)
	if err != nil {
		http.Error(w, "не удалось собрать QR-код", http.StatusInternalServerError)
		return
	}
	code.Scale = 12
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `inline; filename="schedulefu-qr.png"`)
	_, _ = w.Write(code.PNG())
}
