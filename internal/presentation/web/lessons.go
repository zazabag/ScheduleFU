package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// Раздел «Пары»: предметы закреплённой группы, записи занятий, конспекты и
// домашние задания.
//
// Предмет — единица списка: студент помнит, что ищет конспект по истории.
// Внутри предмета — лента пар (days.go): конспект и задания одной пары
// открываются вместе. Список предметов
// склеивается из двух источников: расписание даёт те, что идут сейчас,
// наши таблицы — те, по которым уже есть конспекты. Второе обязательно:
// окно сбора расписания — неделя, а конспект нужен к сессии.

// subjectCard — предмет в списке.
type subjectCard struct {
	Name     string
	Href     template.URL
	Lecturer string
	Meta     string
	Notes    int
	Homework int
	Soon     string // «сегодня в 10:10», «ср, 24 сентября»
}

// lessonOption — пара предмета, к которой можно привязать запись.
type lessonOption struct {
	Value string // 2026-09-22T10:10
	Label string
	On    bool
}

// noteView — конспект для показа.
type noteView struct {
	ID        int64
	Date      string
	Time      string
	Title     string
	Blocks    []noteBlock
	Theses    []string
	Saved     bool
	Homeworks []homeworkView
	// AskHW — модель задания не нашла и руками его к паре не вписывали:
	// при сохранении спрашиваем «что задали?».
	AskHW bool
	// Куда вернуться после «Сохранить» и «Удалить»; куда после сохранения
	// задания из конспекта.
	SaveBack, DeleteBack, HwBack template.URL
	// ShareHref — ссылка, по которой конспект открывает кто угодно; пусто —
	// им не делились.
	ShareHref string
	// Карточки и словарь по конспекту: можно ли просить, что с ними сейчас
	// и где они лежат.
	CanCards                  bool
	CardsStatus, CardsFailure string
	CardsHref                 template.URL
}

// noteBlock — кусок конспекта. Разметку разбирает сервер, а не браузер:
// текст пришёл от модели, и пускать его в шаблон как HTML нельзя.
type noteBlock struct {
	Kind  string // head | text | list
	Text  string
	Items []string
}

// homeworkView — задание для показа.
type homeworkView struct {
	ID     int64
	Body   string
	Due    string
	Date   string
	Done   bool
	Saved  bool
	Manual bool
	// Пара, к которой задание: в «Не сделано» оно ведёт к ней.
	DayHref  template.URL
	DayLabel string
}

// recordingView — запись и её состояние.
type recordingView struct {
	ID       int64
	Date     string
	Status   string
	Label    string
	Failure  string
	Duration string
	Working  bool
	Ready    bool
	Href     template.URL
	// Gaps — где айфон выключал микрофон: «на 23-й минуте — около 4 мин».
	Gaps []string
}

func (s *Server) lessons(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		http.NotFound(w, r)
		return
	}
	subj := SubjectFromCookie(r)
	if q, err := sched.SubjectFromValues(r.URL.Query()); err == nil && !q.IsZero() {
		subj = q
	}
	data := map[string]any{"Title": "Пары", "Tab": "lessons"}
	if subj.IsZero() {
		// Закреплять нечего — тот же выбор группы, что и на расписании:
		// раздел без владельца расписания смысла не имеет.
		s.pickerData(r, data)
		data["Picker"] = true
		s.render(w, r, "lessons", data)
		return
	}
	secure := httpx.IsSecure(r)
	owner := httpx.OwnerKey(w, r, secure)
	if owner == "" {
		http.Error(w, "не удалось завести ключ устройства", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	label := subj.Group
	if subj.Kind == sched.SubjectLecturer {
		label = s.d.Schedule.LecturerName(ctx, subj.LecturerOid, nil)
	}
	data["Group"], data["SubjectQuery"] = label, template.URL(subj.Query())
	data["CanRecord"] = s.d.NotesReady
	data["CanCards"] = s.d.Notes.CanCards()

	discipline := strings.TrimSpace(r.URL.Query().Get("d"))
	if discipline == "" {
		if err := s.subjectList(ctx, owner, subj, data); err != nil {
			http.Error(w, "не удалось собрать список предметов", http.StatusInternalServerError)
			return
		}
		s.render(w, r, "lessons", data)
		return
	}
	if err := s.subjectCard(ctx, owner, subj, discipline, data); err != nil {
		http.Error(w, "не удалось открыть предмет", http.StatusInternalServerError)
		return
	}
	if v := r.URL.Query().Get("rec"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "неверный номер записи", http.StatusBadRequest)
			return
		}
		if err := s.recordingScreen(ctx, owner, id, data); err != nil {
			http.Error(w, "не удалось открыть запись", http.StatusInternalServerError)
			return
		}
	} else if key := r.URL.Query().Get("day"); key != "" {
		if err := s.dayScreen(ctx, owner, subj, discipline, key, data); err != nil {
			http.Error(w, "не удалось открыть пару", http.StatusInternalServerError)
			return
		}
	}
	s.render(w, r, "lessons", data)
}

