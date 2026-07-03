package splitter

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"image"
	"io"
	"os"
	"sync"
	"rtiprep/rti"

	xDraw "golang.org/x/image/draw"
)

// WritePyramidalTiff creates a single, tiled pyramidal TIFF file with all layers packed as channels
func WritePyramidalTiff(img rti.MultiLayerImage, destFile string, weightsJson string) error {
	w, h := img.Width(), img.Height()

	// Determine channels based on format
	numChannels := img.NumLayers()
	if img.Type() == "LRGB_PTM" || img.Type() == "RGB_PTM" {
		numChannels = 6
	}
	// For standard image, use 3 channels (RGB) or 4 channels (RGBA / latent map) if weights are present
	if img.Type() == "IMAGE" {
		if weightsJson != "" {
			numChannels = 4
		} else {
			numChannels = 3
		}
	} else if img.Type() == "LRGB_PTM" {
		// LRGB PTM has 3 layers: Layer 0 (a0-a2), Layer 1 (a3-a5), Layer 2 (R-B) -> total 9 channels
		numChannels = 9
	} else if img.Type() == "RGB_PTM" {
		// RGB PTM has 6 layers, each is RGB -> total 18 channels
		numChannels = 18
	} else if img.Type() == "HSH_RTI" {
		numChannels = img.NumLayers() * 3
	}

	// Calculate number of levels
	wTemp, hTemp := w, h
	nLevels := 1
	for wTemp > 256 || hTemp > 256 {
		wTemp = (wTemp + 1) / 2
		hTemp = (hTemp + 1) / 2
		nLevels++
	}

	// Level dimensions
	levelWidths := make([]int, nLevels)
	levelHeights := make([]int, nLevels)
	wTemp, hTemp = w, h
	for l := 0; l < nLevels; l++ {
		levelWidths[l] = wTemp
		levelHeights[l] = hTemp
		wTemp = (wTemp + 1) / 2
		hTemp = (hTemp + 1) / 2
	}

	// Generate and compress tiles for all levels in memory (Pass 1)
	type levelTiles struct {
		tiles           [][]byte
		offsetsRelative []uint32
	}

	levels := make([]levelTiles, nLevels)
	var tilesBuffer bytes.Buffer

	for l := 0; l < nLevels; l++ {
		lw, lh := levelWidths[l], levelHeights[l]
		tilesX := (lw + 255) / 256
		tilesY := (lh + 255) / 256
		numTiles := tilesX * tilesY

		// Downsample layers for this level
		levelLayers := make([]image.Image, img.NumLayers())
		for i := 0; i < img.NumLayers(); i++ {
			origLayer, err := img.GetLayer(i)
			if err != nil {
				return err
			}
			if l == 0 {
				levelLayers[i] = origLayer
			} else {
				var resized image.Image
				if img.Type() == "IMAGE" && weightsJson != "" {
					resizedNRGBA := image.NewNRGBA(image.Rect(0, 0, lw, lh))
					xDraw.NearestNeighbor.Scale(resizedNRGBA, resizedNRGBA.Bounds(), origLayer, origLayer.Bounds(), xDraw.Src, nil)
					resized = resizedNRGBA
				} else {
					resizedRGBA := image.NewRGBA(image.Rect(0, 0, lw, lh))
					xDraw.BiLinear.Scale(resizedRGBA, resizedRGBA.Bounds(), origLayer, origLayer.Bounds(), xDraw.Src, nil)
					resized = resizedRGBA
				}
				levelLayers[i] = resized
			}
		}

		// Compress tiles in parallel
		compressedTiles := make([][]byte, numTiles)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var lastErr error

		for ty := 0; ty < tilesY; ty++ {
			for tx := 0; tx < tilesX; tx++ {
				tileIdx := ty*tilesX + tx
				wg.Add(1)
				go func(tx, ty, idx int) {
					defer wg.Done()
					tileData := getTileData(levelLayers, tx, ty, lw, lh, numChannels, img.Type())
					compressed, err := compressZlib(tileData)
					if err != nil {
						mu.Lock()
						lastErr = err
						mu.Unlock()
						return
					}
					compressedTiles[idx] = compressed
				}(tx, ty, tileIdx)
			}
		}
		wg.Wait()
		if lastErr != nil {
			return lastErr
		}

		// Write to temp tiles buffer and track relative offsets
		offsetsRel := make([]uint32, numTiles)
		for idx, tileBytes := range compressedTiles {
			offsetsRel[idx] = uint32(tilesBuffer.Len())
			tilesBuffer.Write(tileBytes)
		}

		levels[l] = levelTiles{
			tiles:           compressedTiles,
			offsetsRelative: offsetsRel,
		}
	}

	// Serialize bias and scale into JSON metadata for the ImageDescription tag
	var metaObj map[string]interface{}
	if weightsJson != "" {
		var weights interface{}
		_ = json.Unmarshal([]byte(weightsJson), &weights)
		metaObj = map[string]interface{}{
			"type":    "neural",
			"weights": weights,
		}
	} else {
		metaObj = map[string]interface{}{
			"bias":  img.Bias(),
			"scale": img.Scale(),
		}
	}
	metaBytes, _ := json.Marshal(metaObj)
	metaStr := string(metaBytes) + "\x00" // Null-terminated ASCII

	// Calculate absolute offsets for IFDs and values (Pass 2 Layout)
	ifdOffsets := make([]uint32, nLevels)
	bitsPerSampleOffsets := make([]uint32, nLevels)
	imageDescOffsets := make([]uint32, nLevels)
	tileOffsetsOffsets := make([]uint32, nLevels)
	tileByteCountsOffsets := make([]uint32, nLevels)

	currentOffset := uint32(8) // Skip 8-byte TIFF Header

	const ifdSize = 2 + 13*12 + 4 // 2 (tag count) + 13 tags * 12 bytes + 4 (next IFD offset) = 162 bytes

	for l := 0; l < nLevels; l++ {
		ifdOffsets[l] = currentOffset
		currentOffset += ifdSize

		// BitsPerSample values (numChannels * 2 bytes)
		bitsPerSampleOffsets[l] = align4(currentOffset)
		currentOffset = bitsPerSampleOffsets[l] + uint32(numChannels*2)

		// ImageDescription string value (len(metaStr) bytes)
		imageDescOffsets[l] = align4(currentOffset)
		currentOffset = imageDescOffsets[l] + uint32(len(metaStr))

		numTiles := uint32(len(levels[l].tiles))

		// TileOffsets values (numTiles * 4 bytes)
		tileOffsetsOffsets[l] = align4(currentOffset)
		currentOffset = tileOffsetsOffsets[l] + numTiles*4

		// TileByteCounts values (numTiles * 4 bytes)
		tileByteCountsOffsets[l] = align4(currentOffset)
		currentOffset = tileByteCountsOffsets[l] + numTiles*4

		currentOffset = align4(currentOffset)
	}

	tilesStartOffset := currentOffset

	// Assemble final file
	outFile, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// Write TIFF Header
	// Byte order 'II' (Little Endian), magic number 42, offset to first IFD (8)
	if _, err := outFile.Write([]byte{'I', 'I', 42, 0, 8, 0, 0, 0}); err != nil {
		return err
	}

	// Write IFDs and their metadata values
	for l := 0; l < nLevels; l++ {
		lw, lh := levelWidths[l], levelHeights[l]
		numTiles := len(levels[l].tiles)

		// 1. Write the IFD tag list (sorted by tag ID)
		// Number of tags: 13
		binary.Write(outFile, binary.LittleEndian, uint16(13))

		// NewSubfileType (254): 0 (main) or 1 (reduced resolution)
		subfileType := uint32(0)
		if l > 0 {
			subfileType = 1
		}
		writeTag(outFile, 254, 4, 1, subfileType)

		// ImageWidth (256)
		writeTag(outFile, 256, 4, 1, uint32(lw))

		// ImageLength (257)
		writeTag(outFile, 257, 4, 1, uint32(lh))

		// BitsPerSample (258): points to array of shorts
		writeTag(outFile, 258, 3, uint32(numChannels), bitsPerSampleOffsets[l])

		// Compression (259): 8 (Adobe Deflate)
		writeTag(outFile, 259, 3, 1, 8)

		// PhotometricInterpretation (262): 1 (BlackIsZero)
		writeTag(outFile, 262, 3, 1, 1)

		// ImageDescription (270): points to JSON metadata string
		writeTag(outFile, 270, 2, uint32(len(metaStr)), imageDescOffsets[l])

		// SamplesPerPixel (277)
		writeTag(outFile, 277, 3, 1, uint32(numChannels))

		// PlanarConfiguration (284): 1 (Chunky)
		writeTag(outFile, 284, 3, 1, 1)

		// TileWidth (322)
		writeTag(outFile, 322, 4, 1, 256)

		// TileLength (323)
		writeTag(outFile, 323, 4, 1, 256)

		// TileOffsets (324): points to array of longs (or direct value if 1 tile)
		tileOffsetVal := tileOffsetsOffsets[l]
		if numTiles == 1 {
			tileOffsetVal = tilesStartOffset + levels[l].offsetsRelative[0]
		}
		writeTag(outFile, 324, 4, uint32(numTiles), tileOffsetVal)

		// TileByteCounts (325): points to array of longs (or direct value if 1 tile)
		tileByteCountVal := tileByteCountsOffsets[l]
		if numTiles == 1 {
			tileByteCountVal = uint32(len(levels[l].tiles[0]))
		}
		writeTag(outFile, 325, 4, uint32(numTiles), tileByteCountVal)

		// Next IFD Offset (4 bytes)
		nextIFD := uint32(0)
		if l < nLevels-1 {
			nextIFD = ifdOffsets[l+1]
		}
		binary.Write(outFile, binary.LittleEndian, nextIFD)

		// 2. Write BitsPerSample array values
		seekToOffset(outFile, bitsPerSampleOffsets[l])
		for c := 0; c < numChannels; c++ {
			binary.Write(outFile, binary.LittleEndian, uint16(8))
		}

		// 2b. Write ImageDescription string values
		seekToOffset(outFile, imageDescOffsets[l])
		outFile.Write([]byte(metaStr))

		// 3. Write TileOffsets array values
		seekToOffset(outFile, tileOffsetsOffsets[l])
		for _, relOffset := range levels[l].offsetsRelative {
			binary.Write(outFile, binary.LittleEndian, tilesStartOffset+relOffset)
		}

		// 4. Write TileByteCounts array values
		seekToOffset(outFile, tileByteCountsOffsets[l])
		for _, tile := range levels[l].tiles {
			binary.Write(outFile, binary.LittleEndian, uint32(len(tile)))
		}
	}

	// 5. Append all compressed tile pixel data
	seekToOffset(outFile, tilesStartOffset)
	if _, err := io.Copy(outFile, &tilesBuffer); err != nil {
		return err
	}

	return nil
}

