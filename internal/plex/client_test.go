package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func flaky(t *testing.T, failures int32, failStatus, thenStatus int) (*Client, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= failures {
			w.WriteHeader(failStatus)
			return
		}
		w.WriteHeader(thenStatus)
	}))
	t.Cleanup(srv.Close)
	old := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = old })
	return New(srv.URL, "tok", Options{}), &calls
}

func TestMoveRetriesTransient500(t *testing.T) {
	c, calls := flaky(t, 2, 500, 200)
	if err := c.MovePlaylistItem(context.Background(), "1", "2", "3"); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestAddToPlaylistIsNotRetried(t *testing.T) {
	c, calls := flaky(t, 1, 500, 200)
	if err := c.AddToPlaylist(context.Background(), "m", "1", []string{"5"}); err == nil {
		t.Fatal("want error")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	c, calls := flaky(t, 10, 400, 200)
	if err := c.MovePlaylistItem(context.Background(), "1", "2", ""); err == nil {
		t.Fatal("want error")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestRetriedDeleteTreats404AsDone(t *testing.T) {
	c, _ := flaky(t, 1, 500, 404)
	if err := c.RemoveFromPlaylist(context.Background(), "1", "2"); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestEditTagsEncoding(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", Options{})
	ctx := context.Background()
	if err := c.EditTags(ctx, "1", 1, []string{"10", "11"}, "label", []string{"to keep"}, false); err != nil {
		t.Fatal(err)
	}
	if want := "type=1&id=10%2C11&label.locked=1&label[0].tag.tag=to%20keep"; got != want {
		t.Errorf("add query = %s\nwant        %s", got, want)
	}
	if err := c.EditTags(ctx, "1", 1, []string{"10"}, "label", []string{"to keep"}, true); err != nil {
		t.Fatal(err)
	}
	if want := "type=1&id=10&label.locked=1&label[].tag.tag-=to%2520keep"; got != want {
		t.Errorf("remove query = %s\nwant           %s", got, want)
	}
}

func TestAllowsMediaDeletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"MediaContainer":{"Setting":[{"id":"other","value":"x"},{"id":"allowMediaDeletion","value":true}]}}`))
	}))
	defer srv.Close()
	ok, err := New(srv.URL, "tok", Options{}).AllowsMediaDeletion(context.Background())
	if err != nil || !ok {
		t.Fatalf("got %v, %v", ok, err)
	}
}
