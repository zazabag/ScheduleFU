package domain

import (
	"strings"
	"time"
)

// CardKind — вид карточки: вопрос для повторения или термин словаря.
type CardKind string

const (
	CardQuestion CardKind = "card"
	CardTerm     CardKind = "term"
)

// Card — карточка для повторения или статья словаря. Живёт у предмета, а
// не у конспекта: конспект удалят, а выученное должно остаться.
type Card struct {
	ID       int64
	OwnerKey string
	NoteID   *int64
	Lesson   LessonRef // чьё расписание и предмет; дата — пары, откуда карточка
	Kind     CardKind
	Front    string // вопрос или термин
	Back     string // ответ или определение
	Box      int    // коробка Лейтнера, 1…5
	DueOn    time.Time
}

// CardSet — что модель вынула из конспекта.
type CardSet struct {
	Cards []QA
	Terms []QA
}

// QA — пара «лицо — оборот».
type QA struct{ Front, Back string }

// Интервалы повторения по коробкам Лейтнера, в днях. Удвоение — самая
// простая схема, которая работает: выученное всплывает реже, забытое —
// сразу.
var boxDays = [...]int{0, 1, 2, 4, 8, 16}

// MaxBox — последняя коробка.
const MaxBox = len(boxDays) - 1

// Review — ответ на карточку. «Помню» — в следующую коробку и дальше по
// интервалу; «не помню» — в первую и снова сегодня, в конце подхода.
func (c Card) Review(remembered bool, today time.Time) Card {
	if !remembered {
		c.Box, c.DueOn = 1, today
		return c
	}
	if c.Box < MaxBox {
		c.Box++
	}
	c.DueOn = today.AddDate(0, 0, boxDays[c.Box])
	return c
}

// Потолки на то, что вынимает модель: карточек, которые никто не пройдёт,
// лучше не делать, а длинный ответ на карточке — уже не карточка.
const (
	maxCards    = 12
	maxTerms    = 15
	maxCardText = 400
)

// Clean отбрасывает пустое, дубли и лишнее; обрезает длинное.
func (s CardSet) Clean() CardSet {
	clean := func(in []QA, limit int) []QA {
		seen := map[string]bool{}
		var out []QA
		for _, q := range in {
			f, b := strings.TrimSpace(q.Front), strings.TrimSpace(q.Back)
			k := strings.ToLower(f)
			if f == "" || b == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, QA{Front: cut(f, maxCardText), Back: cut(b, maxCardText)})
			if len(out) == limit {
				break
			}
		}
		return out
	}
	return CardSet{Cards: clean(s.Cards, maxCards), Terms: clean(s.Terms, maxTerms)}
}

func cut(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
