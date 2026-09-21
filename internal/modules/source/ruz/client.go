package ruz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/source"
)

const (
	// DefaultBaseURL — рабочий источник. Та же платформа стоит в других
	// вузах (ruz.hse.ru, ruz.spbstu.ru), поэтому адрес вынесен в параметр.
	DefaultBaseURL = "https://ruz.fa.ru"

	// dateLayout — формат дат в параметрах start/finish.
	// Он отличается от формата даты внутри пары (2026-09-08).
	dateLayout = "2006.01.02"
)

// ErrShortTerm возвращается, когда строка поиска короче трёх символов:
// источник на такой запрос отвечает прикладной ошибкой, а не пустым списком.
var ErrShortTerm = errors.New("ruz: строка поиска короче трёх символов")

// Client — потокобезопасный клиент ruz.fa.ru.
//
// Источник заметно медленнее собственной базы (расписание отдаётся за
// 0.8с в среднем и до 4с в худшем случае), поэтому предполагается, что
// поверх клиента работает кэш, а не пользовательские запросы напрямую.
type Client struct {
	BaseURL   string
	UserAgent string

	http    *http.Client
	limiter *limiter

	// MaxRetries — число повторов при сетевой ошибке или 5xx.
	MaxRetries int
}

// Options — настройки клиента.
type Options struct {
	BaseURL   string
	UserAgent string
	// RPS ограничивает частоту запросов к источнику. Мы ходим в чужую
	// инфраструктуру, которая нам ничего не должна: лучше собирать слепок
	// на минуту дольше, чем получить бан по адресу.
	RPS        float64
	Timeout    time.Duration
	MaxRetries int
}

// New создаёт клиент с разумными значениями по умолчанию.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "ScheduleFU/0.1 (+https://github.com/zazabag/ScheduleFU)"
	}
	if opts.RPS <= 0 {
		opts.RPS = 8
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	return &Client{
		BaseURL:    strings.TrimRight(opts.BaseURL, "/"),
		UserAgent:  opts.UserAgent,
		MaxRetries: opts.MaxRetries,
		limiter:    newLimiter(opts.RPS),
		http: &http.Client{
			Timeout: opts.Timeout,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 16,
			},
		},
	}
}

// Search ищет сущность по подстроке. Строка должна быть не короче трёх
// символов. Выдача обрезается источником, полного списка получить нельзя.
func (c *Client) Search(ctx context.Context, kind SearchKind, term string) ([]SearchResult, error) {
	if len([]rune(strings.TrimSpace(term))) < 3 {
		return nil, ErrShortTerm
	}
	q := url.Values{"term": {term}, "type": {string(kind)}}
	var raw []struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		Label       string `json:"label"`
		Description string `json:"description"`
	}
	if err := c.getJSON(ctx, "/api/search?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(raw))
	for _, r := range raw {
		out = append(out, SearchResult{Type: r.Type, ID: r.ID, Label: r.Label, Description: r.Description})
	}
	return out, nil
}

// Schedule возвращает пары за период по одному из трёх измерений.
//
// id — всегда числовой идентификатор источника: groupID из поиска,
// lecturerOid или auditoriumOid. Передавать сюда GUID нельзя: для
// преподавателя источник ответит чужими парами вместо ошибки.
func (c *Client) Schedule(ctx context.Context, kind Kind, id int64, from, to time.Time) ([]Lesson, error) {
	if id <= 0 {
		return nil, fmt.Errorf("ruz: некорректный идентификатор %d для %s", id, kind)
	}
	if to.Before(from) {
		return nil, fmt.Errorf("ruz: конец периода %s раньше начала %s",
			to.Format(dateLayout), from.Format(dateLayout))
	}
	q := url.Values{
		"start":  {from.Format(dateLayout)},
		"finish": {to.Format(dateLayout)},
		"lng":    {"1"},
	}
	path := "/api/schedule/" + string(kind) + "/" + strconv.FormatInt(id, 10) + "?" + q.Encode()
	var raw []wireLesson
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(raw))
	for _, w := range raw {
		out = append(out, w.lesson())
	}
	return out, nil
}

