package notes

import (
	"context"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

// Поиск fakeRepo не трогает: заглушка держит интерфейс.
func (f *fakeRepo) SearchNotes(context.Context, string, string, int) ([]domain.NoteHit, error) {
	return nil, nil
}
