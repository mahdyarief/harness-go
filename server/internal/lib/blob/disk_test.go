package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func keyFor(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newTestDisk(t *testing.T) (*Disk, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kb")
	d, err := NewDisk(dir)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, dir
}

func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.HasPrefix(e.Name(), tmpPrefix) {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestRoundTripAndLayout(t *testing.T) {
	d, dir := newTestDisk(t)
	ctx := context.Background()
	key := keyFor("a")
	want := []byte("hello world")

	if err := d.Put(ctx, key, want); err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(ctx, key)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Get = %q, %v", got, err)
	}

	p := filepath.Join(dir, key[0:2], key[2:4], key)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("expected sharded file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
		}
		di, _ := os.Stat(filepath.Dir(p))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("dir perm = %o, want 700", di.Mode().Perm())
		}
	}
	if left := tempFiles(t, dir); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestPutTwice(t *testing.T) {
	d, _ := newTestDisk(t)
	ctx := context.Background()
	key := keyFor("twice")
	for i := 0; i < 2; i++ {
		if err := d.Put(ctx, key, []byte("same")); err != nil {
			t.Fatalf("Put #%d: %v", i, err)
		}
	}
	got, _ := d.Get(ctx, key)
	if string(got) != "same" {
		t.Fatalf("got %q", got)
	}
}

func TestEmptyObject(t *testing.T) {
	d, _ := newTestDisk(t)
	ctx := context.Background()
	key := keyFor("empty")
	if err := d.Put(ctx, key, nil); err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(ctx, key)
	if err != nil || len(got) != 0 {
		t.Fatalf("Get = %q, %v", got, err)
	}
}

func TestGetMissing(t *testing.T) {
	d, _ := newTestDisk(t)
	if _, err := d.Get(context.Background(), keyFor("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteIdempotent(t *testing.T) {
	d, _ := newTestDisk(t)
	ctx := context.Background()
	key := keyFor("del")
	if err := d.Put(ctx, key, []byte("x")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := d.Delete(ctx, key); err != nil {
			t.Fatalf("Delete #%d: %v", i, err)
		}
	}
	if _, err := d.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := d.Delete(ctx, keyFor("never")); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestInvalidKeys(t *testing.T) {
	d, _ := newTestDisk(t)
	ctx := context.Background()
	good := keyFor("g")
	cases := map[string]string{
		"empty":      "",
		"dotdot":     "..",
		"traversal":  "../../etc/passwd",
		"slash":      good[:10] + "/" + good[11:],
		"backslash":  good[:10] + `\` + good[11:],
		"uppercase":  strings.ToUpper(good),
		"short":      good[:63],
		"long":       good + "0",
		"non hex":    "g" + good[1:],
		"nul":        good[:10] + "\x00" + good[11:],
		"dot prefix": "." + good[1:],
		"space":      " " + good[1:],
		"dotdot64":   strings.Repeat("..", 32),
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			if err := d.Put(ctx, key, []byte("x")); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("Put err = %v, want ErrInvalidKey", err)
			}
			if _, err := d.Get(ctx, key); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("Get err = %v, want ErrInvalidKey", err)
			}
			if err := d.Delete(ctx, key); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("Delete err = %v, want ErrInvalidKey", err)
			}
		})
	}
}

func TestCanceledContext(t *testing.T) {
	d, _ := newTestDisk(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	key := keyFor("ctx")
	if err := d.Put(ctx, key, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("Put err = %v", err)
	}
	if _, err := d.Get(ctx, key); !errors.Is(err, context.Canceled) {
		t.Errorf("Get err = %v", err)
	}
	if err := d.Delete(ctx, key); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete err = %v", err)
	}
}

func TestConcurrent(t *testing.T) {
	d, dir := newTestDisk(t)
	ctx := context.Background()
	shared := keyFor("shared")
	sharedData := bytes.Repeat([]byte("s"), 4096)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Put(ctx, shared, sharedData); err != nil {
				t.Errorf("Put shared: %v", err)
			}
			if got, err := d.Get(ctx, shared); err == nil && !bytes.Equal(got, sharedData) {
				t.Errorf("torn read of shared key: %d bytes", len(got))
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := keyFor("own-" + string(rune('A'+i)))
			data := bytes.Repeat([]byte{byte(i)}, 1000+i)
			if err := d.Put(ctx, k, data); err != nil {
				t.Errorf("Put own: %v", err)
				return
			}
			got, err := d.Get(ctx, k)
			if err != nil || !bytes.Equal(got, data) {
				t.Errorf("own round trip mismatch: %v", err)
			}
		}()
	}
	wg.Wait()
	if left := tempFiles(t, dir); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestNoTempFileAfterFailure(t *testing.T) {
	d, dir := newTestDisk(t)
	key := keyFor("fail")
	// A non-empty directory squats on the final name, so the rename fails
	// after the temp file was written.
	blocker := filepath.Join(dir, key[0:2], key[2:4], key)
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(context.Background(), key, []byte("data")); err == nil {
		t.Fatal("expected Put to fail")
	}
	if left := tempFiles(t, dir); len(left) != 0 {
		t.Errorf("temp files left after failure: %v", left)
	}
}

func TestLargeObject(t *testing.T) {
	d, _ := newTestDisk(t)
	ctx := context.Background()
	data := make([]byte, 3<<20)
	for i := range data {
		data[i] = byte(i * 7)
	}
	key := keyFor("large")
	if err := d.Put(ctx, key, data); err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(ctx, key)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("3 MiB round trip failed: %v (len %d)", err, len(got))
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	d, dir := newTestDisk(t)
	ctx := context.Background()
	outside := t.TempDir()
	key := keyFor("escape")

	// Shard dir replaced by a symlink pointing outside the root.
	if err := os.Symlink(outside, filepath.Join(dir, key[0:2])); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := d.Put(ctx, key, []byte("secret")); err == nil {
		t.Error("Put through escaping symlink succeeded")
	}
	// A file that exists outside must be unreachable.
	if err := os.MkdirAll(filepath.Join(outside, key[2:4]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, key[2:4], key), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := d.Get(ctx, key); err == nil {
		t.Errorf("Get read outside root: %q", got)
	}
	if err := d.Delete(ctx, key); err == nil {
		t.Error("Delete through escaping symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, key[2:4], key)); err != nil {
		t.Errorf("outside file was touched: %v", err)
	}
}

func TestNewDiskErrors(t *testing.T) {
	if _, err := NewDisk(""); err == nil {
		t.Error("empty root accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDisk(file); err == nil {
		t.Error("root that is a regular file accepted")
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := filepath.Join(t.TempDir(), "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		if _, err := NewDisk(ro); err == nil {
			t.Error("read-only root accepted")
		}
	}
}
