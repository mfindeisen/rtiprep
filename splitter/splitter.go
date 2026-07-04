package splitter

import (
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"rtiprep/rti"

	"encoding/json"
	"fmt"
	"image"
	"math"
	"runtime"
	"strings"

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

	var progressMutex sync.Mutex
	completedLayers := 0

	for l := 0; l < numLayers; l++ {
		wg.Add(1)
		go func(layerIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.splitLayer(layerIdx, destFolder, quality, format); err != nil {
				errChan <- fmt.Errorf("error in layer %d: %w", layerIdx, err)
			} else {
				progressMutex.Lock()
				completedLayers++
				fmt.Printf("[PROGRESS] %d,%d\n", completedLayers, numLayers)
				progressMutex.Unlock()
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

	w := s.image.Width()
	h := s.image.Height()
	woffset := (s.tree.MaxSize - w) / 2
	hoffset := (s.tree.MaxSize - h) / 2

	destSize := s.tileSize + 2

	// Process all valid nodes
	for i := 0; i < len(s.tree.Nodes); i++ {
		node := s.tree.Nodes[i]
		if !node.Valid {
			continue
		}

		// Create destination tile, initially filled with opaque black
		resized := image.NewRGBA(image.Rect(0, 0, destSize, destSize))
		for idx := 3; idx < len(resized.Pix); idx += 4 {
			resized.Pix[idx] = 255
		}

		// Find the overlap between the node's virtual padded bounding box and the image bounds in virtual space
		intersectVirtual := node.PaddedImgBox.Intersect(s.tree.ImgRect)

		// Map this virtual intersection rectangle to the actual source coordinates of layerImg
		srcRect := intersectVirtual.Sub(image.Pt(woffset, hoffset))

		// Compute destination rectangle in the destSize x destSize tile
		scale := float64(destSize) / float64(node.PaddedImgBox.Dx())

		xStartVirt := intersectVirtual.Min.X - node.PaddedImgBox.Min.X
		yStartVirt := intersectVirtual.Min.Y - node.PaddedImgBox.Min.Y
		xEndVirt := intersectVirtual.Max.X - node.PaddedImgBox.Min.X
		yEndVirt := intersectVirtual.Max.Y - node.PaddedImgBox.Min.Y

		dx0 := int(math.Round(float64(xStartVirt) * scale))
		dy0 := int(math.Round(float64(yStartVirt) * scale))
		dx1 := int(math.Round(float64(xEndVirt) * scale))
		dy1 := int(math.Round(float64(yEndVirt) * scale))

		if dx0 < 0 { dx0 = 0 }
		if dy0 < 0 { dy0 = 0 }
		if dx1 > destSize { dx1 = destSize }
		if dy1 > destSize { dy1 = destSize }

		dstRect := image.Rect(dx0, dy0, dx1, dy1)

		// Scale only the overlapping part directly into the destination tile
		if !dstRect.Empty() && !srcRect.Empty() {
			xDraw.BiLinear.Scale(resized, dstRect, layerImg, srcRect, xDraw.Src, nil)
		}

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

// SaveLegacyDescriptor outputs the XML descriptor file info.xml required by legacy viewers
func (s *Splitter) SaveLegacyDescriptor(destFolder string, format string) error {
	filePath := filepath.Join(destFolder, "info.xml")

	formatVal := "0"
	if format == "png" {
		formatVal = "1"
	}

	var sb strings.Builder
	sb.WriteString("<?xml version='1.0' encoding='UTF-8'?>\n")
	sb.WriteString(fmt.Sprintf("<MultiRes format=\"%s\">\n", formatVal))

	coefs := s.image.NumLayers()
	if s.image.Type() == "LRGB_PTM" || s.image.Type() == "RGB_PTM" {
		coefs = 6
	}

	sb.WriteString(fmt.Sprintf("  <Content type=\"%s\">\n", s.image.Type()))
	sb.WriteString(fmt.Sprintf("    <Size width=\"%d\" height=\"%d\" coefficients=\"%d\"/>\n",
		s.image.Width(), s.image.Height(), coefs))

	scaleStrs := make([]string, len(s.image.Scale()))
	for idx, val := range s.image.Scale() {
		scaleStrs[idx] = fmt.Sprintf("%f", val)
	}
	sb.WriteString(fmt.Sprintf("    <Scale>%s </Scale>\n", strings.Join(scaleStrs, " ")))

	biasStrs := make([]string, len(s.image.Bias()))
	for idx, val := range s.image.Bias() {
		biasStrs[idx] = fmt.Sprintf("%f", val)
	}
	sb.WriteString(fmt.Sprintf("    <Bias>%s </Bias>\n", strings.Join(biasStrs, " ")))
	sb.WriteString("  </Content>\n")

	sb.WriteString("  <Tree>\n")
	sb.WriteString(fmt.Sprintf("%d 0\n", len(s.tree.Nodes)))
	sb.WriteString(fmt.Sprintf("%d\n", s.tileSize))
	sb.WriteString(fmt.Sprintf("%d %d 255\n", s.tree.MaxSize, s.tree.MaxSize))
	sb.WriteString("0 0 0\n")

	for i := 0; i < len(s.tree.Nodes); i++ {
		node := s.tree.Nodes[i]

		// Map children to order: Bottom-Left, Bottom-Right, Top-Left, Top-Right
		// Go index order: 0:TL, 1:TR, 2:BL, 3:BR
		// C++ maps: j=0 -> BL(2), j=1 -> BR(3), j=2 -> TL(0), j=3 -> TR(1)
		childBL := node.Children[2]
		childBR := node.Children[3]
		childTL := node.Children[0]
		childTR := node.Children[1]

		validStr := "0"
		if node.Valid {
			validStr = "1"
		}

		sb.WriteString(fmt.Sprintf("%d %d %d %d %d %d %d %s %f %f 0 %f %f 1\n",
			node.ID,
			node.Parent,
			childBL,
			childBR,
			childTL,
			childTR,
			s.tileSize,
			validStr,
			node.NormalizeBox.Left,
			node.NormalizeBox.Top,
			node.NormalizeBox.Right,
			node.NormalizeBox.Bottom,
		))
	}
	sb.WriteString("  </Tree>\n")
	sb.WriteString("</MultiRes>\n")

	return os.WriteFile(filePath, []byte(sb.String()), 0644)
}

// SaveThumbnail saves the first layer of the MultiLayerImage as a JPEG file to be used as a preview/thumbnail
func SaveThumbnail(img rti.MultiLayerImage, destFile string) error {
	layer, err := img.GetLayer(0)
	if err != nil {
		return err
	}

	outFile, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer outFile.Close()

	return jpeg.Encode(outFile, layer, &jpeg.Options{Quality: 80})
}
