// Server example: in-memory Petstore server.
//
// Demonstrates the full wire-up you'd write in your own service:
//
//  1. A type that implements yaggo's generated ServerInterface (one method per
//     OpenAPI operation). yaggo prescribes the signatures; the business logic
//     inside each method is entirely yours.
//  2. A chi router with the middlewares you want, plus the routes mounted by
//     petstore.RegisterHandlers.
//  3. A standard library http.Server with sensible timeouts, optional TLS, and
//     graceful shutdown on SIGINT/SIGTERM.
//
// Run plain HTTP (default):
//
//	go run ./examples/server
//
// Run HTTPS with a TLS certificate:
//
//	go run ./examples/server -addr :8443 -cert cert.pem -key key.pem
//
// Quick local certificate for development (issued by Go's test toolchain or
// `mkcert`); never use self-signed certs in production:
//
//	go run filippo.io/mkcert@latest localhost 127.0.0.1 ::1
//	mv localhost+2.pem cert.pem && mv localhost+2-key.pem key.pem
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/123456890987654321/yaggo/examples/petstore"
)

// store is the example's in-memory backing for the Petstore API. A real service
// would replace it with a database-backed type — anything that implements the
// generated petstore.ServerInterface will plug in unchanged.
//
// Concurrency:
//   - mu is an RWMutex because reads (List, Get) dominate writes (Create,
//     Update, Delete) and the standard library's RWMutex lets concurrent
//     readers proceed without blocking each other.
//   - next is an atomic counter so we can allocate IDs without holding the
//     write lock; the lock then only covers the map mutation itself.
type store struct {
	mu   sync.RWMutex
	pets map[int64]petstore.Pet
	next atomic.Int64
}

func newStore() *store {
	return &store{pets: make(map[int64]petstore.Pet)}
}

// The methods below collectively satisfy petstore.ServerInterface, which yaggo
// generates from the OpenAPI spec. Each signature is fixed by the spec:
//   - the (w, r) pair is always first;
//   - path parameters arrive as typed positional args (e.g. petId int64);
//   - if the operation has a request body, the decoded struct comes last and
//     has already been Validate()'d when the spec declares required fields.
// You write the body; yaggo writes the routing, decoding, and error handling.

// ListPets handles GET /pets. Filtering / pagination is application logic and
// lives here, not in the generated code — yaggo only decodes the params struct.
func (s *store) ListPets(w http.ResponseWriter, _ *http.Request, params petstore.ListPetsParams) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Status is an optional pointer (*petstore.PetStatus). nil → no filter.
	var out []petstore.Pet
	for _, p := range s.pets {
		if params.Status != nil && (p.Status == nil || *p.Status != *params.Status) {
			continue
		}
		out = append(out, p)
	}
	// Limit is *int32; nil means "no cap". Clamp before slicing so callers
	// asking for more than we have still get a clean response.
	if params.Limit != nil && len(out) > int(*params.Limit) {
		out = out[:*params.Limit]
	}
	// Always return an array, never JSON null — easier for typed clients to
	// consume and avoids surprising nil-vs-empty bugs downstream.
	if out == nil {
		out = []petstore.Pet{}
	}
	// WriteJSON is a helper yaggo emits next to RegisterHandlers; it sets the
	// Content-Type header and JSON-encodes the value. There's also WriteError
	// (used below) and WriteContent for non-application/json media types.
	petstore.WriteJSON(w, http.StatusOK, out)
}

// CreatePet handles POST /pets. body has already been JSON-decoded into a
// petstore.NewPet and, because NewPet's `name` is required in the spec,
// Validate() has been called by the generated handler before we run.
func (s *store) CreatePet(w http.ResponseWriter, _ *http.Request, body petstore.NewPet) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.next.Add(1)
	pet := petstore.Pet{Id: id, Name: body.Name, Tag: body.Tag, Status: body.Status}
	s.pets[id] = pet
	petstore.WriteJSON(w, http.StatusCreated, pet)
}

