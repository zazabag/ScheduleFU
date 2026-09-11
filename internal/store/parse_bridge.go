package store

import "github.com/zazabag/schedulefu/internal/ruz"

// parseForStore — мост к разбору имён аудиторий.
//
// Все правила разбора живут в пакете ruz, чтобы знание о странностях
// источника не расползалось по проекту; здесь только перевод в модель
// хранилища.
func parseForStore(name, building string) Auditorium {
	a := ruz.ParseAuditorium(name, building)
	return Auditorium{
		Oid:          a.Oid,
		Name:         a.Name,
		Prefix:       a.Prefix,
		Room:         a.Room,
		Building:     a.Building,
		Campus:       string(a.Campus),
		Kind:         a.Kind,
		Floor:        a.Floor,
		IsStudySpace: a.IsStudySpace(),
	}
}
