package splitter

import (
	"image"
	"os"
	"path/filepath"
	"testing"
)

type mockImage struct {
	w         int
	h         int
	numLayers int
	layers    []image.Image
}

func (m *mockImage) Width() int {
	return m.w
}

func (m *mockImage) Height() int {
	return m.h
}

func (m *mockImage) NumLayers() int {
	return m.numLayers
}

func (m *mockImage) GetLayer(layerIdx int) (image.Image, error) {
	return m.layers[layerIdx], nil
}

func (m *mockImage) Type() string {
	return "IMAGE"
}

func (m *mockImage) Scale() []float64 {
	return nil
}

func (m *mockImage) Bias() []float64 {
	return nil
}

func TestWritePyramidalTiff(t *testing.T) {
	// Create a simple mock image (500x500, 3 channels standard RGB image)
	rgba := image.NewRGBA(image.Rect(0, 0, 500, 500))
	// Draw a simple pattern
	for y := 0; y < 500; y++ {
		for x := 0; x < 500; x++ {
			idx := rgba.PixOffset(x, y)
			rgba.Pix[idx] = byte(x % 256)
			rgba.Pix[idx+1] = byte(y % 256)
			rgba.Pix[idx+2] = byte((x + y) % 256)
			rgba.Pix[idx+3] = 255
		}
	}

	mock := &mockImage{
		w:         500,
		h:         500,
		numLayers: 1,
		layers:    []image.Image{rgba},
	}

	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "output.tif")

	err := WritePyramidalTiff(mock, tempFile, "")
	if err != nil {
		t.Fatalf("WritePyramidalTiff failed: %v", err)
	}

	// Verify file size and header
	info, err := os.Stat(tempFile)
	if err != nil {
		t.Fatalf("failed to stat generated TIFF file: %v", err)
	}
	if info.Size() < 8 {
		t.Fatalf("TIFF file is too small: %d bytes", info.Size())
	}

	f, err := os.Open(tempFile)
	if err != nil {
		t.Fatalf("failed to open generated TIFF file: %v", err)
	}
	defer f.Close()

	header := make([]byte, 8)
	if _, err := f.Read(header); err != nil {
		t.Fatalf("failed to read TIFF header: %v", err)
	}

	// Check Little Endian 'II' format and magic 42
	if header[0] != 'I' || header[1] != 'I' {
		t.Errorf("expected little endian 'II' header, got '%c%c'", header[0], header[1])
	}
	if header[2] != 42 || header[3] != 0 {
		t.Errorf("expected magic number 42, got %d", int(header[2])+int(header[3])<<8)
	}
}
