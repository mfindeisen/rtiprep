package splitter

import (
	"image"
	"math"
)

// RectF represents a rectangle with float64 coordinates, used for normalized WebGL texture coordinates
type RectF struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

// Node represents a quadtree tile node
type Node struct {
	ID           int     `json:"id"`           // 1-based node index
	Parent       int     `json:"parent"`       // 0-based parent index, -1 for root
	Children     []int   `json:"children"`     // 0-based indices of child nodes, -1 for leaf
	Level        int     `json:"level"`        // pyramid level (0 = coarsest)
	GridX        int     `json:"gridX"`        // tile column at this level
	GridY        int     `json:"gridY"`        // tile row at this level (0 = top)
	ImgBox       image.Rectangle `json:"-"`    // Virtual image bounding box (unpadded, in S x S space)
	PaddedImgBox image.Rectangle `json:"-"`    // Virtual image bounding box (padded with neighbor border)
	NormalizeBox RectF           `json:"normalizeBox"`
	Valid        bool            `json:"valid"` // true if node overlaps with original image boundaries
}

// Tree manages the quadtree structure
type Tree struct {
	TileSize int
	MaxSize  int
	NLevels  int
	Nodes    []Node
	ImgRect  image.Rectangle // Centered original image rectangle in virtual S x S space
}

// BuildTree constructs the complete quadtree structure for given image size and target tile size
func BuildTree(w, h, tileSize int) *Tree {
	maxImgSize := w
	if h > maxImgSize {
		maxImgSize = h
	}

	// Find the next power of 2 for the maximum dimension
	maxSize := 1
	for maxSize < maxImgSize {
		maxSize <<= 1
	}

	// Calculate number of levels
	nLevels := 0
	for temp := maxSize; temp >= tileSize; temp >>= 1 {
		nLevels++
	}

	woffset := (maxSize - w) / 2
	hoffset := (maxSize - h) / 2

	// Position original image in centered virtual space
	imgRect := image.Rect(woffset, hoffset, woffset+w, hoffset+h)

	// Total nodes in a perfect 4-ary tree of height nLevels: (4^nLevels - 1) / 3
	totalNodes := int((math.Pow(4, float64(nLevels)) - 1) / 3)
	nodes := make([]Node, totalNodes)

	tree := &Tree{
		TileSize: tileSize,
		MaxSize:  maxSize,
		NLevels:  nLevels,
		Nodes:    nodes,
		ImgRect:  imgRect,
	}

	// Build the tree recursively starting at root (index 0)
	tree.makeNode(0, -1, 0, 0, 0)

	return tree
}

func (t *Tree) makeNode(idx int, parentIdx int, level int, gx int, gy int) {
	node := &t.Nodes[idx]
	node.ID = idx + 1
	node.Parent = parentIdx
	node.Children = []int{-1, -1, -1, -1}
	node.Level = level
	node.GridX = gx
	node.GridY = gy

	// Unpadded box size at this level
	sz := t.MaxSize >> level
	x0 := gx * sz
	y0 := gy * sz
	node.ImgBox = image.Rect(x0, y0, x0+sz, y0+sz)

	// Validate intersection with centered original image
	node.Valid = t.ImgRect.Overlaps(node.ImgBox)

	// Compute normalizeBox in [0, 1] texture coordinates
	gridSizeInt := 1 << uint(level)
	gridSize := float64(gridSizeInt)
	node.NormalizeBox.Left = float64(gx) / gridSize
	node.NormalizeBox.Right = float64(gx+1) / gridSize

	topVal := gridSizeInt - 1 - gy
	node.NormalizeBox.Top = float64(topVal) / gridSize

	bottomVal := gridSizeInt - gy
	node.NormalizeBox.Bottom = float64(bottomVal) / gridSize

	// Seam Border Padding: expand box to include 1-pixel neighbor border
	pfp := 1 << (t.NLevels - 1 - level)
	node.PaddedImgBox = image.Rect(
		x0-pfp,
		y0-pfp,
		x0+sz+pfp,
		y0+sz+pfp,
	)

	// Construct children if not at leaf level
	if level < t.NLevels-1 {
		// Populate child references in order: Top-Left, Top-Right, Bottom-Left, Bottom-Right
		// Child index in tree array: 4 * parent_index + 1 + i
		for i := 0; i < 4; i++ {
			childIdx := 4*idx + 1 + i
			node.Children[i] = childIdx

			// Morton code / Z-order coordinates for children:
			// i = 0 (Top-Left):     gx*2,   gy*2
			// i = 1 (Top-Right):    gx*2+1, gy*2
			// i = 2 (Bottom-Left):  gx*2,   gy*2+1
			// i = 3 (Bottom-Right): gx*2+1, gy*2+1
			cgx := gx*2 + (i & 1)
			cgy := gy*2 + (i >> 1)

			t.makeNode(childIdx, idx, level+1, cgx, cgy)
		}
	}
}
