package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// Ключ владельца пользовательских данных (конспектов и заданий) живёт
// здесь, а не в web: его читают оба транспорта — страницы и JSON, — а
// транспорт транспорт не импортирует (ARCHITECTURE.md § 6).

// ownerCookie — ключ владельца конспектов.
//
// Аккаунтов в проекте нет (ARCHITECTURE.md § 0), но конспект обязан кому-то
// принадлежать, иначе его увидит любой. Ключ — случайные 16 байт, выданные
// устройству: ни почты, ни имени, ни пароля, и никакой возможности узнать
// по нему человека. Цена решения честная: конспекты не переезжают на другое
// устройство и теряются вместе с очисткой браузера — перенос по коду
// появится отдельным экраном, когда попросят (§ 9).
const ownerCookie = "schedulefu_owner"

// OwnerKey возвращает ключ владельца, заводя его при первом заходе.
//
// HttpOnly: скрипту раздела он не нужен — запросы идут со своего же
// источника, и браузер приложит cookie сам. SameSite=Lax закрывает чужие
// формы: без него посторонняя страница могла бы отправить POST и удалить
// конспект.
func OwnerKey(w http.ResponseWriter, r *http.Request, secure bool) string {
	if c, err := r.Cookie(ownerCookie); err == nil && validOwner(c.Value) {
		return c.Value
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Без случайности ключ выдавать нельзя: предсказуемый — это чужой.
		return ""
	}
	key := hex.EncodeToString(buf[:])
	http.SetCookie(w, &http.Cookie{Name: ownerCookie, Value: key, Path: "/",
		MaxAge: 365 * 24 * 60 * 60, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	return key
}

// ExistingOwnerKey читает ключ, не заводя нового: для запросов, которым
// нечего показывать новичку.
func ExistingOwnerKey(r *http.Request) string {
	c, err := r.Cookie(ownerCookie)
	if err != nil || !validOwner(c.Value) {
		return ""
	}
	return c.Value
}

// validOwner проверяет форму ключа. Значение приходит от браузера и может
// быть любым: в запрос к базе оно попадает как есть, поэтому форма строгая.
func validOwner(v string) bool {
	if len(v) != 32 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// IsSecure сообщает, пришёл ли запрос по HTTPS. За прокси — по заголовку,
// который тот проставляет; cookie с флагом Secure на http браузер не примет
// вовсе, и без этой проверки закрепление молча перестало бы сохраняться на
// локальном стенде.
func IsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
