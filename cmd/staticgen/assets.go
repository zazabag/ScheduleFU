package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zazabag/schedulefu/internal/web"
)

//go:embed site/*
var siteFS embed.FS

// writeAssets раскладывает страницу, скрипт и стили.
//
// Стили берутся из того же файла, что и у серверной версии: два набора
// разъехались бы после первой же правки.
func writeAssets(out string) error {
	entries, err := fs.ReadDir(siteFS, "site")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := siteFS.ReadFile("site/" + e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, e.Name()), body, 0o644); err != nil {
			return fmt.Errorf("запись %s: %w", e.Name(), err)
		}
	}

	css, err := web.StaticFS().ReadFile("static/style.css")
	if err != nil {
		return fmt.Errorf("стили: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, "style.css"), css, 0o644); err != nil {
		return err
	}

	if err := writeIcons(out); err != nil {
		return fmt.Errorf("иконки: %w", err)
	}

	// .nojekyll обязателен: без него Pages прогоняет сайт через Jekyll и
	// выкидывает файлы и папки, начинающиеся с подчёркивания.
	return os.WriteFile(filepath.Join(out, ".nojekyll"), nil, 0o644)
}