// subjectList собирает список предметов.
func (s *Server) subjectList(ctx context.Context, owner string, subj sched.Subject, data map[string]any) error {
	week, err := s.weekLessons(ctx, subj)
	if err != nil {
		return err
	}
	mine, err := s.d.Notes.Disciplines(ctx, owner, subj.Key())
	if err != nil {
		return err
	}

	today := s.d.Clock.Today()
	now := s.d.Clock.HHMM()
	byName := map[string]*subjectCard{}
	var order []string
	get := func(name string) *subjectCard {
		if c, ok := byName[name]; ok {
			return c
		}
		c := &subjectCard{Name: name,
			Href: template.URL("/lessons?" + subj.Query() + "&d=" + url.QueryEscape(name))}
		byName[name] = c
		order = append(order, name)
		return c
	}

	lecturers := map[string]map[string]bool{}
	kinds := map[string]map[string]bool{}
	for _, l := range week {
		if strings.TrimSpace(l.Discipline) == "" {
			continue
		}
		c := get(l.Discipline)
		if l.LecturerName != "" {
			if lecturers[l.Discipline] == nil {
				lecturers[l.Discipline] = map[string]bool{}
			}
			lecturers[l.Discipline][l.LecturerName] = true
		}
		if k := shortKind(l.KindOfWork); k != "" {
			if kinds[l.Discipline] == nil {
				kinds[l.Discipline] = map[string]bool{}
			}
			kinds[l.Discipline][k] = true
		}
		// Ближайшая пара: сегодняшняя, если ещё не кончилась, иначе первая
		// из будущих. Прошедшие в подпись не идут — они там как «было».
		if c.Soon == "" && !before(l, today, now) {
			c.Soon = whenLabel(l, today)
		}
	}
	for _, d := range mine {
		c := get(d.Name)
		c.Notes, c.Homework = d.Notes, d.Homework
	}
	for name, c := range byName {
		c.Lecturer = joinSet(lecturers[name], 2)
		c.Meta = joinSet(kinds[name], 3)
	}

	// Порядок: сначала то, что идёт скоро, потом остальное по алфавиту.
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byName[order[i]], byName[order[j]]
		if (a.Soon != "") != (b.Soon != "") {
			return a.Soon != ""
		}
		return naturalLess(a.Name, b.Name)
	})
	cards := make([]*subjectCard, 0, len(order))
	for _, n := range order {
		cards = append(cards, byName[n])
	}
	data["Subjects"] = cards
	return nil
}

// subjectCard готовит экран одного предмета.
func (s *Server) subjectCard(ctx context.Context, owner string, subj sched.Subject, discipline string, data map[string]any) error {
	week, err := s.weekLessons(ctx, subj)
	if err != nil {
		return err
	}
	today := s.d.Clock.Today()
	now := s.d.Clock.HHMM()

	lecturers, kinds, rooms := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var options []lessonOption
	var soon string
	for _, l := range week {
		if l.Discipline != discipline {
			continue
		}
		if l.LecturerName != "" {
			lecturers[l.LecturerName] = true
		}
		if k := shortKind(l.KindOfWork); k != "" {
			kinds[k] = true
		}
		if l.Auditorium != "" {
			rooms[roomShort(l.Auditorium)] = true
		}
		options = append(options, lessonOption{
			Value: l.DateKey() + "T" + l.BeginsAt,
			Label: clock.WeekdayShortRu(l.Date) + ", " + clock.DateRu(l.Date) + ", " + l.BeginsAt,
		})
		if soon == "" && !before(l, today, now) {
			soon = whenLabel(l, today)
		}
	}
	// По умолчанию выбрана идущая или ближайшая пара: чаще всего записывают
	// ту, которая идёт прямо сейчас.
	def := defaultOption(options, today.Format("2006-01-02"), now)
	for i := range options {
		options[i].On = options[i].Value == def
	}

	notesList, err := s.d.Notes.Notes(ctx, owner, subj.Key(), discipline)
	if err != nil {
		return err
	}
	homeworks, err := s.d.Notes.Homeworks(ctx, owner, subj.Key(), discipline, true)
	if err != nil {
		return err
	}
	recs, err := s.d.Notes.Recordings(ctx, owner, subj.Key(), discipline)
	if err != nil {
		return err
	}
	drafts, err := s.d.Notes.DraftNotes(ctx, owner, subj.Key(), discipline)
	if err != nil {
		return err
	}

	base := "/lessons?" + subj.Query() + "&d=" + url.QueryEscape(discipline)

	data["Title"] = discipline
	data["One"] = map[string]any{
		"Name":     discipline,
		"Lecturer": joinSet(lecturers, 3),
		"Kinds":    joinSet(kinds, 3),
		"Rooms":    joinSet(rooms, 3),
		"Soon":     soon,
		"Options":  options,
		// Черновик — тоже строка ленты, с пометкой «не сохранён»: уйти со
		// страницы записи не значит потерять конспект.
		"Days":    buildDays(notesList, drafts, homeworks, recs, base),
		"Pending": pendingHomework(homeworks, base),
		"Base":    template.URL(base),
		"Today":   today.Format("2006-01-02"),
	}
	data["Discipline"] = discipline
	return nil
}

