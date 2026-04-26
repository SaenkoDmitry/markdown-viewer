package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"markdown-viewer/internal/content"
)

type Session struct {
	ID         string
	RepoURL    string
	Branch     string
	ContentDir string
	Pages      map[string]*content.Page
	Nav        []content.NavItem
	CreatedAt  time.Time
}

var (
	store = make(map[string]*Session)
	mu    sync.RWMutex
)

func GenerateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func FindByRepo(repoURL string) *Session {
	mu.RLock()
	defer mu.RUnlock()
	for _, s := range store {
		if s.RepoURL == repoURL {
			return s
		}
	}
	return nil
}

func Get(id string) *Session {
	mu.RLock()
	defer mu.RUnlock()
	return store[id]
}

func All() map[string]*Session {
	mu.RLock()
	defer mu.RUnlock()
	result := make(map[string]*Session, len(store))
	for k, v := range store {
		result[k] = v
	}
	return result
}

func Create(repoURL string) (*Session, error) {
	if existing := FindByRepo(repoURL); existing != nil {
		log.Printf("Reusing existing session %s for %s", existing.ID, repoURL)
		return existing, nil
	}

	owner, repo, ok := content.ParseGitHubURL(repoURL)
	if !ok {
		return nil, fmt.Errorf("invalid URL")
	}

	branch, err := content.DetectBranch(owner, repo)
	if err != nil {
		branch = "main"
	}

	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("mdviewer-%d", time.Now().Unix()))
	if err := content.DownloadRepo(owner, repo, branch, tmpDir); err != nil {
		return nil, err
	}

	foundDir := content.FindMarkdownDir(tmpDir)
	if foundDir == "" {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("no markdown files found")
	}

	sessID := GenerateID()
	prefix := "/s/" + sessID + "/"

	pages, nav, err := content.Load(foundDir, prefix)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}

	sess := &Session{
		ID:         sessID,
		RepoURL:    repoURL,
		Branch:     branch,
		ContentDir: tmpDir,
		Pages:      pages,
		Nav:        nav,
		CreatedAt:  time.Now(),
	}

	for _, p := range sess.Pages {
		p.Sidebar = sess.Nav
	}

	mu.Lock()
	store[sess.ID] = sess
	mu.Unlock()

	SaveToDisk()

	return sess, nil
}

func RestoreFromDisk() error {
	items, err := LoadFromDisk()
	if err != nil {
		return err
	}

	for _, item := range items {
		if _, err := os.Stat(item.Dir); os.IsNotExist(err) {
			continue
		}

		pages, nav, err := content.Load(item.Dir, "/s/"+item.ID+"/")
		if err != nil {
			continue
		}

		sess := &Session{
			ID:         item.ID,
			RepoURL:    item.RepoURL,
			Branch:     item.Branch,
			ContentDir: item.Dir,
			Pages:      pages,
			Nav:        nav,
			CreatedAt:  time.Unix(item.CreatedAt, 0),
		}
		for _, p := range sess.Pages {
			p.Sidebar = sess.Nav
		}

		mu.Lock()
		store[sess.ID] = sess
		mu.Unlock()

		log.Printf("Restored session: %s (%d pages)", sess.ID, len(sess.Pages))
	}
	return nil
}
