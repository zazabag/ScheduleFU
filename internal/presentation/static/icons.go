package static

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// Цвета иконки повторяют интерфейс: тёмная база и лаймовый акцент.
var (
	iconBG     = color.NRGBA{0x0f, 0x11, 0x15, 0xff}
	iconAccent = color.NRGBA{0xc8, 0xfa, 0x5f, 0xff}
	iconBusy   = color.NRGBA{0x3a, 0x2f, 0x24, 0xff}
	iconPast   = color.NRGBA{0x2a, 0x30, 0x38, 0xff}
)

// writeIcons рисует иконки приложения.
//
// Мотив — та же полоса занятости, что и на главном экране: три прошедшие
// клетки, занятая и свободные. На маленьком размере читается как штрих-код,
// но это единственный элемент интерфейса, который ни на что не похож у
// других расписаний, и им приложение узнаётся среди иконок на экране.
func writeIcons(out string) error {
	for _, size := range []int{192, 512} {
		if err := writeIcon(filepath.Join(out, iconName(size)), size, false); err != nil {
			return err
		}
	}
	// Maskable-иконка: Android обрезает её по своей форме, поэтому рисунок
	// ужимается к центру, чтобы не потерять края под круглой маской.
	return writeIcon(filepath.Join(out, "icon-maskable-512.png"), 512, true)
}

func iconName(size int) string {
	if size == 192 {
		return "icon-192.png"
	}
	return "icon-512.png"
}

func writeIcon(path string, size int, maskable bool) error {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fill(img, image.Rect(0, 0, size, size), iconBG)

	// Доля картинки, занятая рисунком: под маску оставляем безопасное поле.
	inset := 0.16
	if maskable {
		inset = 0.28
	}
	left := int(float64(size) * inset)
	right := size - left
	width := right - left

	const cells = 6
	gap := width / 24
	cellW := (width - gap*(cells-1)) / cells
	cellH := int(float64(size) * 0.30)
	top := (size - cellH) / 2

	states := []color.NRGBA{iconPast, iconPast, iconBusy, iconAccent, iconAccent, iconAccent}
	for i := 0; i < cells; i++ {
		x := left + i*(cellW+gap)
		fill(img, image.Rect(x, top, x+cellW, top+cellH), states[i])
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func fill(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}
