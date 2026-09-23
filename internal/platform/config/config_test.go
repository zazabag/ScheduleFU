package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultZapuskaetsyaBezFayla(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("без файла должно запускаться: %v", err)
	}
	if cfg.Source.BaseURL != "https://ruz.fa.ru" || cfg.Source.Days != 7 {
		t.Errorf("умолчания источника: %+v", cfg.Source)
	}
}

func TestYAMLZatemOkruzhenie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("http:\n  addr: \":9000\"\nsource:\n  days: 3\n  interval: 30m\n"), 0o644)

	// Окружение сильнее файла: секрет или переопределение стенда не должны
	// требовать правки файла, который лежит в репозитории.
	t.Setenv("SCHEDULEFU_SOURCE_DAYS", "5")
	t.Setenv("SCHEDULEFU_NOTIFY_VAPID_PUBLIC", "pub")
	t.Setenv("SCHEDULEFU_NOTIFY_VAPID_PRIVATE", "priv")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":9000" {
		t.Errorf("из файла не прочитан addr: %q", cfg.HTTP.Addr)
	}
	if cfg.Source.Days != 5 {
		t.Errorf("окружение не перекрыло файл: days=%d", cfg.Source.Days)
	}
	if cfg.Source.Interval != 30*time.Minute {
		t.Errorf("интервал из файла: %s", cfg.Source.Interval)
	}
	if !cfg.PushEnabled() {
		t.Error("ключи из окружения не включили уведомления")
	}
}

// TestPotolokDneyDerzhitsyaVKonfige: выкачивать семестр нельзя по праву, и
// потолок живёт в валидации, чтобы его нельзя было обойти флагом.
func TestPotolokDneyDerzhitsyaVKonfige(t *testing.T) {
	t.Setenv("SCHEDULEFU_SOURCE_DAYS", "120")
	if _, err := Load(""); err == nil {
		t.Fatal("120 дней окна приняты, а это уже архив базы вуза")
	}
}

func TestKlyuchiTolkoParoy(t *testing.T) {
	t.Setenv("SCHEDULEFU_NOTIFY_VAPID_PUBLIC", "pub")
	if _, err := Load(""); err == nil {
		t.Fatal("один ключ из двух принят — отправка молча не заработает")
	}
}

// Ночное окно сравнивается строками, поэтому «6:30» без ведущего нуля
// обязано валить запуск, а не молча сдвигать ночь.
func TestNochnoeOknoTolkoChChMM(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("умолчания: %v", err)
	}
	cfg.Source.NightTo = "6:30"
	if cfg.Validate() == nil {
		t.Error("«6:30» принято")
	}
	cfg.Source.NightFrom, cfg.Source.NightTo = "", ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("пустое окно: %v", err)
	}
}

func TestGdePrepodavatelVklyuchenIVyklyuchaetsyaOkruzheniem(t *testing.T) {
	if !Default().Privacy.WhereLecturer {
		t.Fatal("по умолчанию функция работает, как работала до выключателя")
	}
	t.Setenv("SCHEDULEFU_PRIVACY_WHERE_LECTURER", "false")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Privacy.WhereLecturer {
		t.Error("выключатель из окружения не сработал")
	}
}
