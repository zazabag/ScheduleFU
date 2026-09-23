package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

func testRepo(t *testing.T) (*Repo, context.Context) {
	t.Helper()
	pool := db.TestPool(t, "TEST_DATABASE_URL_NOTES", "schedulefu_test_notes", "homeworks", "notes", "recordings")
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
