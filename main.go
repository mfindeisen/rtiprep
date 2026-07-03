package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"rtiprep/rti"
	"rtiprep/splitter"
)

func main() {
	quality := flag.Int("q", 90, "Quality of saved tiles (default: 90)")
	tileSize := flag.Int("t", 256, "Size of the tile (default: 256)")
	pngFormat := flag.Bool("p", false, "Save output tiles as PNG instead of JPEG")
	outputDir := flag.String("o", "", "Output destination folder/file (defaults to input name without extension or with .tif)")
	tiffMode := flag.Bool("tiff", false, "Save output as a single tiled pyramidal TIFF file instead of a folder of images")
	weightsFile := flag.String("weights", "", "Path to Neural RTI decoder weights JSON file (for GeoTIFF embedding)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <input_file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Supported input formats: .ptm, .rti, .jpg, .jpeg, .png, .tif, .tiff\n\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Error: Missing input file.")
		flag.Usage()
		os.Exit(1)
	}

	filename := args[0]
	info, err := os.Stat(filename)
	if os.IsNotExist(err) || info.IsDir() {
		fmt.Printf("Error: Input file '%s' does not exist.\n", filename)
		os.Exit(1)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	isRti := ext == ".ptm" || ext == ".rti"
	isImage := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".tif" || ext == ".tiff"

	if !isRti && !isImage {
		fmt.Printf("Error: Unsupported file format '%s'.\n", ext)
		flag.Usage()
		os.Exit(1)
	}

	// Compute default destination path if not provided
	destFolder := *outputDir
	if destFolder == "" {
		base := filepath.Base(filename)
		extLen := len(filepath.Ext(base))
		nameWithoutExt := base[:len(base)-extLen]
		if *tiffMode {
			destFolder = filepath.Join(filepath.Dir(filename), nameWithoutExt+".tif")
		} else {
			destFolder = filepath.Join(filepath.Dir(filename), nameWithoutExt)
		}
	}

	format := "jpg"
	if *pngFormat {
		format = "png"
	}

	fmt.Printf("Processing: %s\n", filename)
	if *tiffMode {
		fmt.Printf("Output format: Pyramidal Tiled TIFF (COG)\n")
		fmt.Printf("Output path:   %s\n\n", destFolder)
	} else {
		fmt.Printf("Tile size:  %d px\n", *tileSize)
		fmt.Printf("Quality:    %d\n", *quality)
		fmt.Printf("Format:     %s\n", format)
		fmt.Printf("Output dir: %s\n\n", destFolder)

		// Clean output folder if it exists
		if err := os.RemoveAll(destFolder); err != nil {
			fmt.Printf("Warning: Failed to clean destination folder: %v\n", err)
		}
	}

	startTime := time.Now()

	var img rti.MultiLayerImage
	if isImage {
		img, err = rti.LoadStandardImage(filename)
	} else {
		img, err = rti.LoadRti(filename)
	}

	if err != nil {
		fmt.Printf("Error loading input file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded:     %s (%dx%d, %d layers)\n", img.Type(), img.Width(), img.Height(), img.NumLayers())

	if *tiffMode {
		fmt.Println("Generating pyramidal tiled TIFF...")
		var weightsJson string
		if *weightsFile != "" {
			bytes, err := os.ReadFile(*weightsFile)
			if err != nil {
				fmt.Printf("Error reading weights file: %v\n", err)
				os.Exit(1)
			}
			weightsJson = string(bytes)
		}
		if err := splitter.WritePyramidalTiff(img, destFolder, weightsJson); err != nil {
			fmt.Printf("Error writing pyramidal TIFF: %v\n", err)
			os.Exit(1)
		}
	} else {
		s := splitter.NewSplitter(img, *tileSize)
		fmt.Printf("Pyramid:    %d levels (max size %dx%d)\n", s.NumLevels(), s.MaxSize(), s.MaxSize())

		fmt.Println("Slicing tiles...")
		if err := s.Split(destFolder, *quality, format); err != nil {
			fmt.Printf("Error splitting tiles: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Writing JSON descriptor...")
		if err := s.SaveDescriptor(destFolder, format); err != nil {
			fmt.Printf("Error writing descriptor: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("\nSuccess! Processed in %v.\n", time.Since(startTime).Round(time.Millisecond))
}