func writeTag(w io.Writer, tag uint16, tagType uint16, count uint32, value uint32) {
	binary.Write(w, binary.LittleEndian, tag)
	binary.Write(w, binary.LittleEndian, tagType)
	binary.Write(w, binary.LittleEndian, count)
	binary.Write(w, binary.LittleEndian, value)
}

func align4(n uint32) uint32 {
	return (n + 3) &^ 3
}

func seekToOffset(f *os.File, offset uint32) {
	f.Seek(int64(offset), io.SeekStart)
}

func getRGB(img image.Image, x, y int) (byte, byte, byte) {
	if rgba, ok := img.(*image.RGBA); ok {
		idx := rgba.PixOffset(x, y)
		return rgba.Pix[idx], rgba.Pix[idx+1], rgba.Pix[idx+2]
	}
	if nrgba, ok := img.(*image.NRGBA); ok {
		idx := nrgba.PixOffset(x, y)
		return nrgba.Pix[idx], nrgba.Pix[idx+1], nrgba.Pix[idx+2]
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return byte(r >> 8), byte(g >> 8), byte(b >> 8)
}

func getRGBA(img image.Image, x, y int) (byte, byte, byte, byte) {
	if rgba, ok := img.(*image.RGBA); ok {
		idx := rgba.PixOffset(x, y)
		return rgba.Pix[idx], rgba.Pix[idx+1], rgba.Pix[idx+2], rgba.Pix[idx+3]
	}
	if nrgba, ok := img.(*image.NRGBA); ok {
		idx := nrgba.PixOffset(x, y)
		return nrgba.Pix[idx], nrgba.Pix[idx+1], nrgba.Pix[idx+2], nrgba.Pix[idx+3]
	}
	r, g, b, a := img.At(x, y).RGBA()
	return byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)
}