// ParseAuditorium — реализация порта: разбор по правилам ФУ плюс площадка.
func (c *Client) ParseAuditorium(name, building string) source.Auditorium {
	a := ParseAuditorium(name, building)
	s := SiteOf(building)
	return source.Auditorium{
		Name: a.Name, Room: a.Room, Building: a.Building,
		Site:  source.Site{Slug: s.Slug, Label: s.Label, Order: s.Order},
		Floor: a.Floor, IsReal: a.IsReal(), IsStudySpace: a.IsStudySpace(),
	}
}

// wireLesson — пара в проводном формате источника: из полутора сотен полей
// взяты значимые, имена сохранены, чтобы сверяться с сырым ответом.
type wireLesson struct {
	LessonOid        int64  `json:"lessonOid"`
	Date             string `json:"date"`
	BeginLesson      string `json:"beginLesson"`
	EndLesson        string `json:"endLesson"`
	Auditorium       string `json:"auditorium"`
	AuditoriumOid    int64  `json:"auditoriumOid"`
	AuditoriumAmount int    `json:"auditoriumAmount"`
	Building         string `json:"building"`
	Discipline       string `json:"discipline"`
	KindOfWork       string `json:"kindOfWork"`
	Lecturer         string `json:"lecturer"`
	LecturerOid      int64  `json:"lecturerOid"`
	Stream           string `json:"stream"`
	Group            string `json:"group"`
	SubGroup         string `json:"subGroup"`
	Note             string `json:"note"`
	ModifiedDate     string `json:"modifieddate"`
	DeletionMark     int    `json:"deletion_mark"`
}

func (w wireLesson) lesson() Lesson {
	return Lesson{
		LessonOid: w.LessonOid, Date: w.Date, BeginLesson: w.BeginLesson, EndLesson: w.EndLesson,
		Auditorium: w.Auditorium, AuditoriumOid: w.AuditoriumOid, AuditoriumAmount: w.AuditoriumAmount,
		Building: w.Building, Discipline: w.Discipline, KindOfWork: w.KindOfWork,
		Lecturer: w.Lecturer, LecturerOid: w.LecturerOid, Stream: w.Stream, Group: w.Group,
		SubGroup: w.SubGroup, Note: w.Note, ModifiedDate: w.ModifiedDate, DeletionMark: w.DeletionMark,
	}
}

// apiError — прикладная ошибка источника: HTTP 200 с телом {"error":1,...}.
type apiError struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			// Экспоненциальная пауза: 1с, 2с, 4с.
			delay := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		if err := c.limiter.wait(ctx); err != nil {
			return err
		}
		body, err := c.do(ctx, path)
		if err != nil {
			lastErr = err
			if isRetryable(err) {
				continue
			}
			return err
		}
		// Ответ бывает либо массивом с данными, либо объектом с ошибкой.
		trimmed := strings.TrimLeft(string(body), " \t\r\n")
		if strings.HasPrefix(trimmed, "{") {
			var ae apiError
			if json.Unmarshal(body, &ae) == nil && ae.Message != "" {
				return fmt.Errorf("ruz: источник отказал: %s", ae.Message)
			}
		}
		if err := json.Unmarshal(body, dst); err != nil {
			return fmt.Errorf("ruz: не разобран ответ %s: %w", path, err)
		}
		return nil
	}
	return fmt.Errorf("ruz: запрос %s не удался после %d попыток: %w",
		path, c.MaxRetries+1, lastErr)
}

// retryableError помечает ошибки, которые имеет смысл повторить.
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var re retryableError
	return errors.As(err, &re)
}

func (c *Client) do(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Сетевые сбои и таймауты повторяем.
		return nil, retryableError{err}
	}
	defer resp.Body.Close()

	body, err := readAllLimited(resp.Body, 64<<20)
	if err != nil {
		return nil, retryableError{err}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, retryableError{fmt.Errorf("ruz: %s вернул %s", path, resp.Status)}
	default:
		return nil, fmt.Errorf("ruz: %s вернул %s", path, resp.Status)
	}
}

// Компилятор держит соответствие порту.
var _ source.Source = (*Client)(nil)
