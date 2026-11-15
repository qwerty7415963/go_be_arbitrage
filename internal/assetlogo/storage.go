package assetlogo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	_ "golang.org/x/image/webp"
)

// LogoStorage abstracts persistence of logo bytes.
type LogoStorage interface {
	Put(ctx context.Context, assetID string, data []byte, contentType string) (storageKey string, err error)
	Path(assetID, ext string) string
	Delete(ctx context.Context, storageKey string) error
}

// LocalStorage stores logos on the local filesystem.
type LocalStorage struct {
	BaseDir string
}

func NewLocalStorage(dir string) *LocalStorage {
	if dir == "" {
		dir = "./data/logos"
	}
	return &LocalStorage{BaseDir: dir}
}

var allowedMIME = map[string]string{
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/jpeg": ".jpg",
	"image/jpg":  ".jpg",
}

const maxLogoBytes = 512 * 1024

func validateAssetID(assetID string) error {
	if _, err := uuid.Parse(assetID); err != nil {
		return fmt.Errorf("invalid asset id: %w", err)
	}
	if strings.ContainsAny(assetID, "/\\") {
		return errors.New("asset id contains path separator")
	}
	return nil
}

func ValidateImage(data []byte, contentType string) error {
	ct := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	ext, ok := allowedMIME[ct]
	if !ok {
		return fmt.Errorf("unsupported content type: %s", contentType)
	}
	if len(data) == 0 {
		return errors.New("empty image data")
	}
	if len(data) > maxLogoBytes {
		return fmt.Errorf("image too large: %d bytes", len(data))
	}
	// magic bytes
	if err := checkMagicBytes(data, ct); err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(bytesReader(data))
	if err != nil {
		return fmt.Errorf("decode image config: %w", err)
	}
	if cfg.Width < 16 || cfg.Width > 1024 || cfg.Height < 16 || cfg.Height > 1024 {
		return fmt.Errorf("invalid dimensions: %dx%d", cfg.Width, cfg.Height)
	}
	_ = ext
	return nil
}

func ValidateLogo(data []byte, contentType string) error { return ValidateImage(data, contentType) }

func checkMagicBytes(data []byte, ct string) error {
	switch ct {
	case "image/png":
		if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
			return errors.New("invalid png magic bytes")
		}
	case "image/jpeg", "image/jpg":
		if len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff {
			return errors.New("invalid jpeg magic bytes")
		}
	case "image/webp":
		if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return errors.New("invalid webp magic bytes")
		}
	}
	return nil
}

func bytesReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}

// extForContentType maps a mime type to a file extension.
func extForContentType(contentType string) string {
	ct := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ext, ok := allowedMIME[ct]; ok {
		return ext
	}
	return ".bin"
}

func (s *LocalStorage) Path(assetID, ext string) string {
	if err := validateAssetID(assetID); err != nil {
		return ""
	}
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	// sanitize ext
	ext = filepath.Base(ext)
	if ext == "." || ext == "/" {
		ext = ""
	}
	return filepath.Join(s.BaseDir, assetID+ext)
}

func (s *LocalStorage) Put(ctx context.Context, assetID string, data []byte, contentType string) (string, error) {
	if err := validateAssetID(assetID); err != nil {
		return "", err
	}
	if err := ValidateImage(data, contentType); err != nil {
		return "", err
	}
	ext := extForContentType(contentType)
	path := s.Path(assetID, ext)
	if path == "" {
		return "", errors.New("invalid path")
	}
	if err := os.MkdirAll(s.BaseDir, 0755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(s.BaseDir, assetID+"-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return "", err
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return "", err
	}
	// ensure final mode
	_ = os.Chmod(path, 0644)
	return path, nil
}

func (s *LocalStorage) Delete(ctx context.Context, storageKey string) error {
	if storageKey == "" {
		return errors.New("empty storage key")
	}
	baseAbs, _ := filepath.Abs(s.BaseDir)
	keyAbs, _ := filepath.Abs(storageKey)
	if keyAbs != baseAbs && !strings.HasPrefix(keyAbs, baseAbs+string(os.PathSeparator)) {
		return errors.New("path traversal detected")
	}
	// validate basename is uuid-like
	base := filepath.Base(storageKey)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if err := validateAssetID(name); err != nil {
		return fmt.Errorf("invalid storage key: %w", err)
	}
	return os.Remove(storageKey)
}

// --- download host allowlist ---
var AllowedDownloadHosts = map[string]bool{
	"raw.githubusercontent.com": true,
	"img.logo.dev":              true,
	"api.logo.dev":              true,
}

func ValidateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("invalid scheme: %s", u.Scheme)
	}
	if !AllowedDownloadHosts[u.Hostname()] {
		return fmt.Errorf("host not allowed: %s", u.Hostname())
	}
	return nil
}

func NewSafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if !AllowedDownloadHosts[req.URL.Hostname()] {
				return fmt.Errorf("redirect to disallowed host: %s", req.URL.Hostname())
			}
			return nil
		},
	}
}

func DownloadLogo(ctx context.Context, rawURL string) ([]byte, string, error) {
	if err := ValidateDownloadURL(rawURL); err != nil {
		return nil, "", err
	}
	client := NewSafeHTTPClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return b, resp.Header.Get("Content-Type"), nil
}

func ContentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// sha256 helper exposed for resolver
func sha256Hex(data []byte) string { return ContentHash(data) }

// avoid unused
var _ = time.Second
var _ = uuid.New
var _ = http.DefaultClient