// recordingScreen добавляет экран одной записи: ход обработки, а когда
// готово — конспект с заданиями, которые ещё не сохранены.
func (s *Server) recordingScreen(ctx context.Context, owner string, id int64, data map[string]any) error {
	rec, ok, err := s.d.Notes.Recording(ctx, owner, id)
	if err != nil || !ok {
		return err
	}
	view := recordingView{ID: rec.ID, Date: clock.DateRu(rec.Lesson.Date), Status: string(rec.Status),
		Label: rec.Status.Label(), Failure: rec.Failure, Duration: rec.Duration(),
		Working: rec.Status.Working(), Ready: rec.Status == ndom.StatusReady}
	for _, g := range rec.Gaps {
		view.Gaps = append(view.Gaps, g.Label())
	}
	data["Rec"] = view

	if note, ok, err := s.d.Notes.NoteByRecording(ctx, owner, rec.ID); err != nil {
		return err
	} else if ok {
		hws, err := s.d.Notes.HomeworksByNote(ctx, owner, note.ID)
		if err != nil {
			return err
		}
		base := string(data["One"].(map[string]any)["Base"].(template.URL))
		v := s.noteView(note, hws, base)
		v.HwBack = template.URL(base + "&rec=" + strconv.FormatInt(rec.ID, 10))
		data["RecNote"] = v
	}
	return nil
}

// dayView — экран одной пары.
type dayView struct {
	Key        string
	Label      string
	Notes      []noteView
	Homeworks  []homeworkView
	Recordings []recordingView
	Back       template.URL
}

// dayScreen собирает экран пары: конспекты (сохранённые и черновики),
// задания и записи, которые ещё не стали конспектом.
func (s *Server) dayScreen(ctx context.Context, owner string, subj sched.Subject, discipline, key string, data map[string]any) error {
	base := string(data["One"].(map[string]any)["Base"].(template.URL))
	saved, err := s.d.Notes.Notes(ctx, owner, subj.Key(), discipline)
	if err != nil {
		return err
	}
	drafts, err := s.d.Notes.DraftNotes(ctx, owner, subj.Key(), discipline)
	if err != nil {
		return err
	}
	homeworks, err := s.d.Notes.Homeworks(ctx, owner, subj.Key(), discipline, true)
	if err != nil {
		return err
	}
	recs, err := s.d.Notes.Recordings(ctx, owner, subj.Key(), discipline)
	if err != nil {
		return err
	}
	here := dayHref(base, key)
	view := dayView{Key: key, Back: here}

	var dayHws []ndom.Homework
	for _, h := range homeworks {
		if dayKeyOf(h.Lesson) == key {
			dayHws = append(dayHws, h)
			view.Homeworks = append(view.Homeworks, homeworkViewOf(h))
		}
	}
	for _, n := range append(saved, drafts...) {
		if dayKeyOf(n.Lesson) != key {
			continue
		}
		view.Label = dayLabel(n.Lesson)
		var fromNote []ndom.Homework
		if !n.Saved() {
			// У черновика задания тоже черновики — их показываем с кнопкой.
			if fromNote, err = s.d.Notes.HomeworksByNote(ctx, owner, n.ID); err != nil {
				return err
			}
		}
		v := s.noteView(n, fromNote, base)
		// Спрашивать про задание, если его уже вписали к паре, — лишнее.
		v.AskHW = v.AskHW && len(dayHws) == 0
		v.SaveBack, v.HwBack = here, here
		view.Notes = append(view.Notes, v)
	}
	for _, rec := range recs {
		if dayKeyOf(rec.Lesson) != key || rec.Status == ndom.StatusReady {
			continue
		}
		view.Label = dayLabel(rec.Lesson)
		view.Recordings = append(view.Recordings, recordingView{
			ID: rec.ID, Date: clock.DateRu(rec.Lesson.Date), Status: string(rec.Status),
			Label: rec.Status.Label(), Failure: rec.Failure, Duration: rec.Duration(),
			Working: rec.Status.Working(),
			Href:    template.URL(base + "&rec=" + strconv.FormatInt(rec.ID, 10)),
		})
	}
	if view.Label == "" && len(dayHws) > 0 {
		view.Label = dayLabel(dayHws[0].Lesson)
	}
	if view.Label == "" {
		// По паре ничего нет — например, всё удалили. Подпись из ключа.
		view.Label = labelFromKey(key, s.d.Clock.Location())
	}
	data["Day"] = view
	return nil
}

