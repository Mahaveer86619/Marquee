package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// posterHosts are the only hosts posters are downloaded from.
var posterHosts = map[string]bool{
	"image.tmdb.org":    true,
	"static.tvmaze.com": true,
}

// Poster returns a title's poster image and its content type. Images are
// downloaded once and then served from PosterCacheDir (when set).
func (s *Service) Poster(ctx context.Context, rawRef string) ([]byte, string, error) {
	title, err := s.Title(ctx, rawRef)
	if err != nil {
		return nil, "", err
	}
	if title.PosterURL == "" {
		return nil, "", fmt.Errorf("%w: %s has no poster", ErrNotFound, rawRef)
	}
	u, err := url.Parse(title.PosterURL)
	if err != nil || u.Scheme != "https" || !posterHosts[u.Hostname()] {
		return nil, "", fmt.Errorf("%w: poster URL not allowed", ErrNotFound)
	}

	var cacheFile string
	if s.PosterCacheDir != "" {
		sum := sha256.Sum256([]byte(title.PosterURL))
		cacheFile = filepath.Join(s.PosterCacheDir, hex.EncodeToString(sum[:16])+filepath.Ext(u.Path))
		if data, err := os.ReadFile(cacheFile); err == nil {
			return data, contentTypeFor(cacheFile, data), nil
		}
	}

	data, ctype, err := getBytes(ctx, s.imageClient, title.PosterURL)
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(ctype, "image/") {
		ctype = http.DetectContentType(data)
		if !strings.HasPrefix(ctype, "image/") {
			return nil, "", errors.New("poster download did not return an image")
		}
	}
	if cacheFile != "" {
		if err := os.MkdirAll(s.PosterCacheDir, 0o755); err == nil {
			tmp := cacheFile + ".tmp"
			if os.WriteFile(tmp, data, 0o644) == nil {
				_ = os.Rename(tmp, cacheFile)
			}
		}
	}
	return data, ctype, nil
}

func contentTypeFor(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	}
	return http.DetectContentType(data)
}
