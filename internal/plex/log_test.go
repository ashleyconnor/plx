package plex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashleyconnor/plx/internal/logbuf"
)

func TestFailedRequestIsLoggedInFull(t *testing.T) {
	long := strings.Repeat("x", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("<html>bad request\n" + long + "</html>"))
	}))
	t.Cleanup(srv.Close)
	log := logbuf.New(10)
	c := New(srv.URL, "secret-token", Options{Log: log})

	err := c.do(context.Background(), http.MethodPut, "/library/sections/1/all", Params{}.Add("collection[0].tag.tag", "XXX Watched Unsorted"), nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("err = %v, want a 400 APIError", err)
	}
	if !strings.Contains(apiErr.Body, long) {
		t.Errorf("APIError.Body was truncated to %d bytes", len(apiErr.Body))
	}
	if len(err.Error()) > 400 {
		t.Errorf("Error() is %d chars; the status line should stay short", len(err.Error()))
	}

	es := log.Entries()
	if len(es) != 1 || es[0].Level != logbuf.Error {
		t.Fatalf("entries = %+v, want one error", es)
	}
	all := es[0].Summary + "\n" + strings.Join(es[0].Detail, "\n")
	for _, want := range []string{"PUT /library/sections/1/all → 400 Bad Request", "collection[0].tag.tag = XXX Watched Unsorted", "bad request", long} {
		if !strings.Contains(all, want) {
			t.Errorf("log lacks %q:\n%.300s", want, all)
		}
	}
	if strings.Contains(all, "secret-token") {
		t.Error("token leaked into the log")
	}
}

func TestSuccessfulRequestIsLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	log := logbuf.New(10)
	c := New(srv.URL, "tok", Options{Log: log})
	if err := c.do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatal(err)
	}
	es := log.Entries()
	if len(es) != 1 || es[0].Level != logbuf.Info || !strings.Contains(es[0].Summary, "GET /x → 200 OK") {
		t.Fatalf("entries = %+v", es)
	}
}
