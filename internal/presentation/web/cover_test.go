package web

import (
	"strings"
	"testing"
)

// Обложка пары собирается из названия: одна и та же дисциплина всегда
// выглядит одинаково — иначе при каждой загрузке пара «меняла бы альбом».
func TestObloshkaOdnaIDlyaOdnoyDiscipliny(t *testing.T) {
	a := lessonCover("Эконометрика", "Лекция")
	b := lessonCover("Эконометрика", "Лекция")
	if a != b {
		t.Fatal("обложка одной дисциплины разная от вызова к вызову")
	}
	if lessonCover("Эконометрика", "Лекция") == lessonCover("Эконометрика", "Семинар") {
		t.Error("лекция и семинар должны отличаться меткой Live/Studio")
	}
}

// Знак выбирается по ключевому слову, и «эконометрика» — не «экономика»:
// порядок правил важен.
func TestObloshkaZnakPoKlyuchevomuSlovu(t *testing.T) {
	cases := map[string]string{
		"Эконометрика":   "econometrics",
		"Макроэкономика": "economics",
		"Иностранный язык в профессиональной сфере":             "language",
		"Гражданское право":                                     "law",
		"Бухгалтерский учет":                                    "accounting",
		"Элективные дисциплины по физической культуре и спорту": "sport",
		"Теория вероятностей и математическая статистика":       "statistics",
		"Что-то совсем новое":                                   "",
	}
	for name, want := range cases {
		if got := motifFor(name).id; got != want {
			t.Errorf("%q: знак %q, ждали %q", name, got, want)
		}
	}
}

// Название дисциплины — данные источника, недоверенные. В SVG обложки оно
// не попадает вовсе: знак и цвет выводятся из него, а текста в разметке нет.
func TestObloshkaNeNesetTekstIstochnika(t *testing.T) {
	evil := `<script>alert(1)</script>"&`
	got := string(lessonCover(evil, evil))
	for _, bad := range []string{"<script", "alert", `"&`} {
		if strings.Contains(got, bad) {
			t.Errorf("в обложке оказался текст источника: %q", bad)
		}
	}
}

// Зачёт и экзамен — своя обложка, какой бы ни была дисциплина: на экране
// важнее, что это контроль, а не предмет.
func TestObloshkaKontrolyaSvoya(t *testing.T) {
	a := lessonCover("Гражданское право", "Зачёт")
	b := lessonCover("Философия", "Зачёт")
	if a != b {
		t.Error("у зачёта должна быть одна обложка для всех дисциплин")
	}
	if !strings.Contains(string(lessonCover("Философия", "Экзамен")), "ЭКЗАМЕН") {
		t.Error("на обложке экзамена нет слова «экзамен»")
	}
}
