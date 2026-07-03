package rti

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
)

// HshImage implements MultiLayerImage for Hemispherical Harmonics RTI files
type HshImage struct {
	w           int
	h           int
	coeffNumber int
	gmin        []float32
	gmax        []float32
	layers      []*image.RGBA
}

func (h *HshImage) Width() int {
	return h.w
}

func (h *HshImage) Height() int {
	return h.h
}

func (h *HshImage) NumLayers() int {
	return h.coeffNumber
}

func (h *HshImage) GetLayer(layerIdx int) (image.Image, error) {
	if layerIdx < 0 || layerIdx >= h.coeffNumber {
		return nil, fmt.Errorf("invalid layer index %d for HSH", layerIdx)
	}
	return h.layers[layerIdx], nil
}

func (h *HshImage) Type() string {
	return "HSH_RTI"
}

func (h *HshImage) Scale() []float64 {
	res := make([]float64, len(h.gmax))
	for i, v := range h.gmax {
		res[i] = float64(v)
	}
	return res
}

func (h *HshImage) Bias() []float64 {
	res := make([]float64, len(h.gmin))
	for i, v := range h.gmin {
		res[i] = float64(v)
	}
	return res
}

// LoadHsh loads an HSH file from an open file descriptor
func LoadHsh(file *os.File) (*HshImage, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to start of HSH file: %w", err)
	}

	reader := bufio.NewReader(file)

	// Read format line (skip comments starting with '#')
	var formatLine string
	for {
		line, err := readAsciiLine(reader)
		if err != nil {
			return nil, fmt.Errorf("error reading header: %w", err)
		}
		if !strings.HasPrefix(line, "#") && len(line) > 0 {
			formatLine = line
			break
		}
	}

	// Next line: Width Height Channels (3 tokens)
	sizeLine, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading size: %w", err)
	}
	sizeTokens := strings.Fields(sizeLine)
	if len(sizeTokens) != 3 {
		return nil, fmt.Errorf("invalid size line '%s', expected 3 tokens", sizeLine)
	}
	w, err := strconv.Atoi(sizeTokens[0])
	if err != nil {
		return nil, fmt.Errorf("invalid width '%s': %w", sizeTokens[0], err)
	}
	h, err := strconv.Atoi(sizeTokens[1])
	if err != nil {
		return nil, fmt.Errorf("invalid height '%s': %w", sizeTokens[1], err)
	}

	// Next line: CoeffNumber BasisType ElementSize (3 tokens)
	basisLine, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading basis metadata: %w", err)
	}
	basisTokens := strings.Fields(basisLine)
	if len(basisTokens) != 3 {
		return nil, fmt.Errorf("invalid basis line '%s', expected 3 tokens", basisLine)
	}
	coeffNumber, err := strconv.Atoi(basisTokens[0])
	if err != nil {
		return nil, fmt.Errorf("invalid coeff number '%s': %w", basisTokens[0], err)
	}

	// Read gmin and gmax binary float32 arrays (Little Endian)
	gmin := make([]float32, coeffNumber)
	gmax := make([]float32, coeffNumber)

	if err := binary.Read(reader, binary.LittleEndian, &gmin); err != nil {
		return nil, fmt.Errorf("error reading gmin: %w", err)
	}
	if err := binary.Read(reader, binary.LittleEndian, &gmax); err != nil {
		return nil, fmt.Errorf("error reading gmax: %w", err)
	}

	// Initialize the layers
	layers := make([]*image.RGBA, coeffNumber)
	for i := 0; i < coeffNumber; i++ {
		layers[i] = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	// Read binary coefficient data (top-to-bottom)
	// Each pixel has coeffNumber * 3 bytes
	lineSize := w * coeffNumber * 3
	buf := make([]byte, lineSize)

	for y := 0; y < h; y++ {
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, fmt.Errorf("error reading row %d of HSH pixel data: %w", y, err)
		}
		for x := 0; x < w; x++ {
			pixelOffset := x * 3 * coeffNumber
			for k := 0; k < coeffNumber; k++ {
				// R component is at pixelOffset + k
				// G component is at pixelOffset + coeffNumber + k
				// B component is at pixelOffset + 2*coeffNumber + k
				r := buf[pixelOffset+k]
				g := buf[pixelOffset+coeffNumber+k]
				b := buf[pixelOffset+2*coeffNumber+k]

				destIdx := layers[k].PixOffset(x, y)
				layers[k].Pix[destIdx] = r
				layers[k].Pix[destIdx+1] = g
				layers[k].Pix[destIdx+2] = b
				layers[k].Pix[destIdx+3] = 255 // Alpha
			}
		}
	}

	_ = formatLine // Keep compiler happy if formatLine isn't strictly required downstream
	return &HshImage{
		w:           w,
		h:           h,
		coeffNumber: coeffNumber,
		gmin:        gmin,
		gmax:        gmax,
		layers:      layers,
	}, nil
}
