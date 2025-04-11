package api

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

func recoverMiddleware(next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				slog.Error("recover middleware", "error", err, "trace", debug.Stack())
			}
		}()
		next(w, r)
	}
}

func authMiddleware(next func(http.ResponseWriter, *http.Request), token string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkToken(r, token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func get(next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeHttpError(w, http.StatusMethodNotAllowed, "only GET method is supported")
			return
		}
		next(w, r)
	}
}

func post(next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeHttpError(w, http.StatusMethodNotAllowed, "only POST method is supported")
			return
		}
		next(w, r)
	}
}

func checkToken(req *http.Request, token string) bool {
	auth := strings.Split(req.Header.Get("authorization"), " ")
	if len(auth) != 2 {
		return false
	}
	if auth[0] != "Bearer" {
		return false
	}
	if x := subtle.ConstantTimeCompare([]byte(auth[1]), []byte(token)); x == 1 {
		return true
	} // constant time comparison to prevent time attack
	return false
}

func writeHttpError(resp http.ResponseWriter, status int, comment string) {
	body := struct {
		Error string `json:"error"`
	}{
		Error: comment,
	}
	resp.Header().Add("Content-Type", "application/json")
	resp.WriteHeader(status)
	err := json.NewEncoder(resp).Encode(body)
	if err != nil {
		slog.Error("json encode", "error", err)
	}
}
