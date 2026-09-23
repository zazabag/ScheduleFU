package media

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeWav пишет WAV 16 кГц моно, как его отдаёт ffmpeg: 44 байта
// заголовка и отсчёты int16.
func writeWav(t *testing.T, samples []int16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.wav")
	buf := make([]byte, 44+2*len(samples))
	copy(buf, "RIFF")
	for i, v := range samples {
		binary.LittleEndian.PutUint16(buf[44+2*i:], uint16(v))
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// По пику громкости отличаем «микрофон писал тишину» от «речь не
// распознана»: это разные беды с разными советами человеку.
func TestPikGromkostiWav(t *testing.T) {
	sine := make([]int16, 16000)
	for i := range sine {
		sine[i] = int16(16384 * math.Sin(float64(i)*2*math.Pi*440/16000)) // половина шкалы
	}
	peak, err := wavPeakDB(writeWav(t, sine))
	if err != nil || peak < -6.5 || peak > -5.5 {
		t.Errorf("синус в половину шкалы: %.1f дБ (%v), ожидалось около −6", peak, err)
	}
	silence, _ := wavPeakDB(writeWav(t, make([]int16, 16000)))
	if silence > -90 {
		t.Errorf("тишина: %.1f дБ", silence)
	}
	if _, err := wavPeakDB(writeWav(t, nil)); err != nil {
		t.Errorf("пустой файл: %v", err)
	}
}
