package splitter

import (
	"bytes"
	"image"
	"testing"
)

func TestNormalizeTileFormat(t *testing.T) {
	tests := map[string]string{
		"jpg":   "jpg",
		"JPG":   "jpg",
		"jpeg":  "jpg",
		"png":   "png",
		"webp":  "webp",
		"WEBP":  "webp",
		"other": "jpg",
	}

	for input, want := range tests {
		if got := NormalizeTileFormat(input); got != want {
			t.Fatalf("NormalizeTileFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEncodeTileImageFormats(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))

	for _, format := range []string{"jpg", "png", "webp"} {
		var buf bytes.Buffer
		if err := EncodeTileImage(&buf, img, format, 80); err != nil {
			t.Fatalf("EncodeTileImage(%s) failed: %v", format, err)
		}
		if buf.Len() == 0 {
			t.Fatalf("EncodeTileImage(%s) produced empty output", format)
		}
	}
}
