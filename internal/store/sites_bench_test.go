package store

import (
	"context"
	"testing"
	"time"
)

// BenchmarkSiteDay меряет то, что делает каждая загрузка главной страницы:
// вытащить все аудитории площадки вместе с парами на день.
func BenchmarkSiteDay(b *testing.B) {
	dsn := "postgres://localhost:5432/schedulefu?sslmode=disable"
	s, err := Open(context.Background(), dsn)
	if err != nil {
		b.Skipf("рабочая база недоступна (%v)", err)
	}
	defer s.Close()

	ctx := context.Background()
	date := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rooms, err := s.SiteDay(ctx, "leningradsky", date)
		if err != nil {
			b.Fatal(err)
		}
		if len(rooms) == 0 {
			b.Skip("в базе нет данных за сегодня")
		}
	}
}
