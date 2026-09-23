// Package media — подготовка звука к распознаванию.
//
// Браузеры записывают разное: Chrome и Android отдают webm/opus, Safari —
// mp4/aac, а «загрузить запись» приносит вообще что угодно, вплоть до m4a
// с диктофона телефона. Распознаванию нужен один формат: WAV 16 кГц моно.
// Приводит к нему ffmpeg — внешний бинарник, а не библиотека: декодеров
// полудюжины форматов в стандартной библиотеке Go нет и не будет.
package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

// FFmpeg — декодер на внешнем бинарнике.
type FFmpeg struct {
	// Bin — путь к ffmpeg; пустой означает «искать в PATH».
	Bin string
}

// New создаёт декодер.
func New(bin string) FFmpeg {
	if strings.TrimSpace(bin) == "" {
		bin = "ffmpeg"
	}
	return FFmpeg{Bin: bin}
}

// Available сообщает, найден ли ffmpeg. Проверяется при запуске: узнать,
// что декодера нет, полутора часами позже, когда человек ждёт конспект, —
// худший момент.
func (f FFmpeg) Available() error {
	if _, err := exec.LookPath(f.Bin); err != nil {
		return fmt.Errorf("media: не найден ffmpeg (%s): %w", f.Bin, err)
	}
	return nil
}

// ToWav перегоняет запись в WAV 16 кГц моно и возвращает длительность и
// пик громкости.
func (f FFmpeg) ToWav(ctx context.Context, src, dst string) (domain.Sound, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, f.Bin,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", src,
		"-vn", // видеодорожки быть не должно, но webm с камеры присылали
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		dst)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		// Обрыв дозагрузки даёт битый контейнер, и это самая частая
		// причина сюда попасть — сообщение уходит человеку на экран.
		return domain.Sound{}, fmt.Errorf("не удалось прочитать запись: %s", firstLine(msg))
	}
	st, err := os.Stat(dst)
	if err != nil {
		return domain.Sound{}, fmt.Errorf("media: результат ffmpeg: %w", err)
	}
	peak, err := wavPeakDB(dst)
	if err != nil {
		return domain.Sound{}, err
	}
	// Длительность считается из размера, а не спрашивается у ffprobe: формат
	// фиксирован нами же — 16 000 кадров в секунду по два байта, плюс
	// 44 байта заголовка.
	const bytesPerSec = 16000 * 2
	size := st.Size() - 44
	if size < 0 {
		size = 0
	}
	return domain.Sound{DurationSec: int(size / bytesPerSec), PeakDB: peak}, nil
}

// wavPeakDB — пик WAV, записанного ffmpeg (44 байта заголовка, int16 моно),
// в дБ от полной шкалы. Считается в Go одним проходом по файлу: ffmpeg с
// volumedetect — ещё один запуск декодера ради одного числа.
func wavPeakDB(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("media: чтение WAV: %w", err)
	}
	defer f.Close()
	if _, err := f.Seek(44, io.SeekStart); err != nil {
		return 0, fmt.Errorf("media: чтение WAV: %w", err)
	}
	var peak int32
	buf := make([]byte, 64<<10)
	for {
		n, err := io.ReadFull(f, buf)
		for i := 0; i+1 < n; i += 2 {
			v := int32(int16(binary.LittleEndian.Uint16(buf[i:])))
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("media: чтение WAV: %w", err)
		}
	}
	if peak == 0 {
		return -120, nil // цифровой ноль: логарифма нет, тише некуда
	}
	return 20 * math.Log10(float64(peak)/32768), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
