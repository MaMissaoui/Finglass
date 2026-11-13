package httptransport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

func rootsFor(servers ...*httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, server := range servers {
		pool.AddCert(server.Certificate())
	}
	return pool
}

func newTestTransport(t *testing.T, server *httptest.Server, options ...Option) *Transport {
	t.Helper()
	return newTestTransportAt(t, server, "", options...)
}

func newTestTransportAt(t *testing.T, server *httptest.Server, path string, options ...Option) *Transport {
	t.Helper()
	all := make([]Option, 0, len(options)+2)
	all = append(all, WithRootCAs(rootsFor(server)), WithTimeout(5*time.Second))
	all = append(all, options...)
	transport, err := New(server.URL+path, all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return transport
}

// writeBase64Response answers with CRLF-wrapped base64, like a bank endpoint.
func writeBase64Response(t *testing.T, w http.ResponseWriter, payload []byte) {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(payload)
	var builder strings.Builder
	for start := 0; start < len(encoded); start += 8 {
		end := start + 8
		if end > len(encoded) {
			end = len(encoded)
		}
		builder.WriteString(encoded[start:end])
		builder.WriteString("\r\n")
	}
	if _, err := io.WriteString(w, builder.String()); err != nil {
		t.Errorf("write response: %v", err)
	}
}

// assertNoLeak reports when text contains one of the forbidden values. It
// never prints the values themselves, so a failing test cannot spill a secret.
func assertNoLeak(t *testing.T, label, text string, forbidden ...string) {
	t.Helper()
	for index, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(text, value) {
			t.Errorf("%s leaks forbidden value %d (output has %d bytes)", label, index, len(text))
		}
	}
}

func assertErrorDoesNotLeak(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		assertNoLeak(t, "error "+verb, fmt.Sprintf(verb, err), forbidden...)
	}
}

func TestExchangeRoundTrip(t *testing.T) {
	request := []byte("HNHBK:1:3+000000000001+1+PIN-4711-DISTINCTIVE-SECRET'")
	response := []byte("HNHBK:1:3+000000000002+1+response'")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if got := r.Header.Get("Content-Type"); got != "text/plain; charset=ISO-8859-1" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "*/*" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "Finglass" {
			t.Errorf("User-Agent = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		if bytes.ContainsAny(body, "\r\n \t") {
			t.Error("request body contains whitespace")
		}
		decoded, err := base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			t.Errorf("request body is not base64: %v", err)
			return
		}
		if !bytes.Equal(decoded, request) {
			t.Error("request body is not the base64 of the request")
		}
		writeBase64Response(t, w, response)
	}))
	defer server.Close()

	transport := newTestTransport(t, server)
	original := append([]byte(nil), request...)

	got, err := transport.Exchange(context.Background(), request)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("Exchange returned %d bytes, want %d", len(got), len(response))
	}
	if !bytes.Equal(request, original) {
		t.Fatal("Exchange modified the caller's request")
	}
}

func TestNewRefusesInvalidURL(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	plainHTTP := strings.Replace(server.URL, "https://", "http://", 1)
	cases := []struct {
		name string
		url  string
	}{
		{name: "plain http", url: plainHTTP + "/fints"},
		{name: "missing host", url: "https:///fints"},
		{name: "user info", url: "https://user:secret@127.0.0.1:1/fints"}, // #nosec G101 -- a deliberately invalid URL, never contacted.
		{name: "fragment", url: "https://127.0.0.1:1/fints#section"},
		{name: "empty fragment", url: "https://127.0.0.1:1/fints#"},
		{name: "garbage", url: ":// not a url"},
		{name: "empty", url: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			transport, err := New(testCase.url)
			if err == nil {
				t.Fatal("New accepted an unsafe server URL")
			}
			if transport != nil {
				t.Fatal("New returned a transport together with an error")
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("New made %d request(s)", got)
	}
}

func TestExchangeUntrustedCertificate(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeBase64Response(t, w, []byte("never read"))
	}))
	defer server.Close()

	// No WithRootCAs: the system roots do not know the test server's certificate.
	// The path and query are distinctive so a url.Error (which prints the full
	// URL) would be caught below.
	transport, err := New(server.URL+"/private-path?session=4711", WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = transport.Exchange(context.Background(), []byte("request"))
	if err == nil {
		t.Fatal("Exchange trusted an unknown certificate")
	}
	var verification *tls.CertificateVerificationError
	if !errors.As(err, &verification) {
		t.Fatalf("error is not a TLS certificate verification error: %T", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("handler ran %d time(s) after a failed handshake", got)
	}
	for _, text := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err)} {
		assertNoLeak(t, "TLS error", text, "/private-path", "session=4711")
	}
}

