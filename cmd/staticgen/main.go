// Команда staticgen собирает статическую версию сайта для GitHub Pages.
//
// Pages раздаёт только файлы: ни Go, ни PostgreSQL там нет. Поэтому данные
// выгружаются в JSON, а всю работу — «что свободно сейчас», расписание
// группы, поиск преподавателя — берёт на себя браузер.
//
// Данные разложены по дням, а не по группам и преподавателям: файлов на
// каждую группу и каждого преподавателя вышло бы около четырёх с половиной
// тысяч, и любое обновление означало бы столько же изменений в истории.
// День — это ~250 КБ, и его хватает всем четырём экранам сразу.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
	"github.com/zazabag/schedulefu/internal/web"
)

// Ключи в JSON короткие: файл дня грузится на телефоне, и каждое
// повторение имени поля умножается на полторы тысячи пар.
type jsonLesson struct {
	Oid        int64    `json:"o"`
	AudOid     int64    `json:"a,omitempty"`
	Begins     string   `json:"b"`
	Ends       string   `json:"e"`
	Discipline string   `json:"d"`
	Kind       string   `json:"k,omitempty"`
	Lecturer   string   `json:"l,omitempty"`
	LecturerID int64    `json:"lo,omitempty"`
	Groups     []string `json:"g,omitempty"`
}

type jsonDay struct {
	Date        string       `json:"date"`
	GeneratedAt string       `json:"generated_at"`
	Lessons     []jsonLesson `json:"lessons"`
}

type jsonAuditorium struct {
	Oid      int64  `json:"o"`
	Room     string `json:"r"`
	Building string `json:"b"`
	Site     string `json:"s"`
	Floor    *int   `json:"f,omitempty"`
	Capacity *int   `json:"cap,omitempty"`
	Kind     string `json:"k,omitempty"`
}

type jsonSite struct {
	Value string `json:"v"`
	Label string `json:"l"`
	Rooms int    `json:"n"`
}

type jsonLecturer struct {
	Oid  int64  `json:"o"`
	Name string `json:"n"`
}

type jsonGroup struct {
	Name   string `json:"n"`
	Course int    `json:"c,omitempty"`
}

type jsonMeta struct {
	GeneratedAt string `json:"generated_at"`
	// APIBase — адрес сервера, который принимает подписки на уведомления.
	// Pages раздаёт только файлы, подписку принимать некому, поэтому без
	// этого адреса приложение честно прячет кнопку.
	APIBase     string           `json:"api_base,omitempty"`
	Days        []string         `json:"days"`
	Sites       []jsonSite       `json:"sites"`
	Auditoriums []jsonAuditorium `json:"auditoriums"`
	Groups      []jsonGroup      `json:"groups"`
	Lecturers   []jsonLecturer   `json:"lecturers"`
	Slots       []web.Slot       `json:"slots"`
}

func main() {
	var (
		dsn     = flag.String("dsn", env("DATABASE_URL", "postgres://localhost:5432/schedulefu_dev?sslmode=disable"), "адрес базы")
		out     = flag.String("out", "site", "куда писать сайт")
		apiBase = flag.String("api-base", env("API_BASE", ""), "адрес сервера уведомлений; пусто — уведомления недоступны")
	)
	flag.Parse()

	ctx := context.Background()
	st, err := store.Open(ctx, *dsn)
	if err != nil {
		fail("база недоступна: %v", err)
	}
	defer st.Close()

	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}

	if err := os.MkdirAll(filepath.Join(*out, "data", "day"), 0o755); err != nil {
		fail("не создать папку: %v", err)
	}

	days, err := collectDays(ctx, st, loc, *out)
	if err != nil {
		fail("выгрузка дней: %v", err)
	}
	if len(days) == 0 {
		fail("в базе нет расписания — сначала запустите collector")
	}

	if err := writeMeta(ctx, st, loc, *out, days, *apiBase); err != nil {
		fail("выгрузка справочников: %v", err)
	}
	if err := writeAssets(*out); err != nil {
		fail("страницы: %v", err)
	}

	fmt.Printf("готово: %s, дней %d\n", *out, len(days))
}

