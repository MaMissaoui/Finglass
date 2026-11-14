package trace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type fixedTransport struct {
	response []byte
	err      error
}

func (t *fixedTransport) Exchange(_ context.Context, _ []byte) ([]byte, error) {
	return t.response, t.err
}

type statusError struct {
	status int
	text   string
}

func (e statusError) Error() string   { return e.text }
func (e statusError) HTTPStatus() int { return e.status }

func TestExchangeReturnsTheInnerResponseAndErrorUnchanged(t *testing.T) {
	request := []byte("HNHBK:1:3+000000000100+300+DIALOGMARK0001+1'HNHBS:2:1+1'")
	originalRequest := append([]byte(nil), request...)
	response := []byte("HNHBK:1:3+000000000100+300+DIALOGMARK0001+1'" +
		"HIRMG:2:2+0010::OK'" +
		"HNHBS:3:1+1'")
	originalResponse := append([]byte(nil), response...)
	sentinel := errors.New("inner error SECRETMARK")

	inner := &fixedTransport{response: response, err: sentinel}
	recorder := NewRecorder(inner)

	gotResponse, gotError := recorder.Exchange(context.Background(), request)
	if !bytes.Equal(gotResponse, response) {
		t.Fatal("Exchange did not return the inner response unchanged")
	}
	if !errors.Is(gotError, sentinel) {
		t.Fatalf("Exchange error = %v, want the inner error", gotError)
	}
	if !bytes.Equal(request, originalRequest) {
		t.Fatal("Exchange mutated the caller's request")
	}
	if recorder.Len() != 1 {
		t.Fatalf("Len = %d, want 1", recorder.Len())
	}

	var before strings.Builder
	if _, err := recorder.WriteTo(&before); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	beforeText := before.String()
	assertAbsent(t, beforeText, "DIALOGMARK0001", "000000000100", "SECRETMARK")

	// The recorder must keep no reference to either buffer.
	clear(request)
	clear(response)
	clear(originalResponse)
	clear(originalRequest)

	var after strings.Builder
	if _, err := recorder.WriteTo(&after); err != nil {
		t.Fatalf("WriteTo after wipe: %v", err)
	}
	if after.String() != beforeText {
		t.Fatalf("trace changed after wiping the buffers:\nbefore:\n%s\nafter:\n%s", beforeText, after.String())
	}
}

func TestExchangeErrorLines(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "http status",
			err:  statusError{status: 503, text: "http SECRETMARK"},
			want: "# exchange 1 error: transport http 503",
		},
		{
			name: "cancelled",
			err:  fmt.Errorf("wrapped: %w", context.Canceled),
			want: "# exchange 1 error: cancelled",
		},
		{
			name: "deadline",
			err:  fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
			want: "# exchange 1 error: deadline",
		},
		{
			name: "plain transport",
			err:  errors.New("SECRETMARK transport failed"),
			want: "# exchange 1 error: transport",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := NewRecorder(&fixedTransport{err: testCase.err})
			_, _ = recorder.Exchange(context.Background(), []byte("HNHBK:1:3+1+300+X+1'"))
			var output strings.Builder
			if _, err := recorder.WriteTo(&output); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if !strings.Contains(output.String(), testCase.want) {
				t.Fatalf("trace does not contain %q:\n%s", testCase.want, output.String())
			}
			assertAbsent(t, output.String(), "SECRETMARK", testCase.err.Error())
		})
	}
}

func TestExchangeUnparseableRequestAndResponse(t *testing.T) {
	recorder := NewRecorder(&fixedTransport{response: []byte("not a message RESPONSEMARK")})
	_, _ = recorder.Exchange(context.Background(), []byte("not a request REQUESTMARK"))

	var output strings.Builder
	if _, err := recorder.WriteTo(&output); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	traceText := output.String()
	assertContains(t, traceText, "<unparseable message, 25 bytes>", "<unparseable message, 26 bytes>")
	assertAbsent(t, traceText, "RESPONSEMARK", "REQUESTMARK")
}

func TestRecorderNoteAndFormatting(t *testing.T) {
	recorder := NewRecorder(&fixedTransport{response: []byte("HIRMG:2:2+0010::OK'")})
	recorder.Note("bank 1 of 3")
	recorder.Note("account DE00999999990000000001")

	var output strings.Builder
	if _, err := recorder.WriteTo(&output); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	assertContains(t, output.String(),
		"# note: bank 1 of 3",
		"# note: account …0001",
	)
	assertAbsent(t, output.String(), "DE00999999990000000001")

	_, _ = recorder.Exchange(context.Background(), []byte("HNHBK:1:3+1+300+DIALOGMARK0001+1'"))
	if recorder.Len() != 1 {
		t.Fatalf("Len = %d, want 1", recorder.Len())
	}

	const want = "trace.Recorder{exchanges: 1}"
	for _, value := range []any{recorder, *recorder} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			if got := fmt.Sprintf(format, value); got != want {
				t.Fatalf("%s %T = %q, want %q", format, value, got, want)
			}
		}
	}
	assertAbsent(t, fmt.Sprintf("%v %+v %#v", recorder, recorder, recorder), "DIALOGMARK0001")
}

func TestRecorderConcurrentExchangeAndWriteTo(t *testing.T) {
	recorder := NewRecorder(&fixedTransport{
		response: []byte("HIRMG:2:2+0010::OK'"),
	})

	const workers = 25
	var wait sync.WaitGroup
	wait.Add(workers * 2)
	for index := 0; index < workers; index++ {
		go func() {
			defer wait.Done()
			_, _ = recorder.Exchange(context.Background(), []byte("HNHBK:1:3+1+300+DIALOGMARK0001+1'"))
		}()
		go func() {
			defer wait.Done()
			var output strings.Builder
			_, _ = recorder.WriteTo(&output)
		}()
	}
	wait.Wait()

	if recorder.Len() != workers {
		t.Fatalf("Len = %d, want %d", recorder.Len(), workers)
	}
}
