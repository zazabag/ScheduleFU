package web

import (
	"html/template"
	"net/url"
	"sort"

	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Лента предмета — по парам.
//
// Студент вспоминает «что было во вторник», а не «где у меня конспекты»:
// конспект и задание одной пары — одно событие, и разносить их по вкладкам
// значит заставлять угадывать, где лежит нужное. Поэтому предмет — лента
// пар, по которым что-то есть, а пара открывается экраном с конспектом и
// заданиями. Отдельно сверху — «Не сделано»: задания живут сроком, и
// открывать каждую дату, чтобы вспомнить про четверговое, никто не станет.

// dayRow — пара в ленте предмета.
type dayRow struct {
	Key      string // 2026-09-22T10:10; время пусто, если пару не нашли в расписании
	Label    string // вт, 22 сентября · 10:10
	Href     template.URL
	Note     bool // есть сохранённый конспект
	Draft    bool // конспект готов, но не сохранён
	Homework int  // сохранённых заданий пары
	// State — запись этой пары ещё не стала конспектом: обрабатывается или
	// упала. StateClass — её статус для оформления.
	State, StateClass string
}

// dayKeyOf — ключ пары: дата и время начала, как в выборе пары при записи.
func dayKeyOf(l ndom.LessonRef) string { return l.DateKey() + "T" + l.BeginsAt }

// dayLabel — «вт, 22 сентября · 10:10».
func dayLabel(l ndom.LessonRef) string {
	s := clock.WeekdayShortRu(l.Date) + ", " + clock.DateRu(l.Date)
	if l.BeginsAt != "" {
		s += " · " + l.BeginsAt
	}
	return s
}

func dayHref(base string, key string) template.URL {
	return template.URL(base + "&day=" + url.QueryEscape(key))
}

// buildDays склеивает ленту из конспектов, заданий и записей предмета.
// Готовая запись в ленту отдельно не идёт — она уже там конспектом.
func buildDays(saved, drafts []ndom.Note, hws []ndom.Homework, recs []ndom.Recording, base string) []dayRow {
	rows := map[string]*dayRow{}
	get := func(l ndom.LessonRef) *dayRow {
		key := dayKeyOf(l)
		if r, ok := rows[key]; ok {
			return r
		}
		r := &dayRow{Key: key, Label: dayLabel(l), Href: dayHref(base, key)}
		rows[key] = r
		return r
	}
	for _, n := range saved {
		get(n.Lesson).Note = true
	}
	for _, n := range drafts {
		get(n.Lesson).Draft = true
	}
	for _, h := range hws {
		get(h.Lesson).Homework++
	}
	for _, rec := range recs {
		if rec.Status == ndom.StatusReady {
			continue
		}
		r := get(rec.Lesson)
		if r.State == "" {
			r.State, r.StateClass = rec.Status.Label(), string(rec.Status)
		}
	}
	out := make([]dayRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key > out[j].Key })
	return out
}

// pendingHomework — невыполненные задания всех пар, ранние сверху: чем
// раньше задали, тем ближе срок.
func pendingHomework(hws []ndom.Homework, base string) []homeworkView {
	var todo []ndom.Homework
	for _, h := range hws {
		if !h.Done() {
			todo = append(todo, h)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return dayKeyOf(todo[i].Lesson) < dayKeyOf(todo[j].Lesson) })
	out := make([]homeworkView, 0, len(todo))
	for _, h := range todo {
		v := homeworkViewOf(h)
		v.DayHref, v.DayLabel = dayHref(base, dayKeyOf(h.Lesson)), dayLabel(h.Lesson)
		out = append(out, v)
	}
	return out
}
