// Package humanauth implements human-mode Okta authentication for snow:
// PKCE loopback login (FR-011), device flow (FR-012), refresh with rotation
// (FR-015), a credential store interface (FR-016, D-l) and the TokenSource
// the HTTP layer uses. Tokens are never printed or logged.
package humanauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Credentials is one profile's stored session. It redacts itself under every
// fmt verb and in JSON; only the Store implementations see the values.
type Credentials struct {
	Issuer       string
	ClientID     string
	Subject      string
	Scope        string
	AccessToken  string
	IDToken      string
	RefreshToken string
	Expiry       time.Time
}

const redacted = "[redacted]"

// String redacts the tokens.
func (c Credentials) String() string {
	return fmt.Sprintf("credentials{issuer:%s subject:%s tokens:%s}", c.Issuer, c.Subject, redacted)
}

// GoString redacts the tokens.
func (c Credentials) GoString() string { return c.String() }

// Format redacts the tokens under every verb.
func (c Credentials) Format(f fmt.State, _ rune) { _, _ = fmt.Fprint(f, c.String()) }

// MarshalJSON redacts the tokens.
func (Credentials) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Store is the credential store behind which the OS keychain sits (FR-016).
// Implementations: MemoryStore (tests), FileStore (explicit --insecure-store)
// and fail-closed stubs for backends this build does not include.
type Store interface {
	// Kind names the backend for status output.
	Kind() string
	// Load returns ErrNotFound when the profile has no credentials.
	Load(profile string) (Credentials, error)
	// Save persists atomically.
	Save(profile string, c Credentials) error
	// Delete removes the profile's credentials; deleting nothing is not an error.
	Delete(profile string) error
}

// MemoryStore is the in-memory fake.
type MemoryStore struct {
	mu      sync.Mutex
	m       map[string]Credentials
	SaveErr error // test seam: fail Save
	DelErr  error // test seam: fail Delete
}

// NewMemoryStore returns an empty fake store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]Credentials{}} }

// Kind is "memory".
func (*MemoryStore) Kind() string { return "memory" }

// Load implements Store.
func (s *MemoryStore) Load(p string) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[p]
	if !ok {
		return Credentials{}, ErrNotFound
	}
	return c, nil
}

// Save implements Store.
func (s *MemoryStore) Save(p string, c Credentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SaveErr != nil {
		return s.SaveErr
	}
	s.m[p] = c
	return nil
}

// Delete implements Store.
func (s *MemoryStore) Delete(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DelErr != nil {
		return s.DelErr
	}
	delete(s.m, p)
	return nil
}

// StubStore is a backend that is not built into this binary: every operation
// fails closed with a clear error (D-l).
type StubStore struct{ Backend, Reason string }

// Kind returns the backend name.
func (s StubStore) Kind() string { return s.Backend }

func (s StubStore) err() error {
	return &StoreUnavailableError{Backend: s.Backend, Reason: s.Reason}
}

// Load fails closed.
func (s StubStore) Load(string) (Credentials, error) { return Credentials{}, s.err() }

// Save fails closed.
func (s StubStore) Save(string, Credentials) error { return s.err() }

// Delete fails closed.
func (s StubStore) Delete(string) error { return s.err() }

// SelectStore picks the platform backend. No real OS keychain backend is
// built yet, so every platform gets a fail-closed stub that names what is
// missing; insecure selects the 0600 file store at path instead.
func SelectStore(goos string, wsl, insecure bool, path string) Store {
	if insecure {
		return NewFileStore(path)
	}
	switch {
	case goos == "darwin":
		return StubStore{"macos-keychain", "the macOS Keychain backend is not built into this version"}
	case goos == "linux" && wsl:
		return StubStore{"wsl2-store", "the WSL2 credential store is not built into this version"}
	case goos == "linux":
		return StubStore{"secret-service", "the Linux Secret Service backend is not built into this version"}
	}
	return StubStore{goos + "-store", "no credential store backend exists for this platform"}
}

// DetectWSL reports whether the running Linux kernel is WSL.
func DetectWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// FileStore keeps all profiles in one JSON file with mode 0600. It exists
// only behind the explicit --insecure-store / SNOW_INSECURE_STORE opt-in.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a file store at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// DefaultInsecurePath is ~/.config/snow/credentials.json.
func DefaultInsecurePath(home string) string {
	return filepath.Join(home, ".config", "snow", "credentials.json")
}

// Kind is "insecure-file".
func (*FileStore) Kind() string { return "insecure-file" }

type fileRecord struct {
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	Scope        string    `json:"scope"`
	AccessToken  string    `json:"access_token"`
	IDToken      string    `json:"id_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

func (s *FileStore) read() (map[string]fileRecord, error) {
	m := map[string]fileRecord{}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("credential file %s is corrupt: %w", s.path, err)
	}
	return m, nil
}

func (s *FileStore) write(m map[string]fileRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// Load implements Store.
func (s *FileStore) Load(p string) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.read()
	if err != nil {
		return Credentials{}, err
	}
	r, ok := m[p]
	if !ok {
		return Credentials{}, ErrNotFound
	}
	return Credentials(r), nil
}

// Save implements Store with an atomic temp-file rename.
func (s *FileStore) Save(p string, c Credentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.read()
	if err != nil {
		return err
	}
	m[p] = fileRecord(c)
	return s.write(m)
}

// Delete implements Store.
func (s *FileStore) Delete(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := m[p]; !ok {
		return nil
	}
	delete(m, p)
	return s.write(m)
}
