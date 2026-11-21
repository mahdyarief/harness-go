package blob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
)

// keyLen is the only accepted key length: a hex-encoded SHA-256 digest.
const keyLen = 64

const (
	dirPerm   = 0o700
	filePerm  = 0o600
	tmpPrefix = ".tmp-"
)

// Disk stores objects as files under a root directory. Objects are sharded two
// levels deep by key prefix (ab/cd/<key>) so no directory grows unbounded.
//
// All file access goes through os.Root, so even a symlink planted inside the
// storage tree cannot lead a read, write or delete outside it.
type Disk struct {
	root *os.Root
}

var _ Store = (*Disk)(nil)

// NewDisk opens (creating if needed) the storage root and verifies it is
// writable by creating and removing a probe file.
func NewDisk(root string) (*Disk, error) {
	if root == "" {
		return nil, errors.New("blob: storage root is empty")
	}
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, fmt.Errorf("blob: create storage root %q: %w", root, err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("blob: open storage root %q: %w", root, err)
	}
	d := &Disk{root: r}
	probe := tmpPrefix + "probe-" + randomHex()
	f, err := r.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("blob: storage root %q is not writable: %w", root, err)
	}
	_ = f.Close()
	if err := r.Remove(probe); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("blob: storage root %q: remove probe file: %w", root, err)
	}
	return d, nil
}

// Close releases the root directory handle.
func (d *Disk) Close() error { return d.root.Close() }

// validKey accepts exactly 64 characters from [0-9a-f]. This rejects empty
// keys, uppercase, dots, separators and everything else that could change the
// resolved path.
func validKey(key string) bool {
	if len(key) != keyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func shardDir(key string) string { return path.Join(key[0:2], key[2:4]) }

func randomHex() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b[:])
}

func (d *Disk) Put(ctx context.Context, key string, data []byte) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validKey(key) {
		return ErrInvalidKey
	}
	dir := shardDir(key)
	final := path.Join(dir, key)

	// Same key with same size: content-addressed, so it is the same object.
	if fi, statErr := d.root.Stat(final); statErr == nil && fi.Mode().IsRegular() && fi.Size() == int64(len(data)) {
		return nil
	}

	if err := d.root.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("blob: create shard dir: %w", err)
	}

	tmp := path.Join(dir, tmpPrefix+randomHex())
	f, err := d.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return fmt.Errorf("blob: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = d.root.Remove(tmp)
		}
	}()

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("blob: write: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("blob: sync: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("blob: close: %w", err)
	}
	if err = d.root.Rename(tmp, final); err != nil {
		return fmt.Errorf("blob: rename: %w", err)
	}
	return nil
}

func (d *Disk) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validKey(key) {
		return nil, ErrInvalidKey
	}
	b, err := d.root.ReadFile(path.Join(shardDir(key), key))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blob: read: %w", err)
	}
	return b, nil
}

func (d *Disk) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validKey(key) {
		return ErrInvalidKey
	}
	if err := d.root.Remove(path.Join(shardDir(key), key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob: delete: %w", err)
	}
	return nil
}
