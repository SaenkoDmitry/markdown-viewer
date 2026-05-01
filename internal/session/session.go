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
	CommitSHA  string // ← новое поле
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
	owner, repo, ok := content.ParseGitHubURL(repoURL)
	if !ok {
		return nil, fmt.Errorf("invalid URL")
	}

	branch, err := content.DetectBranch(owner, repo)
	if err != nil {
		branch = "main"
	}

	// Проверяем коммит ДО создания сессии
	commitSHA, err := content.GetLatestCommit(owner, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to detect commit: %w", err)
	}

	// Проверяем существующую сессию: если коммит совпадает — переиспользуем
	if existing := FindByRepo(repoURL); existing != nil {
		if existing.CommitSHA == commitSHA {
			log.Printf("Reusing existing session %s for %s (commit: %s)", existing.ID, repoURL, commitSHA[:7])
			return existing, nil
		}
		log.Printf("Commit changed for %s, creating new session...", repoURL)
		// Старая сессия остаётся в памяти, но при следующем запросе создастся новая
	}

	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("mdviewer-%d", time.Now().Unix()))
	commitSHA, err = content.DownloadRepo(owner, repo, branch, tmpDir)
	if err != nil {
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
		CommitSHA:  commitSHA, // ← сохраняем хэш
		ContentDir: tmpDir,
		Pages:      pages,
		Nav:        nav,
		CreatedAt:  time.Now(),
	}

	for _, p := range sess.Pages {
		p.Sidebar = sess.Nav
		p.SessionID = sess.ID
		p.RepoURL = sess.RepoURL
		p.Branch = sess.Branch
		p.PagesCount = len(sess.Pages)
	}

	mu.Lock()
	store[sess.ID] = sess
	mu.Unlock()

	SaveToDisk()

	log.Printf("Created session %s for %s (%d pages, commit: %s)", sess.ID, repoURL, len(sess.Pages), commitSHA[:7])

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

		owner, repo, ok := content.ParseGitHubURL(item.RepoURL)
		if !ok {
			continue
		}

		// Проверяем актуальность коммита при восстановлении
		latestCommit, err := content.GetLatestCommit(owner, repo, item.Branch)
		if err == nil && latestCommit != item.CommitSHA {
			log.Printf("Session %s outdated (%s -> %s), re-downloading...", item.ID, item.CommitSHA[:7], latestCommit[:7])

			// Обновляем репозиторий
			newCommit, err := content.DownloadRepo(owner, repo, item.Branch, item.Dir)
			if err != nil {
				log.Printf("Failed to update session %s: %v", item.ID, err)
				continue
			}
			item.CommitSHA = newCommit
		}

		pages, nav, err := content.Load(item.Dir, "/s/"+item.ID+"/")
		if err != nil {
			continue
		}

		sess := &Session{
			ID:         item.ID,
			RepoURL:    item.RepoURL,
			Branch:     item.Branch,
			CommitSHA:  item.CommitSHA,
			ContentDir: item.Dir,
			Pages:      pages,
			Nav:        nav,
			CreatedAt:  time.Unix(item.CreatedAt, 0),
		}
		for _, p := range sess.Pages {
			p.Sidebar = sess.Nav
			p.SessionID = sess.ID
			p.RepoURL = sess.RepoURL
			p.Branch = sess.Branch
			p.PagesCount = len(sess.Pages)
		}

		mu.Lock()
		store[sess.ID] = sess
		mu.Unlock()

		log.Printf("Restored session: %s (%d pages, commit: %s)", sess.ID, len(sess.Pages), sess.CommitSHA[:7])
	}
	return nil
}
