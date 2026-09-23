package notes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

// ErrNotShareable — делиться можно только сохранённым своим конспектом.
var ErrNotShareable = errors.New("поделиться можно только сохранённым конспектом")

// ErrShareClosed — ссылки нет: её не было или автор её закрыл.
var ErrShareClosed = errors.New("ссылка на конспект закрыта или не существует")

// Share открывает ссылку на конспект и возвращает её ключ. Повторный вызов
// отдаёт тот же ключ: ссылку уже могли разослать, и новая сломала бы её.
//
// Обмен без условий — решение автора: конспект человек и так может
// скопировать и переслать. Мы лишь делаем это удобным, а ключ случайный,
// чтобы по ссылке открывался ровно тот конспект, которым поделились.
func (s *Service) Share(ctx context.Context, owner string, id int64) (string, error) {
	n, ok, err := s.repo.Note(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if !ok || !n.Saved() {
		return "", ErrNotShareable
	}
	if n.ShareToken != "" {
		return n.ShareToken, nil
	}
	token, err := newShareToken()
	if err != nil {
		return "", err
	}
	if ok, err := s.repo.SetShareToken(ctx, owner, id, token, s.clk.Now()); err != nil {
		return "", err
	} else if !ok {
		return "", ErrNotShareable
	}
	return token, nil
}

// Unshare закрывает ссылку. Сохранённые получателями копии остаются у них.
func (s *Service) Unshare(ctx context.Context, owner string, id int64) error {
	_, err := s.repo.SetShareToken(ctx, owner, id, "", s.clk.Now())
	return err
}

// Shared — конспект по ссылке вместе с заданиями. Ключ владельца наружу не
// отдаётся: по нему можно было бы писать от чужого имени.
func (s *Service) Shared(ctx context.Context, token string) (domain.Note, []domain.Homework, bool, error) {
	if !validShareToken(token) {
		return domain.Note{}, nil, false, nil
	}
	n, ok, err := s.repo.NoteByShare(ctx, token)
	if err != nil || !ok {
		return domain.Note{}, nil, false, err
	}
	hws, err := s.repo.HomeworksByNote(ctx, n.OwnerKey, n.ID)
	if err != nil {
		return domain.Note{}, nil, false, err
	}
	var saved []domain.Homework
	for _, h := range hws {
		if h.Saved() {
			h.OwnerKey = ""
			saved = append(saved, h)
		}
	}
	n.OwnerKey = ""
	return n, saved, true, nil
}

// SaveShared сохраняет конспект по ссылке себе: копией, со своими заданиями,
// под своим расписанием (subjectKey), чтобы он лёг в раздел «Пары» к тому же
// предмету. Пустой subjectKey — расписание автора. Повтор не плодит копий.
func (s *Service) SaveShared(ctx context.Context, owner, subjectKey, token string) (domain.Note, error) {
	if !validShareToken(token) {
		return domain.Note{}, ErrShareClosed
	}
	src, ok, err := s.repo.NoteByShare(ctx, token)
	if err != nil {
		return domain.Note{}, err
	}
	if !ok {
		return domain.Note{}, ErrShareClosed
	}
	if src.OwnerKey == owner {
		return src, nil
	}
	if have, ok, err := s.repo.CopyOf(ctx, owner, src.ID); err != nil || ok {
		return have, err
	}
	hws, err := s.repo.HomeworksByNote(ctx, src.OwnerKey, src.ID)
	if err != nil {
		return domain.Note{}, err
	}

	cp := src
	cp.ID, cp.OwnerKey, cp.RecordingID, cp.ShareToken = 0, owner, nil, ""
	cp.CopiedFrom = &src.ID
	if subjectKey != "" {
		cp.Lesson.SubjectKey = subjectKey
	}
	if cp.ID, err = s.repo.CreateNote(ctx, cp); err != nil {
		return domain.Note{}, err
	}
	now := s.clk.Now()
	if err := s.repo.SaveNote(ctx, owner, cp.ID, now); err != nil {
		return domain.Note{}, err
	}
	for _, h := range hws {
		if !h.Saved() {
			continue
		}
		h.ID, h.OwnerKey, h.NoteID, h.DoneAt = 0, owner, &cp.ID, nil
		h.Lesson.SubjectKey = cp.Lesson.SubjectKey
		id, err := s.repo.CreateHomework(ctx, h)
		if err != nil {
			return domain.Note{}, err
		}
		if err := s.repo.SaveHomework(ctx, owner, id, now); err != nil {
			return domain.Note{}, err
		}
	}
	cp.SavedAt = &now
	return cp, nil
}

// SavedCopy — конспект по ссылке, который у владельца уже есть: его
// собственный или сохранённая копия.
func (s *Service) SavedCopy(ctx context.Context, owner, token string) (domain.Note, bool, error) {
	if !validShareToken(token) {
		return domain.Note{}, false, nil
	}
	src, ok, err := s.repo.NoteByShare(ctx, token)
	if err != nil || !ok {
		return domain.Note{}, false, err
	}
	if src.OwnerKey == owner {
		return src, true, nil
	}
	return s.repo.CopyOf(ctx, owner, src.ID)
}

// newShareToken — 18 случайных байт: 24 символа в ссылке, перебор
// бессмыслен.
func newShareToken() (string, error) {
	var buf [18]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// validShareToken проверяет форму ключа до похода в базу: он приходит из
// адреса и может быть любым.
func validShareToken(t string) bool {
	if len(t) != 24 {
		return false
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
