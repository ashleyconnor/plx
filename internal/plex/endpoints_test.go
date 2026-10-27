package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type seen struct {
	method, path, query string
	header              http.Header
}

// serve answers every request with body (status 200) and records them.
func serve(t *testing.T, body string) (*Client, func() []seen) {
	t.Helper()
	var mu sync.Mutex
	var reqs []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone()})
		mu.Unlock()
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "tok", Options{ClientID: "dev-1"}), func() []seen {
		mu.Lock()
		defer mu.Unlock()
		return append([]seen(nil), reqs...)
	}
}

var bg = context.Background()

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"abc-_.~XYZ019": "abc-_.~XYZ019",
		"a b":           "a%20b",
		"a/b:c":         "a%2Fb%3Ac",
		"100%":          "100%25",
		"é":             "%C3%A9",
		"a,b+c":         "a%2Cb%2Bc",
		"":              "",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParamsKeepOrderAndKeysVerbatim(t *testing.T) {
	if (Params{}).Encode() != "" {
		t.Error("empty params must encode to nothing")
	}
	p := Params{}.Add("z", "1").Add("label[0].tag.tag", "a b").Add("a", "")
	if got, want := p.Encode(), "?z=1&label[0].tag.tag=a%20b&a="; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestNewDefaultsSchemeAndTrimsSlash(t *testing.T) {
	c := New("plex.example:32400/", "t", Options{})
	if c.base != "https://plex.example:32400" || c.clientID != "plx" {
		t.Errorf("base %q clientID %q", c.base, c.clientID)
	}
	if c.hc.Timeout != 120*time.Second {
		t.Errorf("timeout = %v", c.hc.Timeout)
	}
	c = New("http://h:1", "t", Options{Timeout: time.Second, ClientID: "x"})
	if c.base != "http://h:1" || c.hc.Timeout != time.Second || c.clientID != "x" {
		t.Errorf("client = %+v", c)
	}
}

func TestEveryRequestIdentifiesPlx(t *testing.T) {
	c, got := serve(t, `{"MediaContainer":{"machineIdentifier":"M"}}`)
	id, err := c.Identity(bg)
	if err != nil || id != "M" {
		t.Fatal(id, err)
	}
	r := got()[0]
	if r.method != "GET" || r.path != "/identity" {
		t.Errorf("request = %+v", r)
	}
	for k, want := range map[string]string{"X-Plex-Token": "tok", "X-Plex-Client-Identifier": "dev-1",
		"X-Plex-Product": "plx", "Accept": "application/json"} {
		if r.header.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, r.header.Get(k), want)
		}
	}
}

func TestAPIErrorCarriesStatusAndTruncatedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, strings.Repeat("x", 2000))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "bad", Options{}).Identity(bg)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 || ae.Method != "GET" || ae.Path != "/identity" || len(ae.Body) != 512 {
		t.Fatalf("err = %v (%+v)", err, ae)
	}
	if !strings.HasPrefix(err.Error(), "plex: GET /identity: 401 xxx") {
		t.Errorf("message = %.60s", err.Error())
	}
}

func TestAllowsMediaDeletionDefaultsToFalse(t *testing.T) {
	for name, body := range map[string]string{
		"missing":    `{"MediaContainer":{"Setting":[{"id":"other","value":true}]}}`,
		"false":      `{"MediaContainer":{"Setting":[{"id":"allowMediaDeletion","value":false}]}}`,
		"not a bool": `{"MediaContainer":{"Setting":[{"id":"allowMediaDeletion","value":"1"}]}}`,
	} {
		c, _ := serve(t, body)
		if ok, err := c.AllowsMediaDeletion(bg); ok || err != nil {
			t.Errorf("%s: got %v, %v (deletion must be assumed forbidden)", name, ok, err)
		}
	}
}

