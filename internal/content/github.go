package content

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func ParseGitHubURL(rawURL string) (owner, repo string, ok bool) {
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+?)(?:\.git|/|$)`)
	m := re.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSuffix(m[2], ".git"), true
}

func DetectBranch(owner, repo string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var data struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.DefaultBranch == "" {
		return "main", nil
	}
	return data.DefaultBranch, nil
}

// GetLatestCommit возвращает SHA последнего коммита в ветке
func GetLatestCommit(owner, repo, branch string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, branch)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var data struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.SHA == "" {
		return "", fmt.Errorf("empty commit SHA")
	}
	return data.SHA, nil
}

// DownloadRepo скачивает репозиторий и возвращает SHA коммита.
// Если dest уже существует и содержит актуальный коммит — пропускает скачивание.
func DownloadRepo(owner, repo, branch, dest string) (commitSHA string, err error) {
	commitSHA, err = GetLatestCommit(owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("failed to get latest commit: %w", err)
	}

	// Проверяем, есть ли уже актуальная версия
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		commitFile := filepath.Join(dest, ".commit-sha")
		if data, err := os.ReadFile(commitFile); err == nil {
			if strings.TrimSpace(string(data)) == commitSHA {
				log.Printf("Repository %s/%s@%s already up to date (commit: %s)", owner, repo, branch, commitSHA[:7])
				return commitSHA, nil
			}
			log.Printf("Commit changed: %s -> %s", strings.TrimSpace(string(data))[:7], commitSHA[:7])
		}
	}

	// Удаляем старую версию и создаём директорию заново
	os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0755); err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", owner, repo, branch)
	log.Printf("Downloading %s/%s@%s (commit: %s)...", owner, repo, branch, commitSHA[:7])

	client := &http.Client{Timeout: 2 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if branch != "master" {
			log.Printf("Branch %s not found, trying master...", branch)
			return DownloadRepo(owner, repo, "master", dest)
		}
		return "", fmt.Errorf("github returned %d", resp.StatusCode)
	}

	if resp.ContentLength > 50*1024*1024 {
		return "", fmt.Errorf("repository too large: %d bytes", resp.ContentLength)
	}

	limitedBody := io.LimitReader(resp.Body, 50*1024*1024)

	tmpFile, err := os.CreateTemp("", "repo-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, limitedBody); err != nil {
		return "", err
	}
	tmpFile.Close()

	if err := unzip(tmpFile.Name(), dest); err != nil {
		return "", err
	}

	// Сохраняем SHA коммита
	commitFile := filepath.Join(dest, ".commit-sha")
	if err := os.WriteFile(commitFile, []byte(commitSHA), 0644); err != nil {
		log.Printf("Warning: failed to write commit file: %v", err)
	}

	return commitSHA, nil
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		fpath := filepath.Join(dest, parts[1])

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
