package rti

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
)

// PtmImage implements MultiLayerImage for Polynomial Texture Maps
type PtmImage struct {
	ptmType   string // "LRGB_PTM" or "RGB_PTM"
	w         int
	h         int
	scale     []float64
	bias      []float64
	numLayers int
	layers    []*image.RGBA
}

func (p *PtmImage) Width() int {
	return p.w
}

func (p *PtmImage) Height() int {
	return p.h
}

func (p *PtmImage) NumLayers() int {
	return p.numLayers
}

func (p *PtmImage) GetLayer(layerIdx int) (image.Image, error) {
	if layerIdx < 0 || layerIdx >= p.numLayers {
		return nil, fmt.Errorf("invalid layer index %d for PTM", layerIdx)
	}
	return p.layers[layerIdx], nil
}

func (p *PtmImage) Type() string {
	return p.ptmType
}

func (p *PtmImage) Scale() []float64 {
	return p.scale
}

func (p *PtmImage) Bias() []float64 {
	return p.bias
}

// LoadPtm loads a PTM file from an open file descriptor
func LoadPtm(file *os.File) (*PtmImage, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to start of PTM file: %w", err)
	}

	reader := bufio.NewReader(file)

	// Line 1: format version (e.g. PTM_1.2)
	formatVer, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading format version: %w", err)
	}

	// Line 2: PTM type
	ptmTypeStr, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading PTM type: %w", err)
	}

	// Line 3: Width
	widthStr, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading width: %w", err)
	}
	w, err := strconv.Atoi(widthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid width '%s': %w", widthStr, err)
	}

	// Line 4: Height
	heightStr, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading height: %w", err)
	}
	h, err := strconv.Atoi(heightStr)
	if err != nil {
		return nil, fmt.Errorf("invalid height '%s': %w", heightStr, err)
	}

	// Line 5: Scale
	scaleStr, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading scale: %w", err)
	}
	scaleTokens := strings.Fields(scaleStr)
	if len(scaleTokens) != 6 {
		return nil, fmt.Errorf("expected 6 scale coefficients, got %d", len(scaleTokens))
	}
	scale := make([]float64, 6)
	for i, t := range scaleTokens {
		scale[i], err = strconv.ParseFloat(t, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid scale coefficient '%s': %w", t, err)
		}
	}

	// Line 6: Bias
	biasStr, err := readAsciiLine(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading bias: %w", err)
	}
	biasTokens := strings.Fields(biasStr)
	if len(biasTokens) != 6 {
		return nil, fmt.Errorf("expected 6 bias coefficients, got %d", len(biasTokens))
	}
	bias := make([]float64, 6)
	for i, t := range biasTokens {
		bias[i], err = strconv.ParseFloat(t, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid bias coefficient '%s': %w", t, err)
		}
	}

	ptm := &PtmImage{
		w:     w,
		h:     h,
		scale: scale,
		bias:  bias,
	}

	// Route based on type
	switch ptmTypeStr {
	case "PTM_FORMAT_LRGB":
		ptm.ptmType = "LRGB_PTM"
		ptm.numLayers = 3
		ptm.layers = make([]*image.RGBA, 3)
		for i := 0; i < 3; i++ {
			ptm.layers[i] = image.NewRGBA(image.Rect(0, 0, w, h))
		}

		isPtm12 := formatVer == "PTM_1.2"
		if isPtm12 {
			// Two-pass block-multiplexed layout
			// Pass 1: Coefficients a0..a5 (6 bytes/pixel)
			buf := make([]byte, w*6)
			for y := h - 1; y >= 0; y-- {
				if _, err := io.ReadFull(reader, buf); err != nil {
					return nil, fmt.Errorf("error reading coefficients block: %w", err)
				}
				for x := 0; x < w; x++ {
					srcIdx := x * 6
					// Layer 0: a0, a1, a2
					destIdx0 := ptm.layers[0].PixOffset(x, y)
					ptm.layers[0].Pix[destIdx0] = buf[srcIdx]
					ptm.layers[0].Pix[destIdx0+1] = buf[srcIdx+1]
					ptm.layers[0].Pix[destIdx0+2] = buf[srcIdx+2]
					ptm.layers[0].Pix[destIdx0+3] = 255

					// Layer 1: a3, a4, a5
					destIdx1 := ptm.layers[1].PixOffset(x, y)
					ptm.layers[1].Pix[destIdx1] = buf[srcIdx+3]
					ptm.layers[1].Pix[destIdx1+1] = buf[srcIdx+4]
					ptm.layers[1].Pix[destIdx1+2] = buf[srcIdx+5]
					ptm.layers[1].Pix[destIdx1+3] = 255
				}
			}
			// Pass 2: Chromaticity R, G, B (3 bytes/pixel)
			bufRGB := make([]byte, w*3)
			for y := h - 1; y >= 0; y-- {
				if _, err := io.ReadFull(reader, bufRGB); err != nil {
					return nil, fmt.Errorf("error reading RGB chromaticity block: %w", err)
				}
				for x := 0; x < w; x++ {
					srcIdx := x * 3
					destIdx := ptm.layers[2].PixOffset(x, y)
					ptm.layers[2].Pix[destIdx] = bufRGB[srcIdx]
					ptm.layers[2].Pix[destIdx+1] = bufRGB[srcIdx+1]
					ptm.layers[2].Pix[destIdx+2] = bufRGB[srcIdx+2]
					ptm.layers[2].Pix[destIdx+3] = 255
				}
			}
		} else {
			// Single-pass interleaved layout (9 bytes/pixel)
			buf := make([]byte, w*9)
			for y := h - 1; y >= 0; y-- {
				if _, err := io.ReadFull(reader, buf); err != nil {
					return nil, fmt.Errorf("error reading interleaved pixels: %w", err)
				}
				for x := 0; x < w; x++ {
					srcIdx := x * 9
					// Layer 0
					destIdx0 := ptm.layers[0].PixOffset(x, y)
					ptm.layers[0].Pix[destIdx0] = buf[srcIdx]
					ptm.layers[0].Pix[destIdx0+1] = buf[srcIdx+1]
					ptm.layers[0].Pix[destIdx0+2] = buf[srcIdx+2]
					ptm.layers[0].Pix[destIdx0+3] = 255

					// Layer 1
					destIdx1 := ptm.layers[1].PixOffset(x, y)
					ptm.layers[1].Pix[destIdx1] = buf[srcIdx+3]
					ptm.layers[1].Pix[destIdx1+1] = buf[srcIdx+4]
					ptm.layers[1].Pix[destIdx1+2] = buf[srcIdx+5]
					ptm.layers[1].Pix[destIdx1+3] = 255

					// Layer 2
					destIdx2 := ptm.layers[2].PixOffset(x, y)
					ptm.layers[2].Pix[destIdx2] = buf[srcIdx+6]
					ptm.layers[2].Pix[destIdx2+1] = buf[srcIdx+7]
					ptm.layers[2].Pix[destIdx2+2] = buf[srcIdx+8]
					ptm.layers[2].Pix[destIdx2+3] = 255
				}
			}
		}

	case "PTM_FORMAT_RGB":
		ptm.ptmType = "RGB_PTM"
		ptm.numLayers = 6
		ptm.layers = make([]*image.RGBA, 6)
		for i := 0; i < 6; i++ {
			ptm.layers[i] = image.NewRGBA(image.Rect(0, 0, w, h))
		}

		// RGB PTM contains 3 sequential blocks of W * H * 6 bytes
		// Block 1: Red channel coefficients a0..a5
		// Block 2: Green channel coefficients a0..a5
		// Block 3: Blue channel coefficients a0..a5
		buf := make([]byte, w*6)
		for channelIdx := 0; channelIdx < 3; channelIdx++ {
			for y := h - 1; y >= 0; y-- {
				if _, err := io.ReadFull(reader, buf); err != nil {
					return nil, fmt.Errorf("error reading RGB channel %d block: %w", channelIdx, err)
				}
				for x := 0; x < w; x++ {
					srcIdx := x * 6
					for c := 0; c < 6; c++ {
						destIdx := ptm.layers[c].PixOffset(x, y)
						// Write to channel channelIdx (0=R, 1=G, 2=B)
						ptm.layers[c].Pix[destIdx+channelIdx] = buf[srcIdx+c]
						ptm.layers[c].Pix[destIdx+3] = 255 // Alpha
					}
				}
			}
		}

	default:
		return nil, fmt.Errorf("unsupported PTM type: %s", ptmTypeStr)
	}

	return ptm, nil
}

// readAsciiLine helper reads a line of characters until '\n' and trims whitespace/carriage return
func readAsciiLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