func TestSectionsAndTags(t *testing.T) {
	c, got := serve(t, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`)
	ds, err := c.Sections(bg)
	if err != nil || len(ds) != 1 || ds[0].Key != "1" || ds[0].Type != "movie" {
		t.Fatalf("%+v %v", ds, err)
	}
	if _, err := c.SectionTags(bg, "1", "label"); err != nil {
		t.Fatal(err)
	}
	rs := got()
	if rs[0].path != "/library/sections" || rs[1].path != "/library/sections/1/label" {
		t.Errorf("paths = %v %v", rs[0].path, rs[1].path)
	}
}

func TestSectionItemsFillsSectionFromContainer(t *testing.T) {
	c, got := serve(t, `{"MediaContainer":{"librarySectionTitle":"Movies","Metadata":[
		{"ratingKey":1,"title":"A"},{"ratingKey":2,"title":"B","librarySectionID":9,"librarySectionTitle":"Own"}]}}`)
	ms, err := c.SectionItems(bg, "4", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ms[0].LibrarySectionID != "4" || ms[0].LibrarySectionTitle != "Movies" || ms[1].LibrarySectionID != "9" {
		t.Errorf("sections = %v/%v, %v", ms[0].LibrarySectionID, ms[0].LibrarySectionTitle, ms[1].LibrarySectionID)
	}
	r := got()[0]
	if r.path != "/library/sections/4/all" || r.query != "type=1&X-Plex-Container-Start=0&X-Plex-Container-Size=1000" {
		t.Errorf("request = %s?%s", r.path, r.query)
	}
}

func TestSectionKeysWithTag(t *testing.T) {
	c, got := serve(t, `{"MediaContainer":{"Metadata":[{"ratingKey":5},{"ratingKey":"6"}]}}`)
	keys, err := c.SectionKeysWithTag(bg, "1", 4, "label", "77")
	if err != nil || strings.Join(keys, ",") != "5,6" {
		t.Fatalf("%v %v", keys, err)
	}
	if q := got()[0].query; !strings.HasPrefix(q, "type=4&label=77&") {
		t.Errorf("query = %s", q)
	}
}

func TestPagingFetchesUntilTotalSize(t *testing.T) {
	var mu sync.Mutex
	var starts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("X-Plex-Container-Start")
		mu.Lock()
		starts = append(starts, start)
		mu.Unlock()
		n := pageSize
		if start == "2000" {
			n = 5
		}
		var ms []string
		for i := 0; i < n; i++ {
			ms = append(ms, fmt.Sprintf(`{"ratingKey":%d}`, i))
		}
		fmt.Fprintf(w, `{"MediaContainer":{"totalSize":2005,"Metadata":[%s]}}`, strings.Join(ms, ","))
	}))
	defer srv.Close()
	ms, err := New(srv.URL, "t", Options{}).PlaylistItems(bg, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2005 || strings.Join(starts, ",") != "0,1000,2000" {
		t.Errorf("got %d items from pages %v", len(ms), starts)
	}
}

func TestPagingStopsOnEmptyPageOrMissingTotal(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"MediaContainer":{"totalSize":50,"Metadata":[]}}`)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "t", Options{}).CollectionItems(bg, "1"); err != nil || calls != 1 {
		t.Errorf("calls = %d, err = %v", calls, err)
	}
	c, got := serve(t, `{"MediaContainer":{"Metadata":[{"ratingKey":1}]}}`) // no totalSize
	if ms, _ := c.CollectionItems(bg, "9"); len(ms) != 1 || len(got()) != 1 {
		t.Error("endpoints that ignore paging must not be re-requested")
	}
	if got()[0].path != "/library/collections/9/children" {
		t.Errorf("path = %s", got()[0].path)
	}
}

