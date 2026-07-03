package rti

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	_ "golang.org/x/image/tiff"
)

// StandardImage implements MultiLayerImage for typical single-layer images
type StandardImage struct {
	img image.Image
	w   int
	h   int
}

// LoadStandardImage loads a JPEG, PNG, or TIFF image from disk
func LoadStandardImage(filename string) (*StandardImage, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	return &StandardImage{
		img: img,
		w:   bounds.Dx(),
		h:   bounds.Dy(),
	}, nil
}

func (si *StandardImage) Width() int {
	return si.w
}

func (si *StandardImage) Height() int {
	return si.h
}

func (si *StandardImage) NumLayers() int {
	return 1
}

func (si *StandardImage) GetLayer(layerIdx int) (image.Image, error) {
	if layerIdx != 0 {
		return nil, fmt.Errorf("invalid layer index %d for standard image", layerIdx)
	}
	return si.img, nil
}

func (si *StandardImage) Type() string {
	return "IMAGE"
}

func (si *StandardImage) Scale() []float64 {
	return nil
}

func (si *StandardImage) Bias() []float64 {
	return nil
}
