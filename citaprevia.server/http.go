package main

import (
	"net/http"
)

type loggedResponseWriter struct {
	w http.ResponseWriter

	status    int
	hasStatus bool
}

func (w *loggedResponseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *loggedResponseWriter) Write(b []byte) (int, error) {
	if !w.hasStatus {
		w.hasStatus = true
		w.status = http.StatusOK
	}
	return w.w.Write(b)
}

func (w *loggedResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.hasStatus = true
	w.w.WriteHeader(statusCode)
}

func (w *loggedResponseWriter) HeaderSent() (statusCode int, sent bool) {
	return w.status, w.hasStatus
}

func (w *loggedResponseWriter) Flush() {
	// TODO: Implement optionally.
	if f, ok := w.w.(http.Flusher); ok {
		f.Flush()
	}
}

var _ http.Flusher = (*loggedResponseWriter)(nil)

func (w *loggedResponseWriter) Status() int {
	if w.hasStatus {
		return w.status
	}
	return http.StatusOK
}

func onErr500(h func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		lw, ok := w.(interface {
			http.ResponseWriter
			HeaderSent() (statusCode int, ok bool)
		})
		if !ok {
			lw = &loggedResponseWriter{w: w}
		}

		var err error
		defer func() {
			r := recover()
			if err == nil && r == nil {
				return
			}
			if err != nil {
				log(req.Context()).Printf("Error on request err=%s", err)
			}
			if r != nil {
				log(req.Context()).Printf("Panic on request err=%s", r)
				defer panic(r)
			}

			_, ok = lw.HeaderSent()
			if ok {
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
		}()

		err = h(lw, req)
	})
}