func TestPlaylistsAndCollectionsDecode(t *testing.T) {
	c, got := serve(t, `{"MediaContainer":{"Metadata":[
		{"ratingKey":"7","title":"P","playlistType":"video","smart":"1","leafCount":"3","childCount":2,"subtype":"movie"}]}}`)
	pls, err := c.Playlists(bg)
	if err != nil || !bool(pls[0].Smart) || pls[0].LeafCount != 3 || pls[0].PlaylistType != "video" {
		t.Fatalf("%+v %v", pls, err)
	}
	cols, err := c.Collections(bg, "2")
	if err != nil || cols[0].LibrarySectionID != "2" || cols[0].Subtype != "movie" {
		t.Fatalf("%+v %v", cols, err)
	}
	if rs := got(); rs[0].path != "/playlists" || rs[1].path != "/library/sections/2/collections" {
		t.Errorf("paths = %s %s", rs[0].path, rs[1].path)
	}
}

func TestMetadataNotFound(t *testing.T) {
	c, _ := serve(t, `{"MediaContainer":{"Metadata":[]}}`)
	if _, err := c.Metadata(bg, "5"); err == nil || !strings.Contains(err.Error(), "5 not found") {
		t.Errorf("err = %v", err)
	}
}

func TestImageQuery(t *testing.T) {
	c, got := serve(t, "PNGDATA")
	b, err := c.Image(bg, "/library/metadata/1/thumb/2", 100, 150)
	if err != nil || string(b) != "PNGDATA" {
		t.Fatalf("%q %v", b, err)
	}
	r := got()[0]
	want := "width=100&height=150&minSize=1&upscale=1&url=%2Flibrary%2Fmetadata%2F1%2Fthumb%2F2"
	if r.path != "/photo/:/transcode" || r.query != want {
		t.Errorf("request = %s?%s", r.path, r.query)
	}
}

func TestEmptyBodyIsNotADecodeError(t *testing.T) {
	c, _ := serve(t, "")
	if ids, err := c.Identity(bg); err != nil || ids != "" {
		t.Errorf("%q %v", ids, err)
	}
}

func TestCreateRejectsEmptyAndMissingResult(t *testing.T) {
	c, got := serve(t, `{"MediaContainer":{"Metadata":[]}}`)
	if _, err := c.CreatePlaylist(bg, "m", "t", "video", nil); err == nil {
		t.Error("empty playlist accepted")
	}
	if _, err := c.CreateCollection(bg, "m", "1", "t", 1, nil); err == nil {
		t.Error("empty collection accepted")
	}
	if len(got()) != 0 {
		t.Error("requests sent for an empty create")
	}
	if _, err := c.CreatePlaylist(bg, "m", "t", "video", []string{"1"}); err == nil || !strings.Contains(err.Error(), "no playlist") {
		t.Errorf("err = %v", err)
	}
	if _, err := c.CreateCollection(bg, "m", "1", "t", 1, []string{"1"}); err == nil || !strings.Contains(err.Error(), "no collection") {
		t.Errorf("err = %v", err)
	}
}

func TestCreateIsNeverRetried(t *testing.T) {
	c, calls := flaky(t, 5, 500, 200)
	if _, err := c.CreatePlaylist(bg, "m", "t", "video", []string{"1"}); err == nil {
		t.Fatal("want error")
	}
	if _, err := c.CreateCollection(bg, "m", "1", "t", 1, []string{"1"}); err == nil {
		t.Fatal("want error")
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("calls = %d, want 1 per create", n)
	}
}

