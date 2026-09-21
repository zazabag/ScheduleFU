// Типы границы живут в пакете source; здесь — алиасы, чтобы клиент говорил
// на языке порта, а старый код собирался до конца переезда.
package ruz

import "github.com/zazabag/schedulefu/internal/modules/source"

type (
	Kind         = source.Kind
	SearchKind   = source.SearchKind
	SearchResult = source.SearchResult
	Lesson       = source.Lesson
)

const (
	KindGroup      = source.KindGroup
	KindLecturer   = source.KindLecturer
	KindAuditorium = source.KindAuditorium

	SearchGroup      = source.SearchGroup
	SearchLecturer   = source.SearchLecturer
	SearchAuditorium = source.SearchAuditorium
)
