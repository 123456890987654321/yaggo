//go:generate yaggo -spec ../petstore.yaml -out . -package petstore

// Package petstore contains the generated types, server interface, and HTTP
// client for the Petstore API defined in ../petstore.yaml.
package petstore
