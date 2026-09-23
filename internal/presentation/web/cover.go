package web

import (
	"fmt"
	"hash/fnv"
	"html/template"
	"strings"
)

// Обложки пар для оформления «Плеер»: пара — трек, у трека есть обложка.
// Рисовать картинки для тысяч дисциплин вуза некому, поэтому обложка
// собирается из трёх слоёв: цвет — из хэша названия (одна дисциплина всегда
// одного цвета, соседние — разных), знак — по ключевому слову, метка — по
// виду занятия (Live — лекция, Studio — семинар). У зачёта и экзамена своя
// чёрная обложка: на экране важнее, что это контроль, а не предмет.
//
// Название приходит из источника и ему не доверяем: в разметку обложки оно
// не попадает ни одним символом, только решает, какой знак и цвет взять.
// Градиентов и id внутри нет — одна и та же обложка встречается на странице
// несколько раз (герой и список), и id столкнулись бы.

type palette struct{ bg, ink, shade string }

var coverPalettes = []palette{
	{"#6B4DF0", "#FFFFFF", "#8F78FF"},
	{"#E8613C", "#FFE8DC", "#F2825F"},
	{"#8E2A55", "#FFC2D9", "#6A1C3E"},
	{"#1558B0", "#CFE3FF", "#2E72C9"},
	{"#0F6E56", "#E1F5EE", "#1D8A6D"},
	{"#D0921F", "#412402", "#E0A63A"},
	{"#16123D", "#5DE0B0", "#2E2870"},
	{"#3F8F2A", "#EAF3DE", "#52A33B"},
	{"#C2385A", "#FFE3EA", "#D6547A"},
	{"#2B6F8F", "#DDF3FF", "#3C86A8"},
}

type motif struct {
	id    string
	words []string
	draw  func(p palette) string
}

func glyph(text string, size int) func(p palette) string {
	return func(p palette) string {
		return fmt.Sprintf(`<text x="150" y="%d" font-size="%d" fill="%s" text-anchor="middle" font-family="Unbounded, Inter, sans-serif" font-weight="700">%s</text>`,
			150+size*35/100, size, p.ink, text)
	}
}

// Порядок важен: первое совпадение побеждает, поэтому «эконометрика» стоит
// раньше «экономики», а физкультура — раньше «культуры речи».
var motifs = []motif{
	{"econometrics", []string{"эконометр"}, func(p palette) string {
		pts := [][2]int{{56, 226}, {80, 204}, {98, 216}, {114, 182}, {136, 190}, {150, 156}, {174, 164}, {192, 132}, {210, 140}, {232, 108}, {250, 116}}
		var b strings.Builder
		fmt.Fprintf(&b, `<path d="M40 240 L266 84" stroke="%s" stroke-width="8" stroke-linecap="round"/>`, "#FFD27A")
		for _, q := range pts {
			fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="7" fill="%s"/>`, q[0], q[1], p.ink)
		}
		return b.String()
	}},
	{"sport", []string{"физическ", "физкульт", "спорт"}, func(p palette) string {
		return fmt.Sprintf(`<g fill="none" stroke="%[1]s" stroke-width="6"><rect x="40" y="60" width="220" height="170" rx="8"/><path d="M150 60V230"/><circle cx="150" cy="145" r="34"/></g>`, p.ink)
	}},
	{"statistics", []string{"статист", "вероятн"}, glyph("σ", 170)},
	{"math", []string{"математ", "алгебр"}, glyph("∫", 190)},
	{"language", []string{"иностран", "английск", "немецк", "французск", "китайск"}, glyph("Aa", 140)},
	{"russian", []string{"русск", "речи"}, glyph("Ё", 170)},
	{"law", []string{"право", "правов", "юрид"}, glyph("§", 180)},
	{"accounting", []string{"бухгалт", "учет", "учёт", "аудит"}, func(p palette) string {
		return fmt.Sprintf(`<path d="M150 70V230M70 90H230" stroke="%[1]s" stroke-width="6"/>`, p.ink) +
			fmt.Sprintf(`<text x="105" y="176" font-size="46" fill="%[1]s" text-anchor="middle" font-family="Unbounded, Inter, sans-serif" font-weight="700">Дт</text><text x="195" y="176" font-size="46" fill="%[1]s" text-anchor="middle" font-family="Unbounded, Inter, sans-serif" font-weight="700">Кт</text>`, p.ink)
	}},
	{"tax", []string{"налог"}, glyph("НДС", 72)},
	{"economics", []string{"эконом"}, func(p palette) string {
		var b strings.Builder
		for i, h := range []int{60, 96, 80, 132, 170} {
			fill := p.ink
			if i == 4 {
				fill = "#FFD27A"
			}
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="32" height="%d" rx="4" fill="%s"/>`, 44+i*46, 240-h, h, fill)
		}
		return b.String()
	}},
	{"finance", []string{"финанс", "банк", "кредит", "денежн", "инвест"}, glyph("₽", 180)},
	{"it", []string{"программ", "информат", "алгоритм", "данных", "цифров"}, glyph("{ }", 110)},
	{"philosophy", []string{"философ"}, func(p palette) string {
		return fmt.Sprintf(`<circle cx="150" cy="140" r="92" fill="none" stroke="%[1]s" stroke-width="6"/>`, p.ink) + glyph("?", 130)(p)
	}},
	{"history", []string{"истори"}, glyph("XX", 110)},
	{"marketing", []string{"маркетинг", "реклам"}, glyph("%", 170)},
	{"management", []string{"менеджмент", "управлен"}, glyph("↗", 180)},
	{"psychology", []string{"психолог"}, glyph("Ψ", 170)},
	{"sociology", []string{"социолог", "политолог"}, glyph("∞", 170)},
}