// collectDays выгружает по файлу на каждый день, который есть в базе.
func collectDays(ctx context.Context, st *store.Store, loc *time.Location, out string) ([]string, error) {
	rows, err := st.Pool().Query(ctx, `
		SELECT lesson_date FROM lessons GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	var dates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, err
		}
		dates = append(dates, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now().In(loc).Format(time.RFC3339)
	var keys []string
	for _, d := range dates {
		key := d.Format("2006-01-02")
		keys = append(keys, key)

		lrows, err := st.Pool().Query(ctx, `
			SELECT lesson_oid, coalesce(auditorium_oid, 0),
			       to_char(begins_at,'HH24:MI'), to_char(ends_at,'HH24:MI'),
			       discipline, kind_of_work, lecturer_name, coalesce(lecturer_oid, 0), group_names
			  FROM lessons
			 WHERE lesson_date = $1
			 ORDER BY begins_at`, d)
		if err != nil {
			return nil, err
		}
		day := jsonDay{Date: key, GeneratedAt: now}
		for lrows.Next() {
			var l jsonLesson
			if err := lrows.Scan(&l.Oid, &l.AudOid, &l.Begins, &l.Ends,
				&l.Discipline, &l.Kind, &l.Lecturer, &l.LecturerID, &l.Groups); err != nil {
				lrows.Close()
				return nil, err
			}
			day.Lessons = append(day.Lessons, l)
		}
		lrows.Close()
		if err := lrows.Err(); err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(out, "data", "day", key+".json"), day); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func writeMeta(ctx context.Context, st *store.Store, loc *time.Location, out string, days []string, apiBase string) error {
	meta := jsonMeta{
		GeneratedAt: time.Now().In(loc).Format(time.RFC3339),
		APIBase:     strings.TrimRight(apiBase, "/"),
		Days:        days,
		Slots:       web.Slots,
	}

	sites, err := st.Sites(ctx)
	if err != nil {
		return err
	}
	for _, c := range sites {
		meta.Sites = append(meta.Sites, jsonSite{Value: c.Site, Label: c.Label, Rooms: c.Rooms})
	}

	rooms, err := st.Pool().Query(ctx, `
		SELECT oid, room, building, site, floor, capacity, kind
		  FROM auditoriums
		 WHERE is_study_space AND site <> 'branch'
		 ORDER BY site_order, floor NULLS LAST, room`)
	if err != nil {
		return err
	}
	for rooms.Next() {
		var a jsonAuditorium
		var building string
		if err := rooms.Scan(&a.Oid, &a.Room, &building, &a.Site, &a.Floor, &a.Capacity, &a.Kind); err != nil {
			rooms.Close()
			return err
		}
		a.Building = web.ShortBuilding(building)
		meta.Auditoriums = append(meta.Auditoriums, a)
	}
	rooms.Close()

	// Группы берём из самих пар: в справочнике источника их 479, а в
	// расписании встречается две с половиной тысячи — магистратура,
	// аспирантура и потоки, которых поиск по «год-номер» не находит.
	//
	// Половину этого списка составляют не группы, а склеенные строки вроде
	// «006886_1 Иностранный язык (КАЯиПК)-1»: у языковых занятий поток
	// пустой, и в поле группы попадает код дисциплины с названием подгруппы.
	// В списке для выбора им не место, поэтому берём только то, что выглядит
	// как настоящая группа: буквы, две цифры года, дефис и номер.
	grows, err := st.Pool().Query(ctx, `
		SELECT DISTINCT unnest(group_names) AS g
		  FROM lessons
		 WHERE EXISTS (
		       SELECT 1 FROM unnest(group_names) x
		        WHERE x ~ '^[А-Яа-яЁёA-Za-z]+[0-9]{2}-[0-9]+[а-яА-Я]*$')
		 ORDER BY g`)
	if err != nil {
		return err
	}
	year := academicYear(time.Now().In(loc))
	for grows.Next() {
		var name string
		if err := grows.Scan(&name); err != nil {
			grows.Close()
			return err
		}
		if !groupNamePattern.MatchString(name) {
			continue
		}
		meta.Groups = append(meta.Groups, jsonGroup{Name: name, Course: courseFromName(name, year)})
	}
	grows.Close()

	lrows, err := st.Pool().Query(ctx, `
		SELECT DISTINCT lecturer_oid, lecturer_name
		  FROM lessons
		 WHERE lecturer_oid IS NOT NULL AND lecturer_name <> ''
		 ORDER BY lecturer_name`)
	if err != nil {
		return err
	}
	for lrows.Next() {
		var l jsonLecturer
		if err := lrows.Scan(&l.Oid, &l.Name); err != nil {
			lrows.Close()
			return err
		}
		meta.Lecturers = append(meta.Lecturers, l)
	}
	lrows.Close()

	return writeJSON(filepath.Join(out, "data", "meta.json"), meta)
}

// groupNamePattern описывает настоящее имя группы: ПИ24-1, Ю24-10, ПИ24-1в.
var groupNamePattern = regexp.MustCompile(`^[\p{L}]+[0-9]{2}-[0-9]+[\p{L}]*$`)

// academicYear: учебный год начинается в сентябре.
func academicYear(t time.Time) int {
	if int(t.Month()) >= 9 {
		return t.Year()
	}
	return t.Year() - 1
}

// courseFromName достаёт курс из названия группы: ПИ24-1 -> набор 2024.
func courseFromName(name string, academicYear int) int {
	digits := 0
	count := 0
	for _, r := range name {
		if r >= '0' && r <= '9' {
			digits = digits*10 + int(r-'0')
			count++
			if count == 2 {
				break
			}
			continue
		}
		if count > 0 {
			break
		}
	}
	if count != 2 {
		return 0
	}
	c := academicYear - (2000 + digits) + 1
	if c < 1 || c > 6 {
		return 0
	}
	return c
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(v)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
