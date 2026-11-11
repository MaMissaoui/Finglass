// Package bind holds the thin Wails services the frontend can call.
package bind

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Version is the application version. Release builds override it with
// -ldflags -X github.com/MaMissaoui/Finglass/internal/bind.Version=<version>.
var Version = "0.4.0-dev"

const (
	// TickEvent is the name of the event emitted once per second while the
	// application is running.
	TickEvent = "appinfo:tick"
	// tickInterval is the period between two tick events.
	tickInterval = time.Second
)

// AppInfoDTO is the information shown in the window.
type AppInfoDTO struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// TickPayload is the payload of the appinfo:tick event.
type TickPayload struct {
	Count int `json:"count"`
}

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[TickPayload](TickEvent)
}

var errEmptyName = errors.New("name must not be empty")

// AppInfo is the first bound service. It reports build and runtime details
// and emits a heartbeat event.
type AppInfo struct {
	mu      sync.Mutex
	app     *application.App
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// ServiceName implements application.ServiceName.
func (s *AppInfo) ServiceName() string {
	return "AppInfo"
}

// Info returns the application and runtime details.
func (s *AppInfo) Info() (AppInfoDTO, error) {
	return AppInfoDTO{
		Name:      "Finglass",
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}, nil
}

// Greet returns a greeting for name. The name is trimmed and must not be
// empty.
func (s *AppInfo) Greet(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errEmptyName
	}
	return fmt.Sprintf("Hello, %s", name), nil
}

// ServiceStartup starts the ticker that emits appinfo:tick once per second.
// The ticker stops when the application context is cancelled or the service is
// shut down.
func (s *AppInfo) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	app := application.Get()
	if app == nil {
		return errors.New("AppInfo needs a running Wails application")
	}

	tickerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	s.app = app
	s.started = true
	s.cancel = cancel
	s.done = done

	go s.runTicker(tickerCtx, done)
	return nil
}

func (s *AppInfo) runTicker(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count++
			s.app.Event.Emit(TickEvent, TickPayload{Count: count})
		}
	}
}

// ServiceShutdown stops the ticker and waits until its goroutine has exited.
func (s *AppInfo) ServiceShutdown() error {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.started = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}
