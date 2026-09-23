// Package config — настройки приложения.
//
// Источника два, и порядок важен: YAML-файл задаёт стенд, переменные
// окружения с префиксом SCHEDULEFU_ накладываются поверх. Секреты живут
// только в окружении — файл попадает в репозиторий стенда, окружение нет.
// Тот же приём, что AKEDA_GO_* в Akeda: тот, кто обслуживает оба продукта,
// не учит два способа конфигурирования.
package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config — все настройки приложения.
//
// Теги yaml задают ключ в файле; имя переменной окружения выводится из
// пути: modules.notify.vapid_private -> SCHEDULEFU_NOTIFY_VAPID_PRIVATE.
type Config struct {
	Stand  Stand  `yaml:"stand"`
	HTTP   HTTP   `yaml:"http"`
	DB     DB     `yaml:"db"`
	Source Source `yaml:"source"`
	Notify Notify `yaml:"notify"`
	Notes  Notes  `yaml:"notes"`
	Static Static `yaml:"static"`
}

// Stand — паспорт контура: отдаётся в /healthz, чтобы отличать прод от
// стенда, не заглядывая в конфиг.
type Stand struct {
	Env    string `yaml:"env"`    // dev | prod
	Origin string `yaml:"origin"` // https://schedulefu.example
}

// HTTP — сервер.
type HTTP struct {
	Addr       string  `yaml:"addr"`
	RPS        float64 `yaml:"rps"`
	Burst      float64 `yaml:"burst"`
	TrustProxy bool    `yaml:"trust_proxy"`
}

// DB — подключение к PostgreSQL.
type DB struct {
	DSN string `yaml:"dsn"`
}

// Source — источник расписания.
//
// University — ключ вуза. Платформа РУЗ тиражная (ruz.hse.ru, ruz.spbstu.ru),
// и второй вуз добавляется сменой BaseURL и таблицы площадок, а не форком.
type Source struct {
	University string        `yaml:"university"`
	BaseURL    string        `yaml:"base_url"`
	RPS        float64       `yaml:"rps"`
	Workers    int           `yaml:"workers"`
	Interval   time.Duration `yaml:"interval"`
	// Ночью обход реже: с NightFrom до NightTo (ЧЧ:ММ, пояс вуза) — раз в
	// NightInterval. Нулевой NightInterval выключает ночной темп.
	NightInterval time.Duration `yaml:"night_interval"`
	NightFrom     string        `yaml:"night_from"`
	NightTo       string        `yaml:"night_to"`
	Days          int           `yaml:"days"`
	Timezone      string        `yaml:"timezone"`
}

// Notify — уведомления.
type Notify struct {
	VAPIDPublic  string `yaml:"vapid_public"`
	VAPIDPrivate string `yaml:"vapid_private"`
	Subject      string `yaml:"subject"`
}

// Notes — записи пар и конспекты.
//
// Раздел включается целиком: без распознавания и модели принимать запись
// нечестно — человек проговорит полтора часа и не получит ничего.
type Notes struct {
	Enabled  bool   `yaml:"enabled"`
	AudioDir string `yaml:"audio_dir"`
	// MaxMinutes — потолок длительности одной записи.
	MaxMinutes int `yaml:"max_minutes"`
	// MaxMB — потолок размера. Пара в Opus 24 кбит/с — около 16 МБ.
	MaxMB int64 `yaml:"max_mb"`
	// KeepAudio оставляет запись на диске после расшифровки. По умолчанию
	// выключено: голос преподавателя у нас не хранится.
	KeepAudio bool        `yaml:"keep_audio"`
	FFmpeg    string      `yaml:"ffmpeg"`
	ASR       NotesASR    `yaml:"asr"`
	LLM       NotesLLM    `yaml:"llm"`
	Worker    NotesWorker `yaml:"worker"`
}

// NotesASR — распознавание речи на своём сервере.
type NotesASR struct {
	Command  string        `yaml:"command"`
	ModelDir string        `yaml:"model_dir"`
	Threads  int           `yaml:"threads"`
	Timeout  time.Duration `yaml:"timeout"`
}

// NotesLLM — сервис, пишущий конспект. Подходит любой с интерфейсом в
// стиле OpenAI: GLM, GigaChat, локальная модель — разница в трёх строках.
type NotesLLM struct {
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
	// APIKey живёт только в окружении: SCHEDULEFU_NOTES_LLM_API_KEY.
	APIKey string `yaml:"api_key"`
	// NoThinking выключает «размышления»: думающая модель на промпте в
	// тридцать тысяч токенов рассуждает минутами. Для GLM обязательно, для
	// провайдера, который такого поля не знает, — выключить.
	NoThinking bool          `yaml:"no_thinking"`
	MaxChars   int           `yaml:"max_chars"`
	Timeout    time.Duration `yaml:"timeout"`
}

// NotesWorker — поведение обработчика очереди.
type NotesWorker struct {
	Idle     time.Duration `yaml:"idle"`
	Retry    time.Duration `yaml:"retry"`
	Attempts int           `yaml:"attempts"`
	// DraftDays — сколько живёт конспект, который не сохранили.
	DraftDays int `yaml:"draft_days"`
}

// Static — сборка версии для GitHub Pages.
type Static struct {
	OutDir  string `yaml:"out_dir"`
	APIBase string `yaml:"api_base"`
}

