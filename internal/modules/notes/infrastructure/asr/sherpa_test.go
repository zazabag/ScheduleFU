package asr

import "testing"

// Настоящий вывод sherpa-onnx v1.13.8 с моделью GigaAM v3, снятый руками
// 22.09.2026. Двоеточие после времени сначала не учли, и таймкоды уезжали
// прямо в текст конспекта.
func TestNastoyashchiyVyvodSherpaOnnx(t *testing.T) {
	out := `0.102 -- 8.556: ничьих не требуя похвал счастлив уж я надеждой сладкой
9.254 -- 11.264: у лукоморья дуб зеленый`

	segs := Parse(out)
	if len(segs) != 2 {
		t.Fatalf("разобрано %d отрезков вместо двух: %+v", len(segs), segs)
	}
	if segs[0].Text != "ничьих не требуя похвал счастлив уж я надеждой сладкой" {
		t.Errorf("в текст попало лишнее: %q", segs[0].Text)
	}
	if segs[1].Start.Seconds() != 9.254 || segs[1].End.Seconds() != 11.264 {
		t.Errorf("время разобрано неверно: %v — %v", segs[1].Start, segs[1].End)
	}
}

// Сборки sherpa-onnx печатают результат по-разному, и обновление бинарника
// не должно молча превращать конспект в пустую страницу.
func TestRazborVyvodaTremyaSposobami(t *testing.T) {
	out := `Loading model from ./encoder.int8.onnx
{"start": 0.0, "end": 5.12, "text": "сегодня разберём реформы Петра"}
5.200 -- 9.800 второй кусок распознан
просто строка без времени
Elapsed seconds: 41.2
Real time factor (RTF): 0.008`

	segs := Parse(out)
	if len(segs) != 3 {
		t.Fatalf("разобрано %d кусков вместо трёх: %+v", len(segs), segs)
	}
	if segs[0].Text != "сегодня разберём реформы Петра" {
		t.Errorf("JSON-строка: %q", segs[0].Text)
	}
	if segs[1].Text != "второй кусок распознан" || segs[1].Start.Seconds() != 5.2 {
		t.Errorf("строка со временем: %+v", segs[1])
	}
	if segs[2].Text != "просто строка без времени" {
		t.Errorf("голая строка: %q", segs[2].Text)
	}
}

// Строки лога идут вперемешку с результатом, и «Elapsed seconds» в
// конспекте выглядит как слова преподавателя.
func TestStrokiLogaVKonspektNePopadayut(t *testing.T) {
	for _, line := range []string{"Elapsed seconds: 41.2", "num threads: 4", "Wave duration: 5400 s", "Done!"} {
		if got := Parse(line); len(got) != 0 {
			t.Errorf("строка лога %q принята за речь: %+v", line, got)
		}
	}
}