func TestChunks(t *testing.T) {
	mk := func(n int) []string {
		s := make([]string, n)
		for i := range s {
			s[i] = fmt.Sprint(i)
		}
		return s
	}
	for n, want := range map[int][]int{0: nil, 1: {1}, 100: {100}, 101: {100, 1}, 250: {100, 100, 50}} {
		var got []int
		for _, c := range chunks(mk(n)) {
			got = append(got, len(c))
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("chunks(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestDeleteEndpointsAndSort(t *testing.T) {
	c, got := serve(t, "")
	c.DeleteItem(bg, "1")
	c.DeleteMedia(bg, "1", "2")
	c.DeletePlaylist(bg, "3")
	c.SetCollectionSort(bg, "4", CollectionSortAlpha)
	c.RemoveFromCollection(bg, "5", "6")
	c.MoveCollectionItem(bg, "5", "6", "")
	c.RenamePlaylist(bg, "3", "Road trip")
	c.DeleteCollection(bg, "5")
	c.RenameCollection(bg, "1", "5", "Sci-fi")
	var lines []string
	for _, r := range got() {
		lines = append(lines, r.method+" "+r.path+"?"+r.query)
	}
	want := []string{
		"DELETE /library/metadata/1?",
		"DELETE /library/metadata/1/media/2?",
		"DELETE /playlists/3?",
		"PUT /library/metadata/4/prefs?collectionSort=1",
		"DELETE /library/collections/5/items/6?",
		"PUT /library/collections/5/items/6/move?",
		"PUT /playlists/3?title=Road%20trip",
		"DELETE /library/collections/5?",
		"PUT /library/sections/1/all?type=18&id=5&title.value=Sci-fi&title.locked=1",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestFlexTypes(t *testing.T) {
	var m Metadata
	js := `{"ratingKey":12,"index":"3","year":2020.0,"viewCount":null,"smart":"true","rating":"7.5","audienceRating":null,
		"parentIndex":"","duration":[1],"title":"T","playlistItemID":"900","librarySectionID":{"x":1}}`
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	if m.RatingKey != "12" || m.Index != 3 || m.Year != 2020 || m.ViewCount != 0 || !bool(m.Smart) ||
		m.Rating != 7.5 || m.AudienceRating != 0 || m.ParentIndex != 0 || m.Duration != 0 ||
		m.PlaylistItemID != "900" || m.LibrarySectionID != "" {
		t.Errorf("decoded %+v", m)
	}
	for in, want := range map[string]bool{`1`: true, `"1"`: true, `true`: true, `"true"`: true, `0`: false, `false`: false, `null`: false, `"no"`: false} {
		var b FlexBool
		json.Unmarshal([]byte(in), &b)
		if bool(b) != want {
			t.Errorf("FlexBool(%s) = %v", in, bool(b))
		}
	}
	var n FlexInt
	if err := json.Unmarshal([]byte(`"abc"`), &n); err == nil {
		t.Error("garbage int accepted")
	}
	var f FlexFloat
	if err := json.Unmarshal([]byte(`"abc"`), &f); err == nil {
		t.Error("garbage float accepted")
	}
}

func TestResourceIsServer(t *testing.T) {
	for provides, want := range map[string]bool{"server": true, "client,player": false, "player,server": true, "": false, "servers": false} {
		if got := (Resource{Provides: provides}).IsServer(); got != want {
			t.Errorf("%q: %v", provides, got)
		}
	}
	if u := (&Pin{Code: "AB12"}).LinkURL(); u != "https://www.plex.tv/link/?pin=AB12" {
		t.Errorf("link = %s", u)
	}
}

func TestConnectPrefersLocalThenRemote(t *testing.T) {
	ok := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"M"}}`)
		}))
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	remote, local, relay := ok(), ok(), ok()
	defer bad.Close()
	defer remote.Close()
	defer local.Close()
	defer relay.Close()
	d := Device{ID: "x"}
	res := Resource{Name: "S", AccessToken: "t", Connections: []Connection{
		{URI: relay.URL, Relay: true}, {URI: bad.URL, Local: true}, {URI: remote.URL}, {URI: local.URL, Local: true},
	}}
	got, err := d.Connect(bg, res)
	if err != nil || got != local.URL {
		t.Fatalf("got %q, %v; want local %q", got, err, local.URL)
	}
	res.Connections = []Connection{{URI: relay.URL, Relay: true}, {URI: remote.URL}, {URI: bad.URL, Local: true}}
	if got, err := d.Connect(bg, res); err != nil || got != remote.URL {
		t.Errorf("got %q, %v; want remote over relay", got, err)
	}
	res.Connections = []Connection{{URI: bad.URL, Local: true}}
	if _, err := d.Connect(bg, res); err == nil || !strings.Contains(err.Error(), "couldn't reach S") {
		t.Errorf("err = %v", err)
	}
}