// Default — значения, с которыми приложение запускается без файла вовсе:
// локальная база, локальный вуз, разумная вежливость к источнику.
func Default() Config {
	return Config{
		Stand: Stand{Env: "dev"},
		HTTP:  HTTP{Addr: ":8090", RPS: 8, Burst: 30},
		DB:    DB{DSN: "postgres://localhost:5432/schedulefu?sslmode=disable"},
		Source: Source{
			University: "fa",
			BaseURL:    "https://ruz.fa.ru",
			// Четыре запроса в секунду и четыре потока: обход идёт две с
			// половиной минуты, пик для вуза вдвое ниже прежнего.
			RPS:           4,
			Workers:       4,
			Interval:      time.Hour,
			NightInterval: 3 * time.Hour,
			NightFrom:     "23:00",
			NightTo:       "06:30",
			Days:          7,
			Timezone:      "Europe/Moscow",
		},
		Notify: Notify{Subject: "mailto:schedulefu@example.org"},
		Notes: Notes{
			AudioDir:   "/var/lib/schedulefu/audio",
			MaxMinutes: 240,
			MaxMB:      512,
			FFmpeg:     "ffmpeg",
			ASR: NotesASR{
				Command:  "sherpa-onnx-vad-with-offline-asr",
				ModelDir: "/var/lib/schedulefu/models/gigaam-v3",
				Threads:  4,
				Timeout:  2 * time.Hour,
			},
			LLM: NotesLLM{
				// Китайские модели отвечают на российские адреса, в отличие
				// от западных; бесплатная glm-4.5-flash с окном 128k берёт
				// полуторачасовую пару одним запросом.
				BaseURL:    "https://open.bigmodel.cn/api/paas/v4",
				Model:      "glm-4.5-flash",
				NoThinking: true,
				MaxChars:   150000,
				Timeout:    10 * time.Minute,
			},
			Worker: NotesWorker{Idle: 20 * time.Second, Retry: 10 * time.Minute, Attempts: 3, DraftDays: 14},
		},
		Static: Static{OutDir: "site"},
	}
}

// Load читает файл (если путь непустой), затем накладывает окружение.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("config: чтение %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("config: разбор %s: %w", path, err)
		}
	}
	if err := applyEnv(reflect.ValueOf(&cfg).Elem(), "SCHEDULEFU"); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

// Validate ловит то, с чем запускаться нельзя.
func (c Config) Validate() error {
	switch {
	case c.DB.DSN == "":
		return fmt.Errorf("config: не задан db.dsn")
	case c.Source.BaseURL == "":
		return fmt.Errorf("config: не задан source.base_url")
	case c.Source.University == "":
		return fmt.Errorf("config: не задан source.university")
	case c.Source.Days < 1 || c.Source.Days > 14:
		// Больше двух недель — уже не «посмотреть расписание», а копирование
		// базы вуза (docs/03-legal-risks.md). Потолок держится здесь, чтобы
		// его нельзя было обойти флагом.
		return fmt.Errorf("config: source.days должен быть от 1 до 14, задано %d", c.Source.Days)
	case !isHHMM(c.Source.NightFrom) || !isHHMM(c.Source.NightTo):
		return fmt.Errorf("config: source.night_from и night_to — ЧЧ:ММ, задано %q и %q",
			c.Source.NightFrom, c.Source.NightTo)
	case (c.Notify.VAPIDPublic == "") != (c.Notify.VAPIDPrivate == ""):
		return fmt.Errorf("config: ключи уведомлений задаются парой")
	case c.Notes.Enabled && c.Notes.AudioDir == "":
		return fmt.Errorf("config: не задан notes.audio_dir")
	case c.Notes.Enabled && c.Notes.MaxMinutes < 1:
		return fmt.Errorf("config: notes.max_minutes должен быть положительным")
	}
	return nil
}

// isHHMM — пусто или «ЧЧ:ММ» с ведущими нулями: ночное окно сравнивается
// строками, и «6:30» без нуля сломало бы порядок молча.
func isHHMM(v string) bool {
	if v == "" {
		return true
	}
	t, err := time.Parse("15:04", v)
	return err == nil && t.Format("15:04") == v
}

// NotesReady сообщает, настроена ли обработка записей до конца. Раздел без
// ключа модели показывать можно — конспекты, записанные раньше, никуда не
// делись, — а принимать новые записи нельзя.
func (c Config) NotesReady() bool {
	return c.Notes.Enabled && c.Notes.LLM.APIKey != "" && c.Notes.LLM.BaseURL != "" && c.Notes.LLM.Model != ""
}

// PushEnabled сообщает, настроены ли уведомления.
func (c Config) PushEnabled() bool {
	return c.Notify.VAPIDPublic != "" && c.Notify.VAPIDPrivate != ""
}

// applyEnv обходит структуру и подменяет поля переменными окружения.
// Имя строится из yaml-тегов по пути: SCHEDULEFU_SOURCE_BASE_URL.
func applyEnv(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := prefix + "_" + strings.ToUpper(tag)
		fv := v.Field(i)

		if fv.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Duration(0)) {
			if err := applyEnv(fv, name); err != nil {
				return err
			}
			continue
		}
		raw, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if err := setFromString(fv, raw); err != nil {
			return fmt.Errorf("config: %s: %w", name, err)
		}
	}
	return nil
}

func setFromString(fv reflect.Value, raw string) error {
	switch fv.Interface().(type) {
	case time.Duration:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		fv.SetInt(int64(d))
		return nil
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("неподдерживаемый тип %s", fv.Kind())
	}
	return nil
}
