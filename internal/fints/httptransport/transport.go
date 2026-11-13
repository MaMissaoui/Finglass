// Package httptransport is the HTTPS transport for FinTS messages.
//
// It posts a base64-encoded FinTS message to a bank's endpoint and returns the
// decoded answer. It implements [fints.Transport] and owns the HTTPS rules the
// dialog must not know about: https only, no redirects, a bounded response and
// errors that never carry a body, a URL path, a query or base64 text.
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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	// defaultTimeout bounds one Exchange, including the TLS handshake.
	defaultTimeout = 60 * time.Second
	// defaultMaxResponseBytes matches [fints.MaxMessageSize]: a response larger
	// than the codec can parse is rejected before it is decoded.
	defaultMaxResponseBytes = fints.MaxMessageSize

	contentType = "text/plain; charset=ISO-8859-1"
	userAgent   = "Finglass"
)

// Fixed errors. None of them embeds a request, a response, a URL or base64
// text, so they can be returned and logged without exposing bank data.
var (
	errInvalidURL       = errors.New("httptransport: server URL must be a valid URL")
	errNotHTTPS         = errors.New("httptransport: server URL must use https")
	errMissingHost      = errors.New("httptransport: server URL must contain a host")
	errUserInfo         = errors.New("httptransport: server URL must not contain user information")
	errFragment         = errors.New("httptransport: server URL must not contain a fragment")
	errInvalidOption    = errors.New("httptransport: invalid option")
	errInvalidTimeout   = errors.New("httptransport: timeout must be positive")
	errInvalidLimit     = errors.New("httptransport: response size limit must be positive")
	errNotConfigured    = errors.New("httptransport: transport is not configured")
	errRequestFailed    = errors.New("httptransport: could not build the request")
	errTransportFailed  = errors.New("httptransport: request failed")
	errResponseRead     = errors.New("httptransport: could not read the response")
	errResponseTooLarge = errors.New("httptransport: response exceeds the size limit")
	errInvalidBase64    = errors.New("httptransport: response is not valid base64")
)

// StatusError is a non-2xx HTTP response, or the 3xx response of a refused
// redirect.
//
// It carries the status code only: the body is never read into it. The dialog
// reads the code through [fints.HTTPStatusError].
type StatusError struct {
	// Code is the HTTP status code.
	Code int
}

// Error implements error. It shows the code only.
func (e *StatusError) Error() string {
	if e == nil {
		return "httptransport: <nil status error>"
	}
	return "httptransport: unexpected HTTP status " + strconv.Itoa(e.Code)
}

// HTTPStatus returns the status code, implementing [fints.HTTPStatusError].
func (e *StatusError) HTTPStatus() int {
	if e == nil {
		return 0
	}
	return e.Code
}

var _ fints.HTTPStatusError = (*StatusError)(nil)
var _ fints.Transport = (*Transport)(nil)

// redirectError is returned by CheckRedirect. It carries the status of the
// response that asked for the redirect, without the redirect's location.
type redirectError struct {
	code int
}

// Error implements error. It shows no URL.
func (e *redirectError) Error() string {
	return "httptransport: redirect refused"
}

// settings is the mutable configuration an [Option] changes.
type settings struct {
	roots            *x509.CertPool
	timeout          time.Duration
	maxResponseBytes int
}

// Option configures a [Transport] built by [New].
type Option func(*settings) error

// WithRootCAs replaces the system roots used to verify the bank's certificate.
// A nil pool restores the system roots. Tests use it for their test server.
func WithRootCAs(roots *x509.CertPool) Option {
	return func(config *settings) error {
		config.roots = roots
		return nil
	}
}

// WithTimeout bounds one Exchange, including the TLS handshake. The default is
// 60 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(config *settings) error {
		if timeout <= 0 {
			return errInvalidTimeout
		}
		config.timeout = timeout
		return nil
	}
}

// WithMaxResponseBytes bounds the encoded response body. The default is
// [fints.MaxMessageSize] (16 MiB). A response body larger than the limit is an
// error, never a truncated answer.
func WithMaxResponseBytes(limit int) Option {
	return func(config *settings) error {
		if limit <= 0 {
			return errInvalidLimit
		}
		config.maxResponseBytes = limit
		return nil
	}
}

// Transport posts FinTS messages to one bank endpoint over HTTPS.
//
// It is safe to reuse for several dialog messages. It keeps no request and no
// response. String, GoString and Format print only the scheme and host.
type Transport struct {
	endpoint         string
	scheme           string
	host             string
	client           *http.Client
	maxResponseBytes int

	// base64Wiped is an unexported test seam. A test sets it to observe the
	// encoded request buffer after [fints.Wipe] has zeroed it.
	base64Wiped func([]byte)
}

