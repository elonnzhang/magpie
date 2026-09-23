package gui

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/update"
)

// updater keeps the app current. It asks the feed at start-up and every six
// hours; when a newer release is out and the app may replace its own
// bundle, the new one is downloaded and unpacked straight away, so all that
// is left is a restart. Quitting installs it too, and the next launch is
// the new version.
type updater struct {
	mu      sync.Mutex
	state   string // checking | latest | downloading | ready | available | source | error
	latest  *update.Release
	err     string
	bundle  string // the .app to replace, "" when not in one or not writable
	staged  string
	onReady func(version string)
}

type updateJSON struct {
	State   string `json:"state"`
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	Notes   string `json:"notes,omitempty"`
	URL     string `json:"url,omitempty"`
	Error   string `json:"error,omitempty"`
}

var updates = &updater{}

const updateEvery = 6 * time.Hour

func (u *updater) start() {
	if b := update.Bundle(); b != "" && update.Writable(filepath.Dir(b)) {
		u.bundle = b
	}
	go func() {
		time.Sleep(5 * time.Second) // let the app settle first
		for {
			u.check()
			time.Sleep(updateEvery)
		}
	}()
}

// check asks the feed and, when it can, stages the new version.
func (u *updater) check() {
	u.mu.Lock()
	if u.state == "checking" || u.state == "downloading" || u.state == "ready" {
		u.mu.Unlock()
		return
	}
	u.state, u.err = "checking", ""
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rel, err := update.Latest(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.state, u.err = "error", err.Error()
		return
	}
	u.latest = rel
	switch {
	case !update.Released(Version):
		u.state = "source"
		return
	case !update.Newer(rel.Version, Version):
		u.state = "latest"
		return
	case u.bundle == "":
		u.state = "available" // the user fetches it from the release page
		return
	}
	u.state = "downloading"
	u.mu.Unlock()
	staged, err := update.Stage(ctx, rel, u.bundle)
	u.mu.Lock()
	if err != nil {
		log.Println("update:", err)
		u.state, u.err = "error", err.Error()
		return
	}
	u.state, u.staged = "ready", staged
	if u.onReady != nil {
		go u.onReady(rel.Version)
	}
}

// install swaps the staged version in; it reports whether there was one.
func (u *updater) install() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.staged == "" {
		return false
	}
	if err := update.Install(u.staged, u.bundle); err != nil {
		log.Println("update:", err)
		u.state, u.err = "error", err.Error()
		return false
	}
	u.staged = ""
	return true
}

func (u *updater) json() updateJSON {
	u.mu.Lock()
	defer u.mu.Unlock()
	j := updateJSON{State: u.state, Current: Version, Error: u.err}
	if u.latest != nil {
		j.Latest, j.Notes, j.URL = u.latest.Version, u.latest.Notes, u.latest.URL
	}
	return j
}

func updateRoutes(mux *http.ServeMux, w Windows) {
	mux.HandleFunc("GET /api/update", func(rw http.ResponseWriter, r *http.Request) {
		writeJSON(rw, updates.json())
	})
	mux.HandleFunc("POST /api/update/check", func(rw http.ResponseWriter, r *http.Request) {
		updates.check()
		writeJSON(rw, updates.json())
	})
	// install restarts into the staged version; without one it opens the
	// release page instead.
	mux.HandleFunc("POST /api/update/install", func(rw http.ResponseWriter, r *http.Request) {
		j := updates.json()
		if j.State == "ready" && restartToUpdate() {
			rw.WriteHeader(http.StatusNoContent)
			go w.Quit()
			return
		}
		if j.URL != "" {
			w.OpenURL(j.URL)
		}
		writeJSON(rw, updates.json())
	})
}

// restartToUpdate installs the staged version and arranges for it to open
// once this process is gone; the caller then quits.
func restartToUpdate() bool {
	bundle := updates.bundle
	if !updates.install() {
		return false
	}
	if err := update.Relaunch(bundle); err != nil {
		log.Println("update:", err)
	}
	return true
}
