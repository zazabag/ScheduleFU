package httpx

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Сжатие страниц.
//
// Страница со списком аудиторий — это сотня почти одинаковых карточек:
// 173 КБ разметки, которые сжимаются до четырёх. Разница в сорок раз, и
// она решающая там, где сервисом будут пользоваться: в здании вуза, с
// телефона, на переполненной сети.

var gzipPool = sync.Pool{
	New: func() any {
		// Уровень по умолчанию: разница в размере с максимальным
		// невелика, а процессорного времени тот съедает заметно больше.
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz     *gzip.Writer
	wrote  bool
	status int
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = status
	h := w.Header()
	// Длина исходного тела после сжатия неверна, а сам факт сжатия
	// меняет представление ресурса — об этом нужно сказать кэшам.
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.gz.Write(p)
}

// Flush пробрасывает сброс буфера: без него потоковые ответы зависали бы
// в буфере сжатия до конца запроса.
func (w *gzipResponseWriter) Flush() {
	w.gz.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Compress сжимает ответы тем клиентам, которые об этом попросили.
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) || alreadyCompressed(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			gz.Close()
			gzipPool.Put(gz)
		}()

		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

// alreadyCompressed отсекает то, что сжимать бессмысленно: картинки и
// шрифты уже сжаты, и повторное сжатие только тратит время процессора.
func alreadyCompressed(path string) bool {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".woff", ".woff2", ".ico"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
