package splitter

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	_ "golang.org/x/image/webp"
	"os"
	"path/filepath"
	"sync"

	xDraw "golang.org/x/image/draw"
)

var openLimeTypeMap = map[string]string{
	"HSH_RTI":  "hsh",
	"HSH":      "hsh",
	"LRGB_PTM": "ptm",
	"RGB_PTM":  "ptm",
	"IMAGE":    "ptm",
}

type openLimeMaterial struct {
	Scale []float64 `json:"scale"`
	Bias  []float64 `json:"bias"`
}

type openLimeDescriptor struct {
	Width      int                `json:"width"`
	Height     int                `json:"height"`
	Format     string             `json:"format"`
	Type       string             `json:"type"`
	Colorspace string             `json:"colorspace"`
	NPlanes    int                `json:"nplanes"`
	Materials  []openLimeMaterial `json:"materials"`
}

func openLimeCoefficients(imgType string, numLayers int) int {
	if imgType == "LRGB_PTM" || imgType == "RGB_PTM" {
		return 6
	}
	return numLayers
}

func openLimePlaneCount(imgType string, coefficients int) int {
	rtiType := openLimeTypeMap[imgType]
	if rtiType == "" {
		rtiType = "hsh"
	}
	if rtiType == "hsh" || rtiType == "ptm" {
		return coefficients * 3
	}
	return coefficients
}

func expandOpenLimeScalars(values []float64, nplanes int) []float64 {
	if len(values) == nplanes {
		return values
	}
	if len(values) > 0 && len(values)*3 == nplanes {
		expanded := make([]float64, 0, nplanes)
		for _, value := range values {
			expanded = append(expanded, value, value, value)
		}
		return expanded
	}

	fallback := 1.0
	if len(values) > 0 {
		fallback = values[len(values)-1]
	}
	padded := append([]float64(nil), values...)
	for len(padded) < nplanes {
		padded = append(padded, fallback)
	}
	return padded[:nplanes]
}

func buildDeepZoomDZI(width, height, tileSize int, format string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Image TileSize="%d" Overlap="0" Format="%s" xmlns="http://schemas.microsoft.com/deepzoom/2008">
  <Size Width="%d" Height="%d"/>
</Image>`, tileSize, format, width, height)
}

// SaveOpenLime writes a native OpenLIME DeepZoom export into <sourceFolder>/openlime/.
func (s *Splitter) SaveOpenLime(sourceFolder, format string) error {
	openlimeDir := filepath.Join(sourceFolder, "openlime")
	if err := os.MkdirAll(openlimeDir, 0755); err != nil {
		return err
	}

	imgType := s.image.Type()
	coefficients := openLimeCoefficients(imgType, s.image.NumLayers())
	rtiType := openLimeTypeMap[imgType]
	if rtiType == "" {
		rtiType = "hsh"
	}
	nplanes := openLimePlaneCount(imgType, coefficients)

	desc := openLimeDescriptor{
		Width:      s.image.Width(),
		Height:     s.image.Height(),
		Format:     format,
		Type:       rtiType,
		Colorspace: "rgb",
		NPlanes:    nplanes,
		Materials: []openLimeMaterial{{
			Scale: expandOpenLimeScalars(s.image.Scale(), nplanes),
			Bias:  expandOpenLimeScalars(s.image.Bias(), nplanes),
		}},
	}

	infoPath := filepath.Join(openlimeDir, "info.json")
	infoFile, err := os.Create(infoPath)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(infoFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(desc); err != nil {
		infoFile.Close()
		return err
	}
	infoFile.Close()

	numLayers := s.image.NumLayers()
	const border = 1

	var wg sync.WaitGroup
	errChan := make(chan error, numLayers*len(s.tree.Nodes))

	for layerIdx := 0; layerIdx < numLayers; layerIdx++ {
		planeName := fmt.Sprintf("plane_%d", layerIdx)
		dziPath := filepath.Join(openlimeDir, planeName+".dzi")
		if err := os.WriteFile(dziPath, []byte(buildDeepZoomDZI(s.image.Width(), s.image.Height(), s.tileSize, format)), 0644); err != nil {
			return err
		}

		filesDir := filepath.Join(openlimeDir, planeName+"_files")
		if err := os.MkdirAll(filesDir, 0755); err != nil {
			return err
		}

		for i := range s.tree.Nodes {
			node := s.tree.Nodes[i]
			if !node.Valid {
				continue
			}

			wg.Add(1)
			go func(layer int, tile Node) {
				defer wg.Done()

				levelDir := filepath.Join(filesDir, fmt.Sprintf("%d", tile.Level))
				if err := os.MkdirAll(levelDir, 0755); err != nil {
					errChan <- err
					return
				}

				srcPath := filepath.Join(sourceFolder, fmt.Sprintf("%d_%d.%s", tile.ID, layer+1, format))
				dstPath := filepath.Join(levelDir, fmt.Sprintf("%d_%d.%s", tile.GridX, tile.GridY, format))

				if err := exportOpenLimeTile(srcPath, dstPath, s.tileSize, border, format); err != nil {
					errChan <- fmt.Errorf("openlime tile %s: %w", dstPath, err)
				}
			}(layerIdx, node)
		}
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

func exportOpenLimeTile(srcPath, dstPath string, tileSize, border int, format string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcImg, _, err := image.Decode(srcFile)
	if err != nil {
		return err
	}

	bounds := srcImg.Bounds()
	cropRect := image.Rect(
		bounds.Min.X+border,
		bounds.Min.Y+border,
		bounds.Min.X+border+tileSize,
		bounds.Min.Y+border+tileSize,
	)
	cropRect = cropRect.Intersect(bounds)
	if cropRect.Empty() {
		return fmt.Errorf("empty crop for %s", srcPath)
	}

	dstImg := image.NewRGBA(image.Rect(0, 0, tileSize, tileSize))
	for i := 3; i < len(dstImg.Pix); i += 4 {
		dstImg.Pix[i] = 255
	}

	dstRect := image.Rect(0, 0, cropRect.Dx(), cropRect.Dy())
	xDraw.NearestNeighbor.Scale(dstImg, dstRect, srcImg, cropRect, xDraw.Src, nil)

	outFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	return EncodeTileImage(outFile, dstImg, format, 90)
}