// GetPet handles GET /pets/{petId}. petId arrives already parsed as int64; if
// the URL value couldn't be parsed, the generated handler returns 400 before
// this method is ever called.
func (s *store) GetPet(w http.ResponseWriter, _ *http.Request, petId int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.pets[petId]
	if !ok {
		// WriteError is the convenience helper for the same JSON shape the
		// generator's default error handler uses: {"error": "<msg>"}.
		petstore.WriteError(w, http.StatusNotFound, fmt.Sprintf("pet %d not found", petId))
		return
	}
	petstore.WriteJSON(w, http.StatusOK, p)
}

// UpdatePet handles PUT /pets/{petId}. The body type (NewPet) is the same as
// POST because the spec reuses the schema — yaggo lifts that 1:1 into Go.
func (s *store) UpdatePet(w http.ResponseWriter, _ *http.Request, petId int64, body petstore.NewPet) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.pets[petId]; !ok {
		petstore.WriteError(w, http.StatusNotFound, fmt.Sprintf("pet %d not found", petId))
		return
	}
	updated := petstore.Pet{Id: petId, Name: body.Name, Tag: body.Tag, Status: body.Status}
	s.pets[petId] = updated
	petstore.WriteJSON(w, http.StatusOK, updated)
}

// DeletePet handles DELETE /pets/{petId}. 204 No Content is the spec's success
// response; we write the status directly because there is no body to emit.
func (s *store) DeletePet(w http.ResponseWriter, _ *http.Request, petId int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.pets[petId]; !ok {
		petstore.WriteError(w, http.StatusNotFound, fmt.Sprintf("pet %d not found", petId))
		return
	}
	delete(s.pets, petId)
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	// CLI flags. None of these are required by yaggo — they're the knobs a
	// real deployment usually exposes (listen address, TLS material, request
	// timeouts, body cap). Lifted to flags so the example doubles as something
	// you can poke at without recompiling.
	addr := flag.String("addr", ":8080", "listen address (use :8443 for TLS)")
	certFile := flag.String("cert", "", "PEM-encoded TLS certificate (enables HTTPS when set alongside -key)")
	keyFile := flag.String("key", "", "PEM-encoded TLS private key")
	readTimeout := flag.Duration("read-timeout", 5*time.Second, "http.Server.ReadTimeout")
	writeTimeout := flag.Duration("write-timeout", 10*time.Second, "http.Server.WriteTimeout")
	// petstore.DefaultMaxBodyBytes is 1 MiB. The flag exists so you can demo
	// the 413 path with `-max-body-bytes 100` and POSTing a larger payload.
	maxBody := flag.Int64("max-body-bytes", petstore.DefaultMaxBodyBytes, "per-request body size cap; 0 disables")
	flag.Parse()

	// chi is the router yaggo's generated server depends on (chi/v5). Any
	// chi-compatible middleware composes with the generated routes — Logger
	// and Recoverer are the two most universally useful.
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// This single call mounts every spec-derived route onto r. The store
	// implements petstore.ServerInterface; WithMaxBodyBytes is one of the
	// generated ServerOption values (see also WithMiddleware, WithErrorHandler).
	petstore.RegisterHandlers(r, newStore(), petstore.WithMaxBodyBytes(*maxBody))

	srv := &http.Server{
		Addr:         *addr,
		Handler:      r,
		ReadTimeout:  *readTimeout,
		WriteTimeout: *writeTimeout,
		IdleTimeout:  60 * time.Second,
		// MinVersion refuses pre-TLS 1.2 negotiations regardless of the cert.
		// Concrete cipher suites are intentionally left to Go's default — the
		// stdlib tracks current best-practice and rotates as algorithms age,
		// so pinning a list here would only let it grow stale.
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	// Graceful shutdown: when SIGINT/SIGTERM arrives, we stop accepting new
	// connections and give in-flight requests up to 10 s to finish. Without
	// this, `Ctrl-C` would drop active responses mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}()

	// TLS is enabled by passing both -cert and -key. The error returned by
	// ListenAndServe(TLS) is http.ErrServerClosed on a clean shutdown — that's
	// the expected, non-failure path so we filter it out before calling Fatal.
	var err error
	if *certFile != "" || *keyFile != "" {
		if *certFile == "" || *keyFile == "" {
			log.Fatal("-cert and -key must be supplied together")
		}
		log.Printf("listening on https://%s", srv.Addr)
		err = srv.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		log.Printf("listening on http://%s (no TLS — use -cert and -key to enable)", srv.Addr)
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
