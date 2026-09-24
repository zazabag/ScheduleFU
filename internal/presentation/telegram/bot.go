package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Deps — что нужно боту.
type Deps struct {
	Schedule *schedule.Service
	Clock    *clock.Clock
	// BuildingLabel — короткая подпись корпуса; живёт у источника.
	BuildingLabel func(string) string
	// Origin — адрес сайта для ссылок: https://fa.planovo.pro.
	Origin string
	Log    *slog.Logger
}

// Bot — inline-режим и /start.
type Bot struct {
	d    Deps
	api  *client
	name string // имя бота для подсказки: узнаётся при запуске
}

// New собирает бота. base пустой — api.telegram.org; другой — для тестов.
func New(token, base string, d Deps) *Bot {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Bot{d: d, api: newClient(token, base)}
}

// maxResults — сколько групп предлагать на запрос: больше в список
// Telegram всё равно не влезает без прокрутки.
const maxResults = 8

// Run — длинный опрос до отмены контекста.
func (b *Bot) Run(ctx context.Context) error {
	if name, err := b.api.me(ctx); err == nil {
		b.name = name
	} else {
		b.d.Log.Warn("бот: имя не узнано", "ошибка", err)
	}
	var offset int64
	for ctx.Err() == nil {
		ups, err := b.api.updates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.d.Log.Warn("бот: опрос", "ошибка", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			offset = u.ID + 1
			b.handle(ctx, u)
		}
	}
	return nil
}

func (b *Bot) handle(ctx context.Context, u update) {
	switch {
	case u.InlineQuery != nil:
		results := b.Inline(ctx, u.InlineQuery.Query)
		if err := b.api.answerInline(ctx, u.InlineQuery.ID, results); err != nil {
			b.d.Log.Warn("бот: ответ на inline", "ошибка", err)
		}
	case u.Message != nil && u.Message.Chat.Type == "private":
		// В личке — подсказка, как пользоваться; в группах бот молчит: его
		// зовут через @, а не разговором.
		if err := b.api.send(ctx, u.Message.Chat.ID, b.help()); err != nil {
			b.d.Log.Warn("бот: ответ в личку", "ошибка", err)
		}
	}
}

func (b *Bot) help() string {
	name := b.name
	if name == "" {
		name = "имя_бота"
	}
	return "Расписание Финансового университета — прямо в чате группы.\n\n" +
		"В любом чате наберите <code>@" + html.EscapeString(name) + " ПИ24-1</code> и выберите группу: " +
		"в чат уйдёт расписание на сегодня, а если пары уже кончились или их нет — на ближайший день.\n\n" +
		"Неделя, свободные аудитории, конспекты пар — на сайте: " + html.EscapeString(b.d.Origin)
}

// Inline — ответ на «@бот ПИ24»: подходящие группы, у каждой — текст
// расписания на ближайший день с парами.
func (b *Bot) Inline(ctx context.Context, query string) []article {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil
	}
	groups, err := b.d.Schedule.Repo().SearchGroups(ctx, query, maxResults)
	if err != nil {
		b.d.Log.Warn("бот: поиск групп", "ошибка", err)
		return nil
	}
	// Точное совпадение — первым: «ПИ24-1» не должна уступать «ПИ24-10».
	sort.SliceStable(groups, func(i, j int) bool {
		return strings.EqualFold(groups[i].Name, query) && !strings.EqualFold(groups[j].Name, query)
	})
	var out []article
	for _, g := range groups {
		a, ok := b.groupArticle(ctx, g.Name)
		if ok {
			out = append(out, a)
		}
	}
	return out
}

