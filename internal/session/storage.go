package session

import (
	"encoding/json"
	"os"
)

const sessionsFile = "sessions.json"

type storedSession struct {
	ID        string `json:"id"`
	RepoURL   string `json:"repo_url"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commit_sha"`
	Dir       string `json:"dir"`
	CreatedAt int64  `json:"created_at"`
}

func SaveToDisk() {
	mu.RLock()
	defer mu.RUnlock()

	var list []storedSession
	for _, s := range store {
		list = append(list, storedSession{
			ID:        s.ID,
			RepoURL:   s.RepoURL,
			Branch:    s.Branch,
			CommitSHA: s.CommitSHA,
			Dir:       s.ContentDir,
			CreatedAt: s.CreatedAt.Unix(),
		})
	}

	data, _ := json.MarshalIndent(list, "", "  ")
	os.WriteFile(sessionsFile, data, 0644)
}

func LoadFromDisk() ([]storedSession, error) {
	data, err := os.ReadFile(sessionsFile)
	if err != nil {
		return nil, err
	}

	var list []storedSession
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}