func compressZlib(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// getTileData packs multiple channel layers into a single Chunky interleaved pixel array for a 256x256 tile
func getTileData(layers []image.Image, tx, ty, w, h, numChannels int, rtiType string) []byte {
	dest := make([]byte, 256*256*numChannels)

	for y := 0; y < 256; y++ {
		py := ty*256 + y
		for x := 0; x < 256; x++ {
			px := tx*256 + x
			destPixelIdx := (y*256 + x) * numChannels

			if px >= w || py >= h {
				// Pad out-of-bounds with black bytes (0)
				continue
			}

			// Multiplex layers into channels based on type
			switch rtiType {
			case "IMAGE":
				if numChannels == 4 {
					r, g, b, a := getRGBA(layers[0], px, py)
					dest[destPixelIdx] = r
					dest[destPixelIdx+1] = g
					dest[destPixelIdx+2] = b
					dest[destPixelIdx+3] = a
				} else {
					r, g, b := getRGB(layers[0], px, py)
					dest[destPixelIdx] = r
					dest[destPixelIdx+1] = g
					dest[destPixelIdx+2] = b
				}

			case "LRGB_PTM":
				r0, g0, b0 := getRGB(layers[0], px, py)
				r1, g1, b1 := getRGB(layers[1], px, py)
				r2, g2, b2 := getRGB(layers[2], px, py)
				dest[destPixelIdx] = r0
				dest[destPixelIdx+1] = g0
				dest[destPixelIdx+2] = b0
				dest[destPixelIdx+3] = r1
				dest[destPixelIdx+4] = g1
				dest[destPixelIdx+5] = b1
				dest[destPixelIdx+6] = r2
				dest[destPixelIdx+7] = g2
				dest[destPixelIdx+8] = b2

			case "RGB_PTM":
				for c := 0; c < 6; c++ {
					r, g, b := getRGB(layers[c], px, py)
					dest[destPixelIdx+c] = r      // Red coefficients
					dest[destPixelIdx+6+c] = g    // Green coefficients
					dest[destPixelIdx+12+c] = b   // Blue coefficients
				}

			case "HSH_RTI":
				numLayers := len(layers)
				for k := 0; k < numLayers; k++ {
					r, g, b := getRGB(layers[k], px, py)
					dest[destPixelIdx+k] = r                 // Red coefficients
					dest[destPixelIdx+numLayers+k] = g       // Green coefficients
					dest[destPixelIdx+2*numLayers+k] = b     // Blue coefficients
				}
			}
		}
	}

	return dest
}