// New builds a transport for serverURL.
//
// The URL must parse, use https, have a host and carry no user information and
// no fragment. Everything else is rejected here, before any connection is
// opened. The default TLS minimum is 1.2 with the system roots.
func New(serverURL string, options ...Option) (*Transport, error) {
	endpoint, err := parseEndpoint(serverURL)
	if err != nil {
		return nil, err
	}

	config := settings{
		timeout:          defaultTimeout,
		maxResponseBytes: defaultMaxResponseBytes,
	}
	for _, option := range options {
		if option == nil {
			return nil, errInvalidOption
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    config.roots,
	}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			TLSClientConfig:   tlsConfig,
			ForceAttemptHTTP2: true,
		},
		Timeout:       config.timeout,
		CheckRedirect: refuseRedirect,
	}

	return &Transport{
		endpoint:         endpoint.String(),
		scheme:           endpoint.Scheme,
		host:             endpoint.Host,
		client:           client,
		maxResponseBytes: config.maxResponseBytes,
	}, nil
}

// parseEndpoint validates the server URL without opening a connection.
func parseEndpoint(serverURL string) (*url.URL, error) {
	endpoint, err := url.Parse(serverURL)
	if err != nil {
		return nil, errInvalidURL
	}
	if endpoint.Scheme != "https" {
		return nil, errNotHTTPS
	}
	if endpoint.Host == "" {
		return nil, errMissingHost
	}
	if endpoint.User != nil {
		return nil, errUserInfo
	}
	if endpoint.Fragment != "" || endpoint.RawFragment != "" || strings.Contains(serverURL, "#") {
		return nil, errFragment
	}
	return endpoint, nil
}

// refuseRedirect rejects every redirect, so a body that carries a PIN is never
// re-sent to another host or path. It keeps only the redirecting status.
func refuseRedirect(request *http.Request, via []*http.Request) error {
	code := 0
	if request != nil && request.Response != nil {
		code = request.Response.StatusCode
	} else if len(via) > 0 && via[len(via)-1].Response != nil {
		code = via[len(via)-1].Response.StatusCode
	}
	return &redirectError{code: code}
}

// String returns only the scheme and host, never the path or query. The
// formatting methods have value receivers so that printing a Transport value,
// not only a pointer, is masked too.
func (t Transport) String() string {
	return t.scheme + "://" + t.host
}

// GoString returns the same representation as String.
func (t Transport) GoString() string {
	return t.String()
}

// Format returns the same representation as String for every fmt verb.
func (t Transport) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(t.String()))
}

// Exchange sends request to the bank and returns the decoded answer.
//
// The request is base64-encoded into a second buffer, which is wiped with
// [fints.Wipe] before Exchange returns on every path. The caller keeps
// ownership of request and must wipe it itself.
func (t *Transport) Exchange(ctx context.Context, request []byte) ([]byte, error) {
	if t == nil || t.client == nil {
		return nil, errNotConfigured
	}

	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(request)))
	base64.StdEncoding.Encode(encoded, request)
	defer func() {
		fints.Wipe(encoded)
		if t.base64Wiped != nil {
			t.base64Wiped(encoded)
		}
	}()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, errRequestFailed
	}
	httpRequest.Header.Set("Content-Type", contentType)
	httpRequest.Header.Set("Accept", "*/*")
	httpRequest.Header.Set("User-Agent", userAgent)

	response, err := t.client.Do(httpRequest)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, classifyRequestError(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &StatusError{Code: response.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, int64(t.maxResponseBytes)+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errResponseRead
	}
	if len(body) > t.maxResponseBytes {
		return nil, errResponseTooLarge
	}
	return decodeBase64(body)
}

// classifyRequestError drops everything that could carry the URL. A context
// failure is returned as is, so errors.Is keeps working; a refused redirect
// becomes a status error; any other HTTP error is unwrapped from url.Error,
// whose text contains the full URL.
func classifyRequestError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	var redirect *redirectError
	if errors.As(err, &redirect) {
		return &StatusError{Code: redirect.code}
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		return urlError.Err
	}
	return errTransportFailed
}

// decodeBase64 decodes the response body, ignoring CR, LF, space and tab. An
// invalid body is an error without its content.
func decodeBase64(body []byte) ([]byte, error) {
	filtered := body[:0]
	for _, b := range body {
		switch b {
		case '\r', '\n', ' ', '\t':
			continue
		default:
			filtered = append(filtered, b)
		}
	}
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(filtered)))
	n, err := base64.StdEncoding.Decode(decoded, filtered)
	if err != nil {
		fints.Wipe(decoded)
		return nil, errInvalidBase64
	}
	return decoded[:n], nil
}