func motifFor(discipline string) motif {
	d := strings.ToLower(discipline)
	for _, m := range motifs {
		for _, w := range m.words {
			if strings.Contains(d, w) {
				return m
			}
		}
	}
	return motif{}
}

func paletteFor(discipline string) palette {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(discipline))))
	return coverPalettes[h.Sum32()%uint32(len(coverPalettes))]
}

// tagFor — метка трека по виду занятия (shortKind): «Live» у лекции.
func tagFor(kind string) string {
	switch kind {
	case "Лекция":
		return "Live"
	case "Семинар":
		return "Studio"
	case "Лабораторная":
		return "Lab"
	case "Консультация":
		return "Q&amp;A"
	}
	return ""
}

func svgCover(body string) template.HTML {
	return template.HTML(`<svg class="cover" viewBox="0 0 300 300" preserveAspectRatio="xMidYMid slice" aria-hidden="true" focusable="false">` + body + `</svg>`)
}

func pill(label, bg, fg string, chars int) string {
	w := chars*12 + 28
	return fmt.Sprintf(`<rect x="20" y="20" width="%d" height="30" rx="15" fill="%s"/><text x="%d" y="41" font-size="16" fill="%s" text-anchor="middle" font-family="Inter, sans-serif" font-weight="700">%s</text>`,
		w, bg, 20+w/2, fg, label)
}

