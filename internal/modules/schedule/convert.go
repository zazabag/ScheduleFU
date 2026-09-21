package schedule

import (
	"regexp"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/modules/source"
)

// fromSource переводит пару источника в доменную.
//
// Отвергается то, что нельзя ни разместить, ни сопоставить: без даты,
// без идентификатора, без времени, с пометкой удаления — источник иногда
// отдаёт помеченные удалёнными записи.
func fromSource(l source.Lesson, loc *time.Location) (domain.Lesson, bool) {
	if l.LessonOid == 0 || l.DeletionMark != 0 {
		return domain.Lesson{}, false
	}
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(l.Date), loc)
	if err != nil {
		return domain.Lesson{}, false
	}
	out := domain.Lesson{
		LessonOid:    l.LessonOid,
		Date:         date,
		BeginsAt:     hhmm(l.BeginLesson),
		EndsAt:       hhmm(l.EndLesson),
		Auditorium:   strings.TrimSpace(l.Auditorium),
		Building:     strings.TrimSpace(l.Building),
		Discipline:   strings.TrimSpace(l.Discipline),
		KindOfWork:   strings.TrimSpace(l.KindOfWork),
		LecturerName: strings.TrimSpace(l.Lecturer),
		Stream:       strings.TrimSpace(l.Stream),
		GroupNames:   ParseStream(l.Stream, l.Group),
		Subgroup:     subgroupOf(l.SubGroup, l.Stream, l.Group),
		Note:         strings.TrimSpace(l.Note),
	}
	if out.BeginsAt == "" || out.EndsAt == "" {
		return domain.Lesson{}, false
	}
	if l.AuditoriumOid > 0 {
		v := l.AuditoriumOid
		out.AuditoriumOid = &v
	}
	if l.LecturerOid > 0 {
		v := l.LecturerOid
		out.LecturerOid = &v
	}
	if ts, ok := parseSourceTime(l.ModifiedDate, loc); ok {
		out.SourceModifiedAt = &ts
	}
	return out, true
}

// ParseStream разбирает поток «ПИ24-1; ПИ24-2; ПИ24-3» в список групп.
//
// Поле group у пары почти всегда пустое, состав лежит в stream. По этому
// списку адресуются уведомления, поэтому дубли недопустимы: они дали бы
// письмо дважды.
//
// Языковые подгруппы источник оформляет отдельными «группами» вида
// «006073_2 Иностранный язык (КАЯиПК)-10 СОЦ25-6_7»: базовые группы
// зашиты в хвост имени, «СОЦ25-6_7» — это СОЦ25-6 и СОЦ25-7 вместе. Без
// разбора хвоста студент СОЦ25-6 не видел бы свой английский вовсе —
// проверено по расписанию группы у самого вуза. Имя потока остаётся в
// списке тоже: по нему пара находится, если кто-то закрепил поток.
func ParseStream(stream, group string) []string {
	raw := strings.TrimSpace(stream)
	if raw == "" {
		raw = strings.TrimSpace(group)
	}
	if raw == "" {
		return []string{}
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' || r == '\n' }) {
		name := strings.TrimSpace(p)
		add(name)
		for _, base := range baseGroupsOf(name) {
			add(base)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// groupToken — «СОЦ25-6», «Ю24-5в», «СОЦ25-6_7» (несколько групп одним
// хвостом). Скобки вокруг — «(РКИ26-15)» — снимаются заранее.
var groupToken = regexp.MustCompile(`^(\p{L}+\d{2}-)(\d+\p{L}*)((?:_\d+\p{L}*)*)$`)

// baseGroupsOf достаёт базовые группы из имени языкового потока. Обычное
// имя группы возвращает пустой список: оно уже в списке само.
func baseGroupsOf(name string) []string {
	if !strings.Contains(name, " ") {
		return nil
	}
	var out []string
	for _, word := range strings.Fields(name) {
		word = strings.Trim(word, "()")
		m := groupToken.FindStringSubmatch(word)
		if m == nil {
			continue
		}
		out = append(out, m[1]+m[2])
		for _, extra := range strings.Split(m[3], "_") {
			if extra != "" {
				out = append(out, m[1]+extra)
			}
		}
	}
	return out
}

// subgroupCode — «(КАЯиПК)-10» → «10»: номер подгруппы из имени потока,
// когда поле subGroup пустое. Так стопка подгрупп в одном слоте получает
// подписи, по которым студент находит свою.
var subgroupCode = regexp.MustCompile(`\)-(\d+)\b`)

func subgroupOf(explicit, stream, group string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	name := strings.TrimSpace(stream)
	if name == "" {
		name = strings.TrimSpace(group)
	}
	if m := subgroupCode.FindStringSubmatch(name); m != nil {
		return "подгруппа " + m[1]
	}
	return ""
}

func hhmm(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 5 && s[2] == ':' {
		return s[:5]
	}
	return ""
}

// parseSourceTime разбирает метку вида «2026-09-09T10:51:08Z00:00» — это не
// RFC3339 (у того либо Z, либо смещение, не оба). Разбираем без зоны и
// считаем временем вуза.
func parseSourceTime(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if i := strings.IndexAny(s, "Z+"); i > 0 {
		s = s[:i]
	}
	ts, err := time.ParseInLocation("2006-01-02T15:04:05", s, loc)
	return ts, err == nil
}

// auditoriumFrom собирает запись справочника из разбора источника.
//
// «Учебная» решается здесь, а не в источнике: тип аудитории приходит только
// из поиска, и адаптер, разбирая имя пары, его не знает. Без этой проверки
// в обход попадали спортзалы и чужие помещения — 767 аудиторий вместо 541.
func auditoriumFrom(oid int64, p source.Auditorium, kind string, capacity int) domain.Auditorium {
	a := domain.Auditorium{
		Oid: oid, Name: p.Name, Room: p.Room, Building: p.Building,
		Site:         domain.Site{Slug: p.Site.Slug, Label: p.Site.Label, Order: p.Site.Order},
		Kind:         kind,
		Floor:        p.Floor,
		IsStudySpace: p.IsStudySpace && studySpaceKind(kind),
	}
	if capacity > 0 {
		a.Capacity = &capacity
	}
	return a
}

// studySpaceKind отсекает по типу то, где заниматься не получится.
// Пустой тип (пара без записи в поиске) считается учебным: лучше показать
// лишнюю аудиторию, чем спрятать настоящую.
func studySpaceKind(kind string) bool {
	k := strings.ToLower(kind)
	for _, bad := range []string{"не принадлежащее университету", "зал аэробики", "тренажерный", "спортивный", "online"} {
		if strings.Contains(k, bad) {
			return false
		}
	}
	return true
}
