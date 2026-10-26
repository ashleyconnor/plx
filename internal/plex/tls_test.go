package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A self-signed certificate on an IP URL is rejected unless insecure.
func TestIPCertificateMustBePlexDirect(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc"}}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := New(srv.URL, "", Options{}).Identity(ctx); err == nil {
		t.Fatal("self-signed certificate accepted")
	}
	if _, err := New(srv.URL, "", Options{Insecure: true}).Identity(ctx); err != nil {
		t.Fatalf("insecure: %v", err)
	}
}
