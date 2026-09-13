// Команда collector — сбор расписания из ruz.fa.ru в базу.
//
//	collector -seed                 загрузить справочники из data/*.json
//	collector -once                 один проход по текущей неделе
//	collector                       непрерывная работа с интервалом
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zazabag/schedulefu/internal/collector"
	"github.com/zazabag/schedulefu/internal/ruz"
	"github.com/zazabag/schedulefu/internal/store"
)

func main() {
	var (
		dsn      = flag.String("dsn", env("DATABASE_URL", "postgres://localhost:5432/schedulefu_dev?sslmode=disable"), "адрес базы")
		dataDir  = flag.String("data", "data", "папка со справочниками")
		seed     = flag.Bool("seed", false, "загрузить справочники из data/*.json и выйти")
		once     = flag.Bool("once", false, "сделать один проход и выйти")
		days     = flag.Int("days", 7, "сколько дней вперёд собирать, начиная с сегодня")
		interval = flag.Duration("interval", time.Hour, "интервал между проходами")
		rps      = flag.Float64("rps", 8, "ограничение частоты запросов к источнику")
		workers  = flag.Int("workers", 6, "сколько аудиторий опрашивать одновременно")
		all      = flag.Bool("all", false, "опрашивать и неучебные помещения (спортзалы, чужие)")
		verbose  = flag.Bool("v", false, "подробный лог")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dsn)
	if err != nil {
		log.Error("база недоступна", "ошибка", err)
		os.Exit(1)
	}
	defer st.Close()

	if *seed {
		if err := seedDirectories(ctx, st, *dataDir, log); err != nil {
			log.Error("справочники не загружены", "ошибка", err)
			os.Exit(1)
		}
		return
	}

	client := ruz.New(ruz.Options{RPS: *rps})
	c := collector.New(client, st, log)
	c.Workers = *workers

	run := func() {
		oids, err := st.AuditoriumOids(ctx, !*all)
		if err != nil {
			log.Error("список аудиторий не прочитан", "ошибка", err)
			return
		}
		if len(oids) == 0 {
			log.Error("справочник аудиторий пуст — запустите с -seed")
			return
		}
		// Собираем от сегодня и на несколько дней вперёд. Весь семестр
		// выкачивать нельзя, см. docs/03-legal-risks.md.
		from := time.Now()
		to := from.AddDate(0, 0, *days-1)
		if _, err := c.RunOnce(ctx, oids, from, to); err != nil {
			log.Error("проход не удался", "ошибка", err)
		}
	}

	run()
	if *once {
		return
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("остановка")
			return
		case <-ticker.C:
			run()
		}
	}
}

// seedDirectories загружает справочники, собранные скриптами из tools/.
func seedDirectories(ctx context.Context, st *store.Store, dir string, log *slog.Logger) error {
	var audDump struct {
		Auditoriums []struct {
			Oid      int64  `json:"oid"`
			Name     string `json:"name"`
			Building string `json:"building"`
			Kind     string `json:"kind"`
			Capacity *int   `json:"capacity"`
		} `json:"auditoriums"`
	}
	if err := readJSON(dir+"/auditoriums.json", &audDump); err != nil {
		return err
	}
	auds := make([]store.Auditorium, 0, len(audDump.Auditoriums))
	for _, r := range audDump.Auditoriums {
		p := ruz.ParseAuditorium(r.Name, r.Building)
		p.Oid, p.Kind, p.Capacity = r.Oid, r.Kind, 0
		site := ruz.SiteOf(p.Building)
		a := store.Auditorium{
			Oid: r.Oid, Name: p.Name, Prefix: p.Prefix, Room: p.Room,
			Building: p.Building, Campus: string(p.Campus), Kind: r.Kind,
			Site: site.Slug, SiteLabel: site.Label, SiteOrder: site.Order,
			Floor: p.Floor, Capacity: r.Capacity,
			IsStudySpace: p.IsStudySpace(),
		}
		auds = append(auds, a)
	}
	if err := st.UpsertAuditoriums(ctx, auds); err != nil {
		return err
	}
	log.Info("справочник аудиторий загружен", "всего", len(auds))

	var grpDump struct {
		Groups []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			FacultyOid string `json:"faculty_oid"`
		} `json:"groups"`
	}
	if err := readJSON(dir+"/groups.json", &grpDump); err != nil {
		return err
	}
	groups := make([]store.Group, 0, len(grpDump.Groups))
	for _, g := range grpDump.Groups {
		groups = append(groups, store.Group{
			ID: g.ID, Name: g.Name, FacultyOid: g.FacultyOid,
			AdmissionYear: admissionYear(g.Name),
		})
	}
	if err := st.UpsertGroups(ctx, groups); err != nil {
		return err
	}
	log.Info("справочник групп загружен", "всего", len(groups))
	return nil
}

// admissionYear достаёт год набора из названия группы: ПИ24-1 -> 2024.
// Курс считается уже от него, чтобы не пересчитывать при смене учебного года.
func admissionYear(name string) *int {
	digits := strings.Builder{}
	for _, r := range name {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			if digits.Len() == 2 {
				break
			}
			continue
		}
		if digits.Len() > 0 {
			break
		}
	}
	if digits.Len() != 2 {
		return nil
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil {
		return nil
	}
	year := 2000 + n
	return &year
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("чтение %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("разбор %s: %w", path, err)
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
