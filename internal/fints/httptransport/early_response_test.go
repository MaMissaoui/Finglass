package httptransport

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A server that answers 500 before reading a large body must not race with
// the deferred wipe of the base64 buffer (run under -race).
func TestExchangeEarlyErrorResponseDoesNotRaceWithWipe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	transport := newTestTransport(t, server)
	request := bytes.Repeat([]byte("A"), 1<<20)
	for attempt := 0; attempt < 5; attempt++ {
		_, err := transport.Exchange(context.Background(), request)
		var status *StatusError
		if err != nil && errors.As(err, &status) && status.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", status.Code)
		}
		if err == nil {
			t.Fatal("Exchange accepted a 500 response")
		}
	}
}