// groupArticle — карточка группы: заголовок, строка-подсказка и текст дня.
func (b *Bot) groupArticle(ctx context.Context, group string) (article, bool) {
	today := b.d.Clock.Today()
	subj := sched.GroupSubject(group)
	lessons, err := b.d.Schedule.ScheduleFor(ctx, subj, today, today.AddDate(0, 0, 6))
	if err != nil {
		return article{}, false
	}
	day, dayLessons := nearestDay(lessons, today, b.d.Clock.HHMM())
	week := b.d.Origin + "/g/" + url.PathEscape(group)
	sum := sha256.Sum256([]byte(group + day.Format("2006-01-02")))
	a := article{Type: "article", ID: hex.EncodeToString(sum[:8]), Title: group,
		Content: messageContent{ParseMode: "HTML", NoPreview: true}}
	if len(dayLessons) == 0 {
		a.Description = "На этой неделе пар нет"
		a.Content.Text = "<b>" + html.EscapeString(group) + "</b>: на этой неделе пар нет.\n\nРасписание: " + week
		return a, true
	}
	when := dayLabel(day, today)
	pairs := slots(dayLessons)
	a.Title = group + " — " + when + ", " + pluralPairs(len(pairs))
	a.Description = pairs[0]
	if p := b.d.BuildingLabel(dayLessons[0].Building); p != "" {
		when += " · " + p
	}
	a.Content.Text = "<b>" + html.EscapeString(group) + " · " + html.EscapeString(when) + "</b>\n" +
		strings.Join(escapeAll(pairs), "\n") + "\n\nВся неделя: " + week
	return a, true
}

// nearestDay — сегодня, если последняя пара ещё не кончилась, иначе
// ближайший день с парами в окне. Вечером в чат нужно завтрашнее, а не
// прошедшее.
func nearestDay(lessons []sched.Lesson, today time.Time, now string) (time.Time, []sched.Lesson) {
	by := map[string][]sched.Lesson{}
	var keys []string
	for _, l := range lessons {
		k := l.DateKey()
		if _, ok := by[k]; !ok {
			keys = append(keys, k)
		}
		by[k] = append(by[k], l)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ls := by[k]
		if k < today.Format("2006-01-02") {
			continue
		}
		if k == today.Format("2006-01-02") && !anyAfter(ls, now) {
			continue
		}
		return ls[0].Date, ls
	}
	return today, nil
}

// slots — строки пар дня, одна на время: подгруппы и поток — одной строкой
// с перечислением аудиторий.
func slots(lessons []sched.Lesson) []string {
	sort.SliceStable(lessons, func(i, j int) bool { return lessons[i].BeginsAt < lessons[j].BeginsAt })
	type slot struct {
		from, to, disc string
		rooms          []string
	}
	var order []string
	by := map[string]*slot{}
	for _, l := range lessons {
		k := l.BeginsAt + l.Discipline
		s, ok := by[k]
		if !ok {
			s = &slot{from: l.BeginsAt, to: l.EndsAt, disc: l.Discipline}
			by[k] = s
			order = append(order, k)
		}
		if r := room(l.Auditorium); r != "" && !contains(s.rooms, r) {
			s.rooms = append(s.rooms, r)
		}
	}
	var out []string
	for _, k := range order {
		s := by[k]
		line := s.from + "–" + s.to + " " + s.disc
		if len(s.rooms) > 0 {
			line += " · " + strings.Join(s.rooms, ", ")
		}
		out = append(out, line)
	}
	return out
}

func anyAfter(ls []sched.Lesson, now string) bool {
	for _, l := range ls {
		if l.EndsAt > now {
			return true
		}
	}
	return false
}

func room(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func escapeAll(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = html.EscapeString(x)
	}
	return out
}

func dayLabel(day, today time.Time) string {
	switch day.Format("2006-01-02") {
	case today.Format("2006-01-02"):
		return "сегодня"
	case today.AddDate(0, 0, 1).Format("2006-01-02"):
		return "завтра"
	}
	return clock.WeekdayRu(day) + ", " + clock.DateRu(day)
}

func pluralPairs(n int) string {
	w := "пар"
	switch {
	case n%10 == 1 && n%100 != 11:
		w = "пара"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		w = "пары"
	}
	return strconv.Itoa(n) + " " + w
}
