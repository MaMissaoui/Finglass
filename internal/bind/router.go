package bind

import (
	"sync"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
)

// PagePublisher is a service that can publish its current state.
type PagePublisher interface {
	publishPage()
}

// PromptPublisher is a service that can publish the open banking prompt.
type PromptPublisher interface {
	publishPrompt()
}

// Router is the one session listener. It maps a changed page to the service
// that publishes it, and "shell" to the Shell service.
//
// main registers it once; the page services themselves never listen to the
// session.
type Router struct {
	mu      sync.RWMutex
	pages   map[string]PagePublisher
	prompts map[string]PromptPublisher
	shell   PagePublisher
	remove  func()
}

// NewRouter builds the router and registers its one session listener. session
// may be nil only for a self-check, which never starts the application.
func NewRouter(session *shell.Session) *Router {
	router := &Router{
		pages:   make(map[string]PagePublisher),
		prompts: make(map[string]PromptPublisher),
	}
	if session != nil {
		router.remove = session.AddListener(router.route)
	}
	return router
}

// Register makes publisher receive the changes of page.
func (r *Router) Register(page string, publisher PagePublisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if publisher == nil {
		delete(r.pages, page)
		return
	}
	r.pages[page] = publisher
}

// RegisterPrompt makes publisher receive the prompt changes of page.
func (r *Router) RegisterPrompt(page string, publisher PromptPublisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if publisher == nil {
		delete(r.prompts, page)
		return
	}
	r.prompts[page] = publisher
}

// RegisterShell makes publisher receive navigation changes.
func (r *Router) RegisterShell(publisher PagePublisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shell = publisher
}

// Shutdown removes the session listener.
func (r *Router) Shutdown() error {
	if r.remove != nil {
		r.remove()
		r.remove = nil
	}
	return nil
}

func (r *Router) route(change shell.Change) {
	r.mu.RLock()
	prompt := r.prompts[change.Page]
	publisher := r.pages[change.Page]
	if change.Page == "shell" {
		publisher = r.shell
	}
	r.mu.RUnlock()
	if prompt != nil {
		prompt.publishPrompt()
	}
	if publisher != nil {
		publisher.publishPage()
	}
}
