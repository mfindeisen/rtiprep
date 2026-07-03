package splitter

import (
	stdDraw "image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"rtiprep/rti"

	"encoding/json"
	"fmt"
	"image"
	"runtime"

	xDraw "golang.org/x/image/draw"
)

// Splitter handles slicing the multilayer image into pyramidal tiles
type Splitter struct {
	image    rti.MultiLayerImage
	tree     *Tree
	tileSize int
}

// NewSplitter creates a new Splitter instance
func NewSplitter(img rti.MultiLayerImage, tileSize int) *Splitter {
	tree := BuildTree(img.Width(), img.Height(), tileSize)
	return &Splitter{
		image:    img,
		tree:     tree,
		tileSize: tileSize,
	}
}

// NumLevels returns the number of levels in the quadtree pyramid
func (s *Splitter) NumLevels() int {
	return s.tree.NLevels
}

// MaxSize returns the maximum dimension of the padded quadtree canvas
func (s *Splitter) MaxSize() int {
	return s.tree.MaxSize
}


// Split processes each layer, generates the pyramid, and saves the tile files in parallel
func (s *Splitter) Split(destFolder string, quality int, format string) error {
	numLayers := s.image.NumLayers()

	// Ensure destination directory exists
	if err := os.MkdirAll(destFolder, 0755); err != nil {
		return err
	}

	// Limit concurrency to NumCPU (at least 1)
	limit := runtime.NumCPU()
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)

	// Process layers concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, numLayers)

	for l := 0; l < numLayers; l++ {
		wg.Add(1)
		go func(layerIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.splitLayer(layerIdx, destFolder, quality, format); err != nil {
				errChan <- fmt.Errorf("error in layer %d: %w", layerIdx, err)
			}
		}(l)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any occurred
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Splitter) splitLayer(layerIdx int, destFolder string, quality int, format string) error {
	layerImg, err := s.image.GetLayer(layerIdx)
	if err != nil {
		return err
	}

	// Create virtual canvas of size maxSize x maxSize
	canvas := image.NewRGBA(image.Rect(0, 0, s.tree.MaxSize, s.tree.MaxSize))

	// Fill canvas with black opaque pixels
	for i := 3; i < len(canvas.Pix); i += 4 {
		canvas.Pix[i] = 255
	}

	// Draw the layer image centered on the canvas
	stdDraw.Draw(canvas, s.tree.ImgRect, layerImg, image.Point{0, 0}, stdDraw.Src)

	// Process all valid nodes recursively/iteratively
	for i := 0; i < len(s.tree.Nodes); i++ {
		node := s.tree.Nodes[i]
		if !node.Valid {
			continue
		}

		// Extract padded sub-image
		cropped := cropAndPad(canvas, node.PaddedImgBox)

		// Resize to tileSize+2 x tileSize+2 (e.g. 258 x 258)
		destSize := s.tileSize + 2
		resized := image.NewRGBA(image.Rect(0, 0, destSize, destSize))
		xDraw.BiLinear.Scale(resized, resized.Bounds(), cropped, cropped.Bounds(), xDraw.Src, nil)

		// Save file as destFolder/nodeIndex_layerIndex.format (nodeIndex is 1-based, layerIndex is 1-based)
		fileName := filepath.Join(destFolder, fmt.Sprintf("%d_%d.%s", node.ID, layerIdx+1, format))
		outFile, err := os.Create(fileName)
		if err != nil {
			return err
		}

		if format == "png" {
			err = png.Encode(outFile, resized)
		} else {
			err = jpeg.Encode(outFile, resized, &jpeg.Options{Quality: quality})
		}
		outFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// cropAndPad extracts the specified box from the canvas, padding out-of-bounds with black pixels
func cropAndPad(src image.Image, rect image.Rectangle) *image.RGBA {
	dest := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	// Fill with opaque black (A=255)
	for i := 3; i < len(dest.Pix); i += 4 {
		dest.Pix[i] = 255
	}
	// Copy src to dest shifted by rect.Min
	stdDraw.Draw(dest, dest.Bounds(), src, rect.Min, stdDraw.Src)
	return dest
}

// SaveDescriptor JSON-serializes the quadtree structure and content metadata
func (s *Splitter) SaveDescriptor(destFolder string, format string) error {
	filePath := filepath.Join(destFolder, "info.json")

	// Determine coefficients count for the content tag based on type
	coefs := s.image.NumLayers()
	if s.image.Type() == "LRGB_PTM" || s.image.Type() == "RGB_PTM" {
		coefs = 6
	}

	type ContentInfo struct {
		Type         string    `json:"type"`
		Width        int       `json:"width"`
		Height       int       `json:"height"`
		Coefficients int       `json:"coefficients"`
		Scale        []float64 `json:"scale,omitempty"`
		Bias         []float64 `json:"bias,omitempty"`
	}

	type TreeInfo struct {
		TileSize int    `json:"tileSize"`
		MaxSize  int    `json:"maxSize"`
		Nodes    []Node `json:"nodes"`
	}

	type Descriptor struct {
		Format  string      `json:"format"`
		Content ContentInfo `json:"content"`
		Tree    TreeInfo    `json:"tree"`
	}

	desc := Descriptor{
		Format: format,
		Content: ContentInfo{
			Type:         s.image.Type(),
			Width:        s.image.Width(),
			Height:       s.image.Height(),
			Coefficients: coefs,
			Scale:        s.image.Scale(),
			Bias:         s.image.Bias(),
		},
		Tree: TreeInfo{
			TileSize: s.tileSize,
			MaxSize:  s.tree.MaxSize,
			Nodes:    s.tree.Nodes,
		},
	}

	outFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(desc); err != nil {
		return err
	}

	return nil
}