// labelFromKey — подпись пары по ключу, когда нет ни одной записи о ней.
func labelFromKey(key string, loc *time.Location) string {
	day, begins, _ := strings.Cut(key, "T")
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return key
	}
	return dayLabel(ndom.LessonRef{Date: d, BeginsAt: begins})
}

// noteView готовит конспект к показу. base — адрес предмета: после
// сохранения человек попадает на экран пары, после удаления — в предмет.
func (s *Server) noteView(n ndom.Note, hws []ndom.Homework, base string) noteView {
	v := noteView{ID: n.ID, Date: clock.DateRu(n.Lesson.Date), Time: n.Lesson.BeginsAt,
		Title: n.Title, Blocks: parseNoteBody(n.Body), Theses: n.Theses, Saved: n.Saved(),
		AskHW:      !n.Saved() && len(hws) == 0,
		SaveBack:   dayHref(base, dayKeyOf(n.Lesson)),
		DeleteBack: template.URL(base)}
	if n.ShareToken != "" {
		v.ShareHref = "/n/" + n.ShareToken
	}
	if s.d.Notes != nil && s.d.Notes.CanCards() {
		v.CanCards, v.CardsStatus, v.CardsFailure = true, n.CardsStatus, n.CardsFailure
		v.CardsHref = template.URL(strings.Replace(base, "/lessons?", "/lessons/cards?", 1))
	}
	for _, h := range hws {
		v.Homeworks = append(v.Homeworks, homeworkViewOf(h))
	}
	return v
}

func homeworkViewOf(h ndom.Homework) homeworkView {
	return homeworkView{ID: h.ID, Body: h.Body, Due: h.Due(), Date: clock.DateRu(h.Lesson.Date),
		Done: h.Done(), Saved: h.Saved(), Manual: h.Origin == "manual"}
}

// parseNoteBody разбирает бедную разметку конспекта в блоки.
//
// Полноценный markdown в шаблон не пускаем: текст пришёл от модели, то есть
// снаружи, и превращать его в HTML — то же самое, что доверять источнику.
// Здесь три вида блоков, и любой из них шаблон печатает как текст.
func parseNoteBody(body string) []noteBlock {
	var out []noteBlock
	var para []string
	var list []string
	flushPara := func() {
		if len(para) > 0 {
			out = append(out, noteBlock{Kind: "text", Text: strings.Join(para, " ")})
			para = nil
		}
	}
	flushList := func() {
		if len(list) > 0 {
			out = append(out, noteBlock{Kind: "list", Items: list})
			list = nil
		}
	}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			flushList()
			flushPara()
		case strings.HasPrefix(line, "## "), strings.HasPrefix(line, "# "):
			flushList()
			flushPara()
			out = append(out, noteBlock{Kind: "head", Text: strings.TrimLeft(line, "# ")})
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "), strings.HasPrefix(line, "— "):
			flushPara()
			list = append(list, strings.TrimSpace(line[strings.IndexByte(line, ' '):]))
		default:
			flushList()
			para = append(para, line)
		}
	}
	flushList()
	flushPara()
	return out
}

// ─── действия ────────────────────────────────────────────────────────────────