func TestExchangeRefusesRedirect(t *testing.T) {
	var landed atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		landed.Add(1)
		writeBase64Response(t, w, []byte("redirected"))
	}))
	defer target.Close()

	var redirectedTo atomic.Value
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		location := target.URL + "/landing?token=redirect-secret"
		redirectedTo.Store(location)
		http.Redirect(w, r, location, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	transport, err := New(source.URL+"/bank/private-path",
		WithRootCAs(rootsFor(source, target)), WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = transport.Exchange(context.Background(), []byte("PIN-4711"))
	if err == nil {
		t.Fatal("Exchange followed a redirect")
	}
	var statusError *StatusError
	if !errors.As(err, &statusError) {
		t.Fatalf("redirect error has type %T, want *StatusError", err)
	}
	if statusError.Code != http.StatusTemporaryRedirect {
		t.Fatalf("redirect status = %d, want 307", statusError.Code)
	}
	var httpStatus fints.HTTPStatusError
	if !errors.As(err, &httpStatus) {
		t.Fatal("redirect error does not implement fints.HTTPStatusError")
	}
	if httpStatus.HTTPStatus() != http.StatusTemporaryRedirect {
		t.Fatalf("HTTPStatus() = %d, want 307", httpStatus.HTTPStatus())
	}
	if got := landed.Load(); got != 0 {
		t.Fatalf("redirect target handler ran %d time(s)", got)
	}
	location, _ := redirectedTo.Load().(string)
	if location == "" {
		t.Fatal("redirect was never issued")
	}
	assertErrorDoesNotLeak(t, err, "private-path", "landing", "redirect-secret", location)
}

func TestExchangeStatusError(t *testing.T) {
	const responseBody = "SERVER-BODY-DISTINCTIVE-SECRET"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := io.WriteString(w, responseBody); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	transport := newTestTransportAt(t, server, "/private-path?session=query-secret")
	_, err := transport.Exchange(context.Background(), []byte("PIN-4711"))
	if err == nil {
		t.Fatal("Exchange accepted a 500 response")
	}
	var statusError *StatusError
	if !errors.As(err, &statusError) {
		t.Fatalf("error has type %T, want *StatusError", err)
	}
	if statusError.Code != http.StatusInternalServerError {
		t.Fatalf("Code = %d, want 500", statusError.Code)
	}
	var httpStatus fints.HTTPStatusError
	if !errors.As(err, &httpStatus) {
		t.Fatal("status error does not implement fints.HTTPStatusError")
	}
	if httpStatus.HTTPStatus() != http.StatusInternalServerError {
		t.Fatalf("HTTPStatus() = %d, want 500", httpStatus.HTTPStatus())
	}
	if err.Error() != "httptransport: unexpected HTTP status 500" {
		t.Fatalf("Error() = %q", err.Error())
	}
	assertErrorDoesNotLeak(t, err, responseBody, "private-path", "query-secret")
}

func TestExchangeResponseSizeLimit(t *testing.T) {
	exactlyAtCap := bytes.Repeat([]byte("a"), 24) // 32 base64 characters
	overCap := bytes.Repeat([]byte("b"), 27)      // 36 base64 characters
	if got := base64.StdEncoding.EncodedLen(len(exactlyAtCap)); got != 32 {
		t.Fatalf("test payload encodes to %d bytes, want 32", got)
	}
	if got := base64.StdEncoding.EncodedLen(len(overCap)); got != 36 {
		t.Fatalf("test payload encodes to %d bytes, want 36", got)
	}

	cases := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{name: "exactly at the cap", payload: exactlyAtCap, wantErr: false},
		{name: "over the cap", payload: overCap, wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(base64.StdEncoding.EncodeToString(testCase.payload))); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()

			transport := newTestTransport(t, server, WithMaxResponseBytes(32))
			got, err := transport.Exchange(context.Background(), []byte("request"))
			if testCase.wantErr {
				if err == nil {
					t.Fatal("Exchange accepted a response over the cap")
				}
				if !errors.Is(err, errResponseTooLarge) {
					t.Fatalf("error = %v, want the size-limit error", err)
				}
				if got != nil {
					t.Fatalf("Exchange returned %d truncated bytes", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("Exchange at the cap: %v", err)
			}
			if !bytes.Equal(got, testCase.payload) {
				t.Fatalf("Exchange returned %d bytes, want %d", len(got), len(testCase.payload))
			}
		})
	}
}

func TestExchangeInvalidBase64(t *testing.T) {
	const body = "!!!!SERVER-INVALID-BASE64-SECRET!!!!"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	transport := newTestTransport(t, server)
	got, err := transport.Exchange(context.Background(), []byte("request"))
	if err == nil {
		t.Fatal("Exchange accepted invalid base64")
	}
	if !errors.Is(err, errInvalidBase64) {
		t.Fatalf("error = %v, want the base64 error", err)
	}
	if got != nil {
		t.Fatalf("Exchange returned %d bytes for invalid base64", len(got))
	}
	assertErrorDoesNotLeak(t, err, body, "SERVER-INVALID")
}

