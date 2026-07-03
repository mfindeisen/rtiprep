package rti

import (
	"bufio"
	"fmt"
	"image"
	"os"
	"strings"
)

// MultiLayerImage represents an image consisting of one or more layers
// (e.g., standard images, or spatial/luminance polynomial coefficients of PTM/HSH).
type MultiLayerImage interface {
	Width() int
	Height() int
	NumLayers() int
	GetLayer(layerIdx int) (image.Image, error)
	Type() string
	Scale() []float64
	Bias() []float64
}

// LoadRti opens an RTI/PTM file and routes it to the appropriate parser
func LoadRti(filename string) (MultiLayerImage, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	// Read the very first line to detect format type
	typeLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("error reading format type line: %w", err)
	}

	if strings.Contains(typeLine, "PTM") {
		return LoadPtm(file)
	} else if strings.Contains(typeLine, "HSH") {
		return LoadHsh(file)
	}

	return nil, fmt.Errorf("unrecognized RTI file format header: '%s'", strings.TrimSpace(typeLine))
}