// lessonsAction — правки из форм раздела. Отдельная ручка на POST и
// перенаправление обратно: иначе «назад» в браузере повторяет действие, и
// сохранённое задание сохраняется второй раз.
func (s *Server) lessonsAction(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "не разобрать форму", http.StatusBadRequest)
		return
	}
	owner := httpx.ExistingOwnerKey(r)
	if owner == "" {
		http.Error(w, "нет ключа устройства", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	var err error
	switch r.FormValue("action") {
	case "note-save":
		// «Что задали?» при сохранении: пустое поле — «не задавали».
		err = s.d.Notes.SaveNote(ctx, owner, id, r.FormValue("hw"))
	case "note-delete":
		err = s.d.Notes.DeleteNote(ctx, owner, id)
	case "note-cards":
		err = s.d.Notes.RequestCards(ctx, owner, id)
	case "card-review":
		err = s.d.Notes.Review(ctx, owner, id, r.FormValue("remembered") == "1")
	case "note-share":
		_, err = s.d.Notes.Share(ctx, owner, id)
	case "note-unshare":
		err = s.d.Notes.Unshare(ctx, owner, id)
	case "hw-save":
		err = s.d.Notes.SaveHomework(ctx, owner, id)
	case "hw-done":
		err = s.d.Notes.ToggleHomework(ctx, owner, id, true)
	case "hw-undone":
		err = s.d.Notes.ToggleHomework(ctx, owner, id, false)
	case "hw-delete":
		err = s.d.Notes.DeleteHomework(ctx, owner, id)
	case "hw-add":
		err = s.addHomework(w, r, owner)
	case "rec-delete":
		err = s.d.Notes.DeleteRecording(ctx, owner, id)
	default:
		http.Error(w, "неизвестное действие", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/lessons") {
		// Возврат берётся из формы и потому недоверенный: чужой адрес
		// превратил бы нашу страницу в открытый редирект.
		back = "/lessons"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) addHomework(w http.ResponseWriter, r *http.Request, owner string) error {
	subj, err := sched.SubjectFromValues(r.Form)
	if err != nil {
		return err
	}
	if subj.IsZero() {
		subj = SubjectFromCookie(r)
	}
	lesson, err := s.ResolveLesson(r.Context(), subj, r.FormValue("d"), r.FormValue("lesson"))
	if err != nil {
		return err
	}
	return s.d.Notes.AddHomework(r.Context(), owner, lesson, r.FormValue("body"))
}

// upload принимает готовый файл обычной формой.
//
// Это не запасной путь, а основной для айфона: PWA на iOS засыпает вместе с
// экраном, и полуторачасовую запись там делают диктофоном, а сюда приносят
// файл (docs/07-notes-module.md § «Фон»). Форма работает и без скрипта.
func (s *Server) lessonsUpload(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil || !s.d.NotesReady {
		http.Error(w, "обработка записей не настроена", http.StatusServiceUnavailable)
		return
	}
	owner := httpx.OwnerKey(w, r, httpx.IsSecure(r))
	if owner == "" {
		http.Error(w, "нет ключа устройства", http.StatusForbidden)
		return
	}
	// Тело читается потоком: файл на полтора часа — это десятки мегабайт, и
	// класть его целиком в память незачем.
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "ожидается форма с файлом", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	fields := map[string]string{}
	var rec ndom.Recording
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FormName() != "file" {
			var b strings.Builder
			// Поля формы короткие; потолок — от подделанного запроса.
			_, _ = copyLimited(&b, part, 4<<10)
			fields[part.FormName()] = strings.TrimSpace(b.String())
			continue
		}
		if part.FileName() == "" {
			continue
		}
		subj, _ := sched.SubjectFromValues(url.Values{"group": {fields["group"]}, "lecturer": {fields["lecturer"]}})
		if subj.IsZero() {
			subj = SubjectFromCookie(r)
		}
		lesson, lerr := s.ResolveLesson(ctx, subj, fields["d"], fields["lesson"])
		if lerr != nil {
			http.Error(w, lerr.Error(), http.StatusBadRequest)
			return
		}
		rec, err = s.d.Notes.Start(ctx, owner, lesson, ndom.OriginUpload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err = s.d.Notes.Append(ctx, owner, rec.ID, 0, part); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err = s.d.Notes.Finish(ctx, owner, rec.ID, nil); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if rec.ID == 0 {
		http.Error(w, "файл не пришёл", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/lessons?%s&d=%s&rec=%d",
		queryOf(fields, r), url.QueryEscape(fields["d"]), rec.ID), http.StatusSeeOther)
}

func queryOf(fields map[string]string, r *http.Request) string {
	if g := fields["group"]; g != "" {
		return "group=" + url.QueryEscape(g)
	}
	if l := fields["lecturer"]; l != "" {
		return "lecturer=" + url.QueryEscape(l)
	}
	return SubjectFromCookie(r).Query()
}

// ResolveLesson восстанавливает слепок пары по предмету и выбранному
// времени.
//
// Поля пары — преподаватель, аудитория, вид занятия — берутся из
// расписания, а не из формы: браузер прислал бы что угодно, а конспект с
// чужим преподавателем хуже, чем без него. Не нашли пару — остаётся дата,
// этого достаточно.
func (s *Server) ResolveLesson(ctx context.Context, subj sched.Subject, discipline, choice string) (ndom.LessonRef, error) {
	discipline = strings.TrimSpace(discipline)
	if discipline == "" {
		return ndom.LessonRef{}, ndom.ErrNoDiscipline
	}
	dateText, begins, _ := strings.Cut(choice, "T")
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateText), s.d.Clock.Location())
	if err != nil {
		date = s.d.Clock.Today()
	}
	ref := ndom.LessonRef{SubjectKey: subj.Key(), Discipline: discipline, Date: date, BeginsAt: begins}
	week, err := s.weekLessons(ctx, subj)
	if err != nil {
		return ref, nil // расписание недоступно — запись всё равно примем
	}
	for _, l := range week {
		if l.Discipline != discipline || l.DateKey() != date.Format("2006-01-02") {
			continue
		}
		if begins != "" && l.BeginsAt != begins {
			continue
		}
		oid := l.LessonOid
		ref.BeginsAt, ref.EndsAt = l.BeginsAt, l.EndsAt
		ref.LecturerName, ref.Auditorium, ref.KindOfWork = l.LecturerName, l.Auditorium, l.KindOfWork
		ref.LessonOid = &oid
		break
	}
	return ref, nil
}

// weekLessons — пары владельца в окне сбора.
//
// Окно шире недели вперёд и захватывает прошедшую: предмет, пары которого
// на этой неделе уже прошли, из списка пропадать не должен.
func (s *Server) weekLessons(ctx context.Context, subj sched.Subject) ([]sched.Lesson, error) {
	today := s.d.Clock.Today()
	return s.d.Schedule.ScheduleFor(ctx, subj, today.AddDate(0, 0, -7), today.AddDate(0, 0, 13))
}

// ─── мелочи ──────────────────────────────────────────────────────────────────

func before(l sched.Lesson, today time.Time, now string) bool {
	key, todayKey := l.DateKey(), today.Format("2006-01-02")
	return key < todayKey || (key == todayKey && l.EndsAt <= now)
}

func whenLabel(l sched.Lesson, today time.Time) string {
	switch days := int(l.Date.Sub(today).Hours() / 24); days {
	case 0:
		return "сегодня в " + l.BeginsAt
	case 1:
		return "завтра в " + l.BeginsAt
	default:
		return clock.WeekdayShortRu(l.Date) + ", " + clock.DateRu(l.Date)
	}
}

// defaultOption выбирает пару, предложенную по умолчанию: идущую сейчас,
// иначе ближайшую будущую, иначе последнюю прошедшую.
func defaultOption(options []lessonOption, todayKey, now string) string {
	if len(options) == 0 {
		return ""
	}
	for _, o := range options {
		day, begins, _ := strings.Cut(o.Value, "T")
		if day > todayKey || (day == todayKey && begins >= now) {
			return o.Value
		}
		if day == todayKey {
			// Сегодняшняя, уже начавшаяся: скорее всего её и записывают.
			return o.Value
		}
	}
	return options[len(options)-1].Value
}

func joinSet(set map[string]bool, max int) string {
	if len(set) == 0 {
		return ""
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > max {
		return strings.Join(out[:max], " · ") + " и ещё " + strconv.Itoa(len(out)-max)
	}
	return strings.Join(out, " · ")
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// copyLimited читает не больше max байт: поля формы короткие, а запрос
// недоверенный.
func copyLimited(dst *strings.Builder, src interface{ Read([]byte) (int, error) }, max int64) (int64, error) {
	buf := make([]byte, 1024)
	var total int64
	for total < max {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
			total += int64(n)
		}
		if err != nil {
			return total, nil
		}
	}
	return total, nil
}
