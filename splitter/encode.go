package splitter

import (
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"

	"github.com/KarpelesLab/gowebp"
)

// NormalizeTileFormat maps user-facing format names to rtiprep tile extensions.
func NormalizeTileFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		return "png"
	case "webp":
		return "webp"
	case "jpeg":
		return "jpg"
	default:
		return "jpg"
	}
}

// EncodeTileImage writes a tile image in the requested format.
func EncodeTileImage(w io.Writer, img image.Image, format string, quality int) error {
	switch NormalizeTileFormat(format) {
	case "png":
		return png.Encode(w, img)
	case "webp":
		if quality < 0 {
			quality = 0
		}
		if quality > 100 {
			quality = 100
		}
		return gowebp.Encode(w, img, &gowebp.Options{
			Lossy:   true,
			Quality: float32(quality),
		})
	default:
		if quality < 1 {
			quality = 1
		}
		if quality > 100 {
			quality = 100
		}
		return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	}
}
