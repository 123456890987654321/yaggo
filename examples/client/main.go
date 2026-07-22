// Client example: exercises every generated client method against the Petstore server.
//
// What this demonstrates:
//   - Constructing a yaggo-generated Client with options (here: a User-Agent).
//   - Passing required vs optional arguments. Required path/body params are
//     plain values; optional query params are pointers, where nil means "not
//     supplied" and ptr(x) means "x".
//   - The 1:1 mapping from spec operations to typed Go methods: every
//     operationId in petstore.yaml has a corresponding Client method here.
//
// Start the server first (from the repo root):
//
//	go run ./examples/server
//
// Then in another terminal (from the repo root):
//
//	go run ./examples/client
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/123456890987654321/yaggo/examples/petstore"
)

// must is example-only sugar that fatals on the first error. Real code would
// inspect each error and decide whether to retry, return, or annotate — yaggo
// surfaces transport, decode, and api-error failures distinctly so callers can
// route them differently. We use must here purely to keep main() short.
func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// ptr returns a pointer to a copy of v. Useful because every optional spec
// field becomes *T in the generated types and signatures, and Go has no syntax
// for taking the address of a literal value directly (`&PetStatus("foo")`
// won't compile).
func ptr[T any](v T) *T { return &v }

func main() {
	ctx := context.Background()

	// NewClient validates the base URL (scheme must be http/https, host must
	// be non-empty) and strips a trailing slash. WithUserAgent is one of the
	// always-emitted client options; see also WithHeader, WithHTTPClient,
	// WithRequestEditor (used to plug in auth — see examples in auth.go).
	c, err := petstore.NewClient("http://localhost:8080",
		petstore.WithUserAgent("yaggo-example/1.0"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create two pets. Required fields (name) are plain strings; optional
	// fields (tag, status) take pointers so the client can distinguish
	// "not supplied" from "explicitly the zero value". ptr() makes the
	// pointer literals tractable.
	buddy := must(c.CreatePet(ctx, petstore.NewPet{
		Name:   "Buddy",
		Tag:    ptr("dog"),
		Status: ptr(petstore.PetStatusAvailable),
	}))
	fmt.Printf("created: id=%d name=%s status=%s\n", buddy.Id, buddy.Name, *buddy.Status)

	whiskers := must(c.CreatePet(ctx, petstore.NewPet{
		Name:   "Whiskers",
		Tag:    ptr("cat"),
		Status: ptr(petstore.PetStatusPending),
	}))
	fmt.Printf("created: id=%d name=%s status=%s\n", whiskers.Id, whiskers.Name, *whiskers.Status)

	// List with no filters: ListPets's signature is
	//   ListPets(ctx, limit *int32, status *PetStatus) ([]Pet, error)
	// Passing nil for both leaves them off the wire — the server sees no
	// `limit=` or `status=` query param at all.
	all := must(c.ListPets(ctx, nil, nil))
	fmt.Printf("all pets (%d): %v\n", len(all), names(all))

	// Filter by enum-typed query param. PetStatusAvailable is a typed
	// constant defined in the generated types.go — invalid enum values
	// can't be expressed without bypassing the type system.
	available := must(c.ListPets(ctx, nil, ptr(petstore.PetStatusAvailable)))
	fmt.Printf("available (%d): %v\n", len(available), names(available))

	// Limit results. The spec declares limit as int32, so the Go type is
	// *int32 — note the explicit `int32(1)` cast.
	limited := must(c.ListPets(ctx, ptr(int32(1)), nil))
	fmt.Printf("limited to 1: %v\n", names(limited))

	// Get a single pet. petId is a required int64 path parameter; the
	// client formats it into the URL ("/pets/123") for you. String path
	// params, by contrast, would be URL-escaped automatically.
	got := must(c.GetPet(ctx, buddy.Id))
	fmt.Printf("get %d: %s\n", got.Id, got.Name)

	// Update reuses the NewPet body type because the spec declares both
	// POST and PUT as taking the same schema. Notice we recycle buddy.Tag
	// (a *string) unchanged — we don't need to deref-and-readdress.
	updated := must(c.UpdatePet(ctx, buddy.Id, petstore.NewPet{
		Name:   buddy.Name,
		Tag:    buddy.Tag,
		Status: ptr(petstore.PetStatusSold),
	}))
	fmt.Printf("updated %d status: %s\n", updated.Id, *updated.Status)

	// Delete has no response body, so the generated method returns only
	// an error. 4xx/5xx responses are mapped to a non-nil error; you do
	// NOT need a "got 204" check here.
	if err := c.DeletePet(ctx, whiskers.Id); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("deleted pet %d\n", whiskers.Id)

	// Confirm deletion by re-listing.
	remaining := must(c.ListPets(ctx, nil, nil))
	fmt.Printf("remaining (%d): %v\n", len(remaining), names(remaining))
}

// names is a display helper, not part of the API surface. It exists so the
// fmt.Printf lines above stay readable.
func names(pets []petstore.Pet) []string {
	out := make([]string, len(pets))
	for i, p := range pets {
		out[i] = p.Name
	}
	return out
}
