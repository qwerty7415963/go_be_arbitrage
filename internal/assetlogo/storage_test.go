package assetlogo

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLocalStorage_Put_Path_Delete(t *testing.T) {
	dir := t.TempDir()
	s := NewLocalStorage(dir)
	assetID := "11111111-2222-3333-4444-555555555555"
	data := makePNG(t)

	key, err := s.Put(context.Background(), assetID, data, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if key != s.Path(assetID, ".png") {
		t.Fatalf("key %s != path %s", key, s.Path(assetID, ".png"))
	}
	if _, err := os.Stat(key); err != nil {
		t.Fatal(err)
	}
	_ = key
	if err := s.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestLocalStorage_PathTraversal(t *testing.T) {
	s := NewLocalStorage(t.TempDir())
	if s.Path("../etc/passwd", ".png") != "" {
		t.Fatal("expected empty")
	}
	err := s.Delete(context.Background(), "/etc/passwd")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateImage_MIMEAndSize(t *testing.T) {
	if err := ValidateImage([]byte("x"), "image/svg+xml"); err == nil {
		t.Fatal("expected reject svg")
	}
	if err := ValidateImage(make([]byte, maxLogoBytes+1), "image/png"); err == nil {
		t.Fatal("expected size error")
	}
	data := makePNG(t)
	if err := ValidateImage(data, "image/png"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDownloadURL(t *testing.T) {
	if err := ValidateDownloadURL("https://example.com/x"); err == nil {
		t.Fatal("expected reject host")
	}
	if err := ValidateDownloadURL("https://raw.githubusercontent.com/x"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDownloadURL("https://api.logo.dev/x"); err != nil {
		t.Fatal(err)
	}
}
