package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

func testRepo(t *testing.T) (*Repo, context.Context) {
	t.Helper()
	pool := db.TestPool(t, "TEST_DATABASE_URL_NOTES", "schedulefu_test_notes", "cards", "homeworks", "notes", "recordings")
	return New(pool), context.Background()
}

// Модель вправе не вернуть ни одного тезиса — на минутной записи их и нет.
// Пустой список не должен уронить запись конспекта на последнем шаге:
// колонка theses NOT NULL, а nil-срез уходит в базу как NULL.
func TestKonspektBezTezisovZapisyvaetsya(t *testing.T) {
	r, ctx := testRepo(t)
	lesson := domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "История",
		Date: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)}
	if _, err := r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Body: "текст"}); err != nil {
		t.Fatalf("конспект без тезисов: %v", err)
	}
}

// Баг 23.09.2026: готовый конспект до «Сохранить» был черновиком, а экран
// предмета показывал только сохранённые — и сам черновик, и его запись
// пропадали из виду, стоило уйти со страницы записи. Черновики должны
// находиться отдельно: своего владельца, своего предмета.
func TestChernovikiNahodyatsyaOtdelnoOtSohranyonnyh(t *testing.T) {
	r, ctx := testRepo(t)
	lesson := domain.LessonRef{SubjectKey: "group:УПП26-2", Discipline: "Основы менеджмента",
		Date: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)}
	draft, err := r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Title: "Черновик"})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Title: "Сохранённый"})
	if err := r.SaveNote(ctx, "я", saved, time.Now()); err != nil {
		t.Fatal(err)
	}
	_, _ = r.CreateNote(ctx, domain.Note{OwnerKey: "чужой", Lesson: lesson, Title: "Чужой"})

	drafts, err := r.DraftNotes(ctx, "я", lesson.SubjectKey, lesson.Discipline)
	if err != nil || len(drafts) != 1 || drafts[0].ID != draft {
		t.Fatalf("черновики: %+v %v", drafts, err)
	}
	if list, _ := r.Notes(ctx, "я", lesson.SubjectKey, lesson.Discipline); len(list) != 1 || list[0].ID != saved {
		t.Errorf("сохранённые: %+v", list)
	}
}

func TestUchyotVyzovovModeli(t *testing.T) {
	r, ctx := testRepo(t)
	if _, err := r.pool.Exec(ctx, `DELETE FROM llm_calls`); err != nil {
		t.Fatal(err)
	}
	old := domain.LLMCall{At: time.Now().Add(-100 * 24 * time.Hour), Kind: "summary", Model: "m", OK: true, PromptTokens: 10}
	fresh := domain.LLMCall{At: time.Now(), Kind: "probe", Model: "m", Code: "1304", Message: "лимит"}
	for _, c := range []domain.LLMCall{old, fresh} {
		if err := r.LogLLMCall(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.CleanupLLMCalls(ctx, 90*24*time.Hour); err != nil || n != 1 {
		t.Errorf("убрано %d (%v), ожидалась одна старая строка", n, err)
	}
}

// Поиск находит слово в другой форме, ищет только в своих сохранённых и
// отдаёт отрывок с границами подсветки, а не с HTML.
func TestPoiskPoSvoimKonspektam(t *testing.T) {
	r, ctx := testRepo(t)
	lesson := domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы", Date: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)}
	mine, _ := r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Title: "Облигации",
		Body: "Дюрация облигации показывает чувствительность цены к ставке <b>не тег</b>."})
	_ = r.SaveNote(ctx, "я", mine, time.Now())
	_, _ = r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Title: "Черновик", Body: "дюрация в черновике"})
	other, _ := r.CreateNote(ctx, domain.Note{OwnerKey: "чужой", Lesson: lesson, Title: "Чужое", Body: "дюрация у соседа"})
	_ = r.SaveNote(ctx, "чужой", other, time.Now())

	hits, err := r.SearchNotes(ctx, "я", "дюрацию", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Note.ID != mine {
		t.Fatalf("найдено: %+v", hits)
	}
	if !strings.Contains(hits[0].Snippet, domain.HitStart+"Дюрация"+domain.HitEnd) {
		t.Errorf("подсветка: %q", hits[0].Snippet)
	}
}

