// Package blob stores the original bytes of uploaded files. The interface is
// deliberately small so the disk implementation can be swapped for object
// storage without touching callers. Retrieval for the agent never reads from
// here: it searches the extracted text in the database.
package blob

import (
	"context"
	"errors"
)

// ErrNotFound means no object exists under the key.
var ErrNotFound = errors.New("blob: object not found")

// ErrInvalidKey means the key could not be a safe storage name.
var ErrInvalidKey = errors.New("blob: invalid key")

// Store keeps immutable objects addressed by an opaque key. Keys are generated
// by the application (a content hash), never taken from user input, and
// implementations must reject any key that is not a plain lowercase hex name so
// a path can never escape the storage root.
type Store interface {
	// Put writes the object atomically: a reader sees all of it or none of it.
	// Putting an existing key with the same bytes is not an error.
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete is idempotent: deleting a missing key returns nil.
	Delete(ctx context.Context, key string) error
}
