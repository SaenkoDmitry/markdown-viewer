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

func DownloadRepo(owner, repo, branch, dest string) error {
	os.RemoveAll(dest)
	os.MkdirAll(dest, 0755)

	url := fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", owner, repo, branch)
	log.Printf("Downloading: %s", url)

	// Client с таймаутом
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if branch != "master" {
			log.Printf("Branch %s not found, trying master...", branch)
			return DownloadRepo(owner, repo, "master", dest)
		}
		return fmt.Errorf("github returned %d", resp.StatusCode)
	}

	// Проверяем Content-Length
	if resp.ContentLength > 50*1024*1024 {
		return fmt.Errorf("repository too large: %d bytes", resp.ContentLength)
	}

	// Ограничиваем чтение
	limitedBody := io.LimitReader(resp.Body, 50*1024*1024)

	tmpFile, err := os.CreateTemp("", "repo-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, limitedBody); err != nil {
		return err
	}
	tmpFile.Close()

	return unzip(tmpFile.Name(), dest)
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