// lessonCover — обложка пары: kind — вид занятия после shortKind.
func lessonCover(discipline, kind string) template.HTML {
	if kind == "Зачёт" || kind == "Экзамен" {
		word := "ЗАЧЁТ"
		if kind == "Экзамен" {
			word = "ЭКЗАМЕН"
		}
		return svgCover(`<rect width="300" height="300" fill="#141418"/>` +
			fmt.Sprintf(`<text x="150" y="128" font-size="44" fill="#F1EFE8" text-anchor="middle" font-family="Unbounded, Inter, sans-serif" font-weight="700">%s</text>`, kind) +
			`<text x="150" y="160" font-size="15" fill="#9A988F" text-anchor="middle" font-family="Inter, sans-serif">финальный трек семестра</text>` +
			`<rect x="150" y="200" width="130" height="80" fill="#fff"/><rect x="150" y="222" width="130" height="36" fill="#141418"/>` +
			`<text x="215" y="216" font-size="12" fill="#141418" text-anchor="middle" font-family="Inter, sans-serif" font-weight="800">ОСТОРОЖНО</text>` +
			fmt.Sprintf(`<text x="215" y="247" font-size="%d" fill="#fff" text-anchor="middle" font-family="Unbounded, Inter, sans-serif" font-weight="700">%s</text>`, map[bool]int{true: 15, false: 18}[kind == "Экзамен"], word) +
			`<text x="215" y="273" font-size="11" fill="#141418" text-anchor="middle" font-family="Inter, sans-serif" font-weight="600">явное содержание</text>`)
	}
	p := paletteFor(discipline)
	m := motifFor(discipline)
	var b strings.Builder
	fmt.Fprintf(&b, `<rect width="300" height="300" fill="%s"/>`, p.bg)
	fmt.Fprintf(&b, `<circle cx="250" cy="50" r="130" fill="%s"/>`, p.shade)
	if m.draw != nil {
		b.WriteString(m.draw(p))
	} else {
		// Своего знака нет — сетка точек и кольцо: обложка всё равно
		// узнаётся по цвету.
		for i := 0; i < 9; i++ {
			for j := 0; j < 9; j++ {
				fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="3" fill="%s" opacity="0.45"/>`, 30+i*30, 30+j*30, p.ink)
			}
		}
		fmt.Fprintf(&b, `<circle cx="150" cy="150" r="70" fill="none" stroke="%s" stroke-width="10"/>`, p.ink)
	}
	if t := tagFor(kind); t != "" {
		n := len([]rune(strings.ReplaceAll(t, "&amp;", "&")))
		b.WriteString(pill(t, "rgba(0,0,0,0.38)", "#FFFFFF", n))
	}
	return svgCover(b.String())
}

// stateCover — обложки состояний дня, у которых нет своей пары: до пар
// («Intro», рассвет), перемена (реклама) и конец дня («Outro», ночь).
// Выходной рисует шаблон: там настоящий манул, а не SVG.
func stateCover(state string) template.HTML {
	switch state {
	case "before":
		var b strings.Builder
		b.WriteString(`<rect width="300" height="300" fill="#2B1B5E"/><rect y="90" width="300" height="80" fill="#6A2A6E"/><rect y="150" width="300" height="100" fill="#C4486B"/>`)
		b.WriteString(`<circle cx="150" cy="205" r="88" fill="#FFD27A"/>`)
		b.WriteString(`<rect y="200" width="300" height="5" fill="#E0697A"/><rect y="215" width="300" height="7" fill="#E0697A"/><rect y="231" width="300" height="9" fill="#E97A6E"/>`)
		b.WriteString(`<rect y="246" width="300" height="54" fill="#1A1240"/>`)
		for _, x := range []int{-150, -30, 60, 120, 180, 240, 330, 450} {
			fmt.Fprintf(&b, `<path d="M150 246 L%d 300" stroke="#5B3FD1" stroke-width="1.5"/>`, x)
		}
		b.WriteString(`<path d="M0 262 H300 M0 280 H300" stroke="#5B3FD1" stroke-width="1.5"/>`)
		b.WriteString(`<text x="26" y="62" font-size="38" fill="#FFF3E0" font-family="Unbounded, Inter, sans-serif" font-weight="700">Intro</text>`)
		b.WriteString(`<text x="28" y="88" font-size="15" fill="#FFD9C2" font-family="Inter, sans-serif">утренний эфир</text>`)
		return svgCover(b.String())
	case "between":
		// Перемена — это реклама. Шутка автора: вместо «перемены» — ролик
		// «спонсора». Логотип и фирменный шрифт чужой марки не берём —
		// только слово обычным шрифтом.
		return svgCover(`<rect width="300" height="300" fill="#2F86F0"/><rect y="170" width="300" height="130" fill="#1E5BD8"/>` +
			`<ellipse cx="70" cy="92" rx="50" ry="16" fill="#fff" opacity="0.85"/><ellipse cx="100" cy="82" rx="30" ry="18" fill="#fff" opacity="0.85"/>` +
			`<ellipse cx="232" cy="250" rx="56" ry="14" fill="#fff" opacity="0.5"/>` +
			`<path d="M24 150 Q140 20 262 70" stroke="#fff" stroke-width="3" stroke-dasharray="8 8" fill="none"/>` +
			`<path d="M254 56 L286 68 L256 82 L262 70 Z" fill="#fff"/>` +
			`<text x="150" y="196" font-size="38" fill="#fff" text-anchor="middle" font-family="Unbounded, Inter, sans-serif" font-weight="700">авиасейлс</text>` +
			`<text x="150" y="224" font-size="15" fill="#E6F1FF" text-anchor="middle" font-family="Inter, sans-serif">перемена — это почти отпуск</text>`)
	case "after":
		var b strings.Builder
		b.WriteString(`<rect width="300" height="300" fill="#141B45"/><rect y="150" width="300" height="150" fill="#2A2160"/>`)
		for i, s := range [][2]int{{30, 40}, {70, 96}, {120, 30}, {250, 50}, {272, 132}, {40, 150}, {200, 110}, {160, 70}} {
			fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%.1f" fill="#fff"/>`, s[0], s[1], 1.6+float64(i%3)*0.7)
		}
		b.WriteString(`<circle cx="206" cy="92" r="40" fill="#FFD27A"/><circle cx="224" cy="80" r="36" fill="#141B45"/>`)
		b.WriteString(`<path d="M0 300V230H40V200H78V240H110V180H150V226H186V196H226V244H262V214H300V300Z" fill="#070A1C"/>`)
		for _, w := range [][2]int{{50, 212}, {62, 226}, {120, 194}, {132, 210}, {196, 208}, {236, 224}, {274, 226}} {
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="6" height="8" fill="#FFD27A"/>`, w[0], w[1])
		}
		b.WriteString(`<text x="26" y="62" font-size="38" fill="#E6E0FF" font-family="Unbounded, Inter, sans-serif" font-weight="700">Outro</text>`)
		b.WriteString(`<text x="28" y="88" font-size="15" fill="#B7ACEF" font-family="Inter, sans-serif">плейлист окончен</text>`)
		return svgCover(b.String())
	}
	return ""
}
