// Package asr — распознавание речи на своём сервере.
//
// Реализация порта notes.Recognizer поверх sherpa-onnx (C++, Apache-2.0) с
// моделью GigaAM v3 (MIT, 240M параметров, русский). Выбор объяснён в
// docs/07-notes-module.md § «Распознавание»; коротко: русский у GigaAM
// заметно лучше, чем у Whisper, лицензии позволяют, а главное — аудио не
// уезжает с нашего сервера, и платить за минуты не нужно.
//
// Запуск — внешним процессом, а не через cgo. Это сознательно: связывать
// Go-бинарник с onnxruntime означает тянуть сборку C++ в деплой ради
// вызова, который и так длится минуты. Процесс отвечает тем же, чем
// отвечал бы вызов, и падает отдельно от сервера.
package asr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

// Options — чем и как распознавать.
type Options struct {
	// Command — бинарник sherpa-onnx с разбиением по тишине.
	Command string
	// ModelDir — каталог с моделью: encoder/decoder/joiner, tokens.txt и
	// silero_vad.onnx рядом.
	ModelDir string
	// Threads — сколько ядер отдать. Ноль означает «решает бинарник».
	Threads int
	// Args задаёт свою командную строку вместо собранной по умолчанию.
	// Подстановки: {wav}, {models}, {threads}. Нужны, потому что имена
	// файлов модели и флаги у разных сборок расходятся, и менять их лучше
	// в конфиге, чем в коде.
	Args []string
	// Timeout — потолок на один прогон. Полтора часа звука на четырёх
	// ядрах — это минуты, но зависший процесс не должен держать очередь.
	Timeout time.Duration
}

// Sherpa — распознаватель на внешнем бинарнике.
type Sherpa struct{ opts Options }

// New собирает распознаватель.
func New(opts Options) *Sherpa {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Hour
	}
	return &Sherpa{opts: opts}
}

// Available проверяет, что бинарник на месте.
func (s *Sherpa) Available() error {
	if strings.TrimSpace(s.opts.Command) == "" {
		return fmt.Errorf("asr: не задан бинарник распознавания")
	}
	if _, err := exec.LookPath(s.opts.Command); err != nil {
		return fmt.Errorf("asr: не найден %s: %w", s.opts.Command, err)
	}
	return nil
}

// defaultArgs — командная строка для GigaAM v3 (nemo transducer) с
// нарезкой по тишине: без неё полтора часа звука уходят в одну свёртку и
// съедают память.
func (s *Sherpa) defaultArgs() []string {
	dir := strings.TrimRight(s.opts.ModelDir, "/\\")
	return []string{
		"--silero-vad-model=" + dir + "/silero_vad.onnx",
		// Квантован только энкодер — он и весит 225 МБ из 230; декодер и
		// джойнер в модели лежат обычные, без int8.
		"--encoder=" + dir + "/encoder.int8.onnx",
		"--decoder=" + dir + "/decoder.onnx",
		"--joiner=" + dir + "/joiner.onnx",
		"--tokens=" + dir + "/tokens.txt",
		"--model-type=nemo_transducer",
		"--num-threads={threads}",
		"{wav}",
	}
}

// Transcribe прогоняет файл через распознаватель.
func (s *Sherpa) Transcribe(ctx context.Context, wavPath string) ([]domain.Segment, error) {
	args := s.opts.Args
	if len(args) == 0 {
		args = s.defaultArgs()
	}
	threads := s.opts.Threads
	if threads <= 0 {
		threads = 4
	}
	rep := strings.NewReplacer("{wav}", wavPath, "{models}", s.opts.ModelDir, "{threads}", strconv.Itoa(threads))
	final := make([]string, 0, len(args))
	for _, a := range args {
		final = append(final, rep.Replace(a))
	}

	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()

	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, s.opts.Command, final...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("asr: %s", lastLine(msg))
	}
	segs := Parse(out.String())
	if len(segs) == 0 {
		// Бинарник некоторых сборок пишет результат в stderr вперемешку с
		// логом; это не ошибка, а его манера.
		segs = Parse(errb.String())
	}
	return segs, nil
}

// Parse разбирает вывод распознавателя.
//
// Формат намеренно не один: сборки sherpa-onnx печатают то JSON на строку,
// то «0.000 -- 5.120 текст», то просто текст. Разбор терпимый — иначе
// обновление бинарника молча превращает конспект в пустую страницу.
func Parse(out string) []domain.Segment {
	var segs []domain.Segment
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if seg, ok := parseJSON(line); ok {
			segs = append(segs, seg)
			continue
		}
		if seg, ok := parseTimed(line); ok {
			segs = append(segs, seg)
			continue
		}
		if isNoise(line) {
			continue
		}
		segs = append(segs, domain.Segment{Text: line})
	}
	return segs
}

type jsonSegment struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func parseJSON(line string) (domain.Segment, bool) {
	if !strings.HasPrefix(line, "{") {
		return domain.Segment{}, false
	}
	var js jsonSegment
	if err := json.Unmarshal([]byte(line), &js); err != nil || strings.TrimSpace(js.Text) == "" {
		return domain.Segment{}, false
	}
	return domain.Segment{
		Start: time.Duration(js.Start * float64(time.Second)),
		End:   time.Duration(js.End * float64(time.Second)),
		Text:  strings.TrimSpace(js.Text),
	}, true
}

// parseTimed разбирает строку со временем.
//
// sherpa-onnx 1.13 печатает «0.102 -- 8.556: текст» — с двоеточием после
// конца отрезка. Двоеточие здесь не украшение: без его отсечения время не
// разбирается как число, строка уходит в запасную ветку целиком, и
// таймкоды оказываются внутри конспекта. Другие сборки печатают то же
// самое без двоеточия, поэтому оно необязательное.
func parseTimed(line string) (domain.Segment, bool) {
	i := strings.Index(line, "--")
	if i <= 0 {
		return domain.Segment{}, false
	}
	start, err := strconv.ParseFloat(strings.TrimSpace(line[:i]), 64)
	if err != nil {
		return domain.Segment{}, false
	}
	rest := strings.TrimSpace(line[i+2:])
	j := strings.IndexFunc(rest, func(r rune) bool { return r == ' ' || r == '\t' })
	if j < 0 {
		return domain.Segment{}, false
	}
	end, err := strconv.ParseFloat(strings.TrimRight(strings.TrimSpace(rest[:j]), ":"), 64)
	if err != nil {
		return domain.Segment{}, false
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[j:]), ":"))
	if text == "" {
		return domain.Segment{}, false
	}
	return domain.Segment{
		Start: time.Duration(start * float64(time.Second)),
		End:   time.Duration(end * float64(time.Second)),
		Text:  text,
	}, true
}

// isNoise отсеивает строки лога, которые бинарник печатает рядом с
// результатом: без этого «Elapsed seconds: 41.2» попадает в конспект.
func isNoise(line string) bool {
	lower := strings.ToLower(line)
	for _, p := range []string{"elapsed", "real time factor", "rtf", "num threads", "loading", "wave duration", "started", "done!", "/sherpa-onnx"} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return !strings.ContainsFunc(line, func(r rune) bool { return r >= 'а' && r <= 'я' || r >= 'А' && r <= 'Я' || r == 'ё' || r == 'Ё' })
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