func TestExchangeIgnoresWhitespaceInBase64(t *testing.T) {
	payload := []byte("HNHBK:1:3+000000000002+1+whitespace'")
	encoded := base64.StdEncoding.EncodeToString(payload)
	var builder strings.Builder
	for index, b := range encoded {
		builder.WriteRune(b)
		switch index {
		case 3:
			builder.WriteString("\r\n")
		case 7:
			builder.WriteByte(' ')
		case 11:
			builder.WriteByte('\t')
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, builder.String()); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	transport := newTestTransport(t, server)
	got, err := transport.Exchange(context.Background(), []byte("request"))
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Exchange returned %d bytes, want %d", len(got), len(payload))
	}
}

func TestExchangeContextCanceled(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
	}))
	defer server.Close()
	defer close(release)

	transport := newTestTransport(t, server, WithTimeout(10*time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := transport.Exchange(ctx, []byte("request"))
		result <- err
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was never reached")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exchange did not return after cancellation")
	}
}

func TestExchangeWipesBase64Buffer(t *testing.T) {
	const secretPIN = "PIN-4711-DISTINCTIVE-SECRET" // #nosec G101 -- a test marker, not a credential.
	const secretResponse = "RESPONSE-BODY-DISTINCTIVE-SECRET"
	const secretPath = "private-bank-path"
	const secretQuery = "query-session-secret" // #nosec G101 -- a test marker, not a credential.

	request := []byte("HNHBK:1:3+000000000001+1+" + secretPIN + "'")
	response := []byte("HNHBK:1:3+000000000002+1+" + secretResponse + "'")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+secretPath {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "session="+secretQuery {
			t.Errorf("request query = %q", r.URL.RawQuery)
		}
		writeBase64Response(t, w, response)
	}))
	defer server.Close()

	transport := newTestTransportAt(t, server, "/"+secretPath+"?session="+secretQuery)
	var wiped []byte
	calls := 0
	transport.base64Wiped = func(buffer []byte) {
		calls++
		wiped = append([]byte(nil), buffer...)
	}

	original := append([]byte(nil), request...)
	got, err := transport.Exchange(context.Background(), request)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("Exchange returned %d bytes, want %d", len(got), len(response))
	}

	if calls != 1 {
		t.Fatalf("wipe hook ran %d time(s), want 1", calls)
	}
	if want := base64.StdEncoding.EncodedLen(len(request)); len(wiped) != want {
		t.Fatalf("wiped buffer has %d bytes, want %d", len(wiped), want)
	}
	for index, b := range wiped {
		if b != 0 {
			t.Fatalf("wiped buffer byte %d is not zero", index)
		}
	}
	if !bytes.Equal(request, original) {
		t.Fatal("Exchange modified the caller's request")
	}

	base64PIN := base64.StdEncoding.EncodeToString([]byte(secretPIN))
	forbidden := []string{secretPIN, base64PIN, secretResponse, secretPath, secretQuery}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		assertNoLeak(t, "transport "+verb, fmt.Sprintf(verb, transport), forbidden...)
		// A Transport value, not only a pointer, stays masked.
		assertNoLeak(t, "transport value "+verb, fmt.Sprintf(verb, *transport), forbidden...)
	}
	if want := "https://" + transport.host; transport.String() != want {
		t.Fatalf("String() = %q, want scheme and host only", transport.String())
	}
}

func TestExchangeErrorsNeverLeak(t *testing.T) {
	const secretPIN = "PIN-4711-DISTINCTIVE-SECRET" // #nosec G101 -- a test marker, not a credential.
	const secretResponse = "RESPONSE-BODY-DISTINCTIVE-SECRET"
	const secretPath = "private-bank-path"
	const secretQuery = "query-session-secret" // #nosec G101 -- a test marker, not a credential.

	base64PIN := base64.StdEncoding.EncodeToString([]byte(secretPIN))
	forbidden := []string{secretPIN, base64PIN, secretResponse, secretPath, secretQuery}
	request := []byte("HNHBK:1:3+000000000001+1+" + secretPIN + "'")

	cases := []struct {
		name    string
		handler http.HandlerFunc
		options []Option
	}{
		{
			name: "status error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				if _, err := io.WriteString(w, secretResponse); err != nil {
					t.Errorf("write response: %v", err)
				}
			},
		},
		{
			name: "invalid base64",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.WriteString(w, secretResponse+"!!!"); err != nil {
					t.Errorf("write response: %v", err)
				}
			},
		},
		{
			name: "over the cap",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.WriteString(w, base64.StdEncoding.EncodeToString([]byte(secretResponse))); err != nil {
					t.Errorf("write response: %v", err)
				}
			},
			options: []Option{WithMaxResponseBytes(8)},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewTLSServer(testCase.handler)
			defer server.Close()

			transport := newTestTransportAt(t, server, "/"+secretPath+"?session="+secretQuery, testCase.options...)
			_, err := transport.Exchange(context.Background(), request)
			if err == nil {
				t.Fatal("Exchange accepted a failing response")
			}
			assertErrorDoesNotLeak(t, err, forbidden...)
		})
	}
}