// Карточки: только по сохранённому, очередь отдаёт конспект один раз,
// повторная генерация не плодит дублей, счёт — на сегодня.
func TestKartochkiVBaze(t *testing.T) {
	r, ctx := testRepo(t)
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	lesson := domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы", Date: day.AddDate(0, 0, -7)}
	draft, _ := r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Body: "черновик"})
	if ok, _ := r.QueueCards(ctx, "я", draft, day); ok {
		t.Error("черновик в очередь карточек не ставится")
	}
	id, _ := r.CreateNote(ctx, domain.Note{OwnerKey: "я", Lesson: lesson, Body: "текст"})
	_ = r.SaveNote(ctx, "я", id, day)
	if ok, err := r.QueueCards(ctx, "я", id, day); !ok || err != nil {
		t.Fatalf("в очередь: %v %v", ok, err)
	}
	n, ok, err := r.ClaimCards(ctx, day)
	if !ok || err != nil || n.ID != id || n.CardsStatus != "working" {
		t.Fatalf("из очереди: %+v %v %v", n, ok, err)
	}
	if _, again, _ := r.ClaimCards(ctx, day); again {
		t.Error("конспект взят из очереди дважды")
	}
	mk := func(kind domain.CardKind, front string, due time.Time) domain.Card {
		return domain.Card{OwnerKey: "я", NoteID: &id, Lesson: lesson, Kind: kind, Front: front, Back: "ответ", Box: 1, DueOn: due}
	}
	added, err := r.AddCards(ctx, []domain.Card{mk(domain.CardQuestion, "Что такое дюрация?", day), mk(domain.CardQuestion, "Что такое купон?", day.AddDate(0, 0, 3)), mk(domain.CardTerm, "Купон", day)})
	if err != nil || added != 3 {
		t.Fatalf("добавлено %d: %v", added, err)
	}
	if again, _ := r.AddCards(ctx, []domain.Card{mk(domain.CardQuestion, "Что такое дюрация?", day)}); again != 0 {
		t.Error("повторная генерация добавила дубль")
	}
	_ = r.FinishCards(ctx, id, "")
	due, total, next, err := r.CardStats(ctx, "я", "group:ПИ24-1", "Финансы", day)
	if err != nil || due != 1 || total != 2 || next == nil {
		t.Errorf("счёт: %d из %d, следующая %v, %v", due, total, next, err)
	}
	cards, _ := r.DueCards(ctx, "я", "group:ПИ24-1", "Финансы", day, 5)
	if len(cards) != 1 || cards[0].Front != "Что такое дюрация?" {
		t.Fatalf("к повторению: %+v", cards)
	}
	c := cards[0].Review(true, day)
	if err := r.SaveReview(ctx, c, day); err != nil {
		t.Fatal(err)
	}
	if left, _, _, _ := r.CardStats(ctx, "я", "group:ПИ24-1", "Финансы", day); left != 0 {
		t.Error("отвеченная «помню» карточка осталась на сегодня")
	}
	if terms, _ := r.Terms(ctx, "я", "group:ПИ24-1", "Финансы"); len(terms) != 1 {
		t.Errorf("словарь: %+v", terms)
	}
}

// Для напоминаний — только сохранённые, не сделанные и заданные в окне.
func TestNesdelannyeZadaniyaDlyaNapominaniy(t *testing.T) {
	r, ctx := testRepo(t)
	at := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	add := func(owner string, d int, body string, save, done bool) {
		t.Helper()
		id, err := r.CreateHomework(ctx, domain.Homework{OwnerKey: owner, Body: body, Origin: "manual",
			Lesson: domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы", Date: at(d)}})
		if err != nil {
			t.Fatal(err)
		}
		if save {
			if err := r.SaveHomework(ctx, owner, id, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if done {
			now := time.Now()
			if err := r.SetHomeworkDone(ctx, owner, id, &now); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("a", 20, "нужное", true, false)
	add("a", 21, "черновик", false, false)
	add("b", 21, "сделано", true, true)
	add("b", 2, "давнее", true, false)
	add("b", 24, "сегодняшнее", true, false)
	got, err := r.PendingHomeworks(ctx, at(10), at(24))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "нужное" {
		t.Errorf("задания для напоминаний: %+v", got)
	}
}
