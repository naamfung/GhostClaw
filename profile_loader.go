package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Profile stores all profile-related content loaded from files.
type Profile struct {
	User     string            // profiles/USER.md
	Soul     string            // profiles/SOUL.md
	Agent    string            // profiles/AGENT.md
	ToolsDoc string            // profiles/TOOLS.md
	Actors   map[string]string // actor_name -> IDENTITY.md content
	mu       sync.RWMutex
}

// ProfileLoader monitors profiles/ directory and hot-reloads profile files.
type ProfileLoader struct {
	profilesDir string
	actorsDir   string
	watcher     *fsnotify.Watcher
	profile     Profile
	mu          sync.RWMutex
	stopCh      chan struct{}
}

// NewProfileLoader creates a ProfileLoader that watches the given directory.
func NewProfileLoader(profilesDir string) (*ProfileLoader, error) {
	actorsDir := filepath.Join(profilesDir, "actors")

	// Ensure directories exist
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(actorsDir, 0755); err != nil {
		return nil, err
	}

	pl := &ProfileLoader{
		profilesDir: profilesDir,
		actorsDir:   actorsDir,
		stopCh:      make(chan struct{}),
		profile: Profile{
			Actors: make(map[string]string),
		},
	}

	// Initial load
	pl.loadAll()

	// Set up watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Warning: failed to create profile watcher: %v", err)
		return pl, nil
	}
	pl.watcher = watcher

	// Watch profiles directory
	if err := watcher.Add(profilesDir); err != nil {
		log.Printf("Warning: failed to watch profiles dir: %v", err)
	}

	// Watch actors directory
	if err := watcher.Add(actorsDir); err != nil {
		log.Printf("Warning: failed to watch actors dir: %v", err)
	}

	go pl.watchLoop()

	log.Printf("Profile loader started. Watching: %s", profilesDir)
	return pl, nil
}

// loadAll loads all profile files from disk.
func (pl *ProfileLoader) loadAll() {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	pl.profile.User = loadFileContent(filepath.Join(pl.profilesDir, "USER.md"))
	pl.profile.Soul = loadFileContent(filepath.Join(pl.profilesDir, "SOUL.md"))
	pl.profile.Agent = loadFileContent(filepath.Join(pl.profilesDir, "AGENT.md"))
	pl.profile.ToolsDoc = loadFileContent(filepath.Join(pl.profilesDir, "TOOLS.md"))

	// Load actor-specific IDENTITY.md files
	pl.loadActorIdentities()
}

// loadActorIdentities scans actors/ subdirectories for IDENTITY.md files.
func (pl *ProfileLoader) loadActorIdentities() {
	// Clear existing
	pl.profile.Actors = make(map[string]string)

	entries, err := os.ReadDir(pl.actorsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		actorName := entry.Name()
		identityPath := filepath.Join(pl.actorsDir, actorName, "IDENTITY.md")
		content := loadFileContent(identityPath)
		if content != "" {
			pl.profile.Actors[actorName] = content
		}
	}
}

// watchLoop watches for file changes and reloads.
func (pl *ProfileLoader) watchLoop() {
	for {
		select {
		case <-pl.stopCh:
			return
		case event, ok := <-pl.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				// Use file name to determine what to reload
				filename := filepath.Base(event.Name)
				dir := filepath.Dir(event.Name)

				if dir == pl.actorsDir || filepath.Dir(dir) == pl.actorsDir {
					// Actor identity change
					pl.mu.Lock()
					pl.loadActorIdentities()
					pl.mu.Unlock()
				} else {
					// Top-level profile file change
					pl.mu.Lock()
					switch filename {
					case "USER.md":
						pl.profile.User = loadFileContent(filepath.Join(pl.profilesDir, "USER.md"))
					case "SOUL.md":
						pl.profile.Soul = loadFileContent(filepath.Join(pl.profilesDir, "SOUL.md"))
					case "AGENT.md":
						pl.profile.Agent = loadFileContent(filepath.Join(pl.profilesDir, "AGENT.md"))
					case "TOOLS.md":
						pl.profile.ToolsDoc = loadFileContent(filepath.Join(pl.profilesDir, "TOOLS.md"))
					}
					pl.mu.Unlock()
				}

				if IsDebug {
					log.Printf("[ProfileLoader] Reloaded: %s", event.Name)
				}
			}
		case err, ok := <-pl.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[ProfileLoader] Watcher error: %v", err)
		}
	}
}

// GetProfile returns a copy of the current Profile.
func (pl *ProfileLoader) GetProfile() *Profile {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	p := &Profile{
		User:     pl.profile.User,
		Soul:     pl.profile.Soul,
		Agent:    pl.profile.Agent,
		ToolsDoc: pl.profile.ToolsDoc,
		Actors:   make(map[string]string),
	}
	for k, v := range pl.profile.Actors {
		p.Actors[k] = v
	}
	return p
}

// Stop stops the watcher.
func (pl *ProfileLoader) Stop() {
	close(pl.stopCh)
	if pl.watcher != nil {
		pl.watcher.Close()
	}
}

// loadFileContent reads a file and returns its content, or empty string if not found.
func loadFileContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// NOTE: globalProfileLoader is declared in main.go, not here.
