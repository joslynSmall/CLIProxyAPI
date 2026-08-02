package watcher

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestSyncAuthFilePrimesWatcherWithoutRepersisting(t *testing.T) {
	authDir := t.TempDir()
	authPath := filepath.Join(authDir, "codex-sync.json")
	if err := os.WriteFile(authPath, []byte(`{"type":"codex","access_token":"test"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	store := &stubStore{}
	w := &Watcher{
		authDir:         authDir,
		config:          &config.Config{AuthDir: authDir},
		currentAuths:    make(map[string]*coreauth.Auth),
		lastAuthHashes:  make(map[string]string),
		fileAuthsByPath: make(map[string]map[string]*coreauth.Auth),
		storePersister:  store,
	}

	auths, err := w.SyncAuthFile(authPath)
	if err != nil {
		t.Fatalf("SyncAuthFile() error = %v", err)
	}
	if len(auths) != 1 || auths[0].ID != filepath.Base(authPath) {
		t.Fatalf("normalized auths = %+v, want one auth with file ID", auths)
	}
	if got := atomic.LoadInt32(&store.authPersisted); got != 0 {
		t.Fatalf("SyncAuthFile() persisted auth %d times, want 0", got)
	}

	// A later fsnotify write event sees the primed hash and must not persist again.
	w.addOrUpdateClient(authPath)
	if got := atomic.LoadInt32(&store.authPersisted); got != 0 {
		t.Fatalf("unchanged watcher event persisted auth %d times, want 0", got)
	}
}
