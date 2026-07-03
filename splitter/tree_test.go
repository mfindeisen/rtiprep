package splitter

import "testing"

func TestBuildTree(t *testing.T) {
	// Standard test case: 512x512 image with 256 tile size
	// maxSize = 512, levels = 2 (Level 0: 512x512, Level 1: 256x256 tiles)
	// Number of nodes = (4^2 - 1) / 3 = 5
	tree := BuildTree(512, 512, 256)

	if tree.MaxSize != 512 {
		t.Errorf("expected MaxSize 512, got %d", tree.MaxSize)
	}
	if tree.NLevels != 2 {
		t.Errorf("expected NLevels 2, got %d", tree.NLevels)
	}
	if len(tree.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(tree.Nodes))
	}

	// Verify root node properties
	root := tree.Nodes[0]
	if root.ID != 1 {
		t.Errorf("expected root ID 1, got %d", root.ID)
	}
	if root.Parent != -1 {
		t.Errorf("expected root parent -1, got %d", root.Parent)
	}
	if root.NormalizeBox.Left != 0.0 || root.NormalizeBox.Right != 1.0 || root.NormalizeBox.Top != 0.0 || root.NormalizeBox.Bottom != 1.0 {
		t.Errorf("invalid root normalizeBox: %+v", root.NormalizeBox)
	}

	// Verify Top-Left child node (Node ID 2, array index 1, t = 1)
	topLeftChild := tree.Nodes[1]
	if topLeftChild.ID != 2 {
		t.Errorf("expected top-left child ID 2, got %d", topLeftChild.ID)
	}
	if topLeftChild.Parent != 0 {
		t.Errorf("expected top-left child parent index 0, got %d", topLeftChild.Parent)
	}
	// Image coordinates: Top-Left is unshifted [0, 0, 256, 256]
	if topLeftChild.ImgBox.Min.X != 0 || topLeftChild.ImgBox.Min.Y != 0 || topLeftChild.ImgBox.Max.X != 256 || topLeftChild.ImgBox.Max.Y != 256 {
		t.Errorf("invalid image box: %+v", topLeftChild.ImgBox)
	}
	// Texture coordinates: standard top-left Y is bottom half in Y-up WebGL space, so Top=0.5, Bottom=1.0
	if topLeftChild.NormalizeBox.Left != 0.0 || topLeftChild.NormalizeBox.Right != 0.5 || topLeftChild.NormalizeBox.Top != 0.5 || topLeftChild.NormalizeBox.Bottom != 1.0 {
		t.Errorf("invalid normalizeBox for top-left child: %+v", topLeftChild.NormalizeBox)
	}

	// Verify Bottom-Left child node (Node ID 4, array index 3, t = 3)
	bottomLeftChild := tree.Nodes[3]
	if bottomLeftChild.ID != 4 {
		t.Errorf("expected bottom-left child ID 4, got %d", bottomLeftChild.ID)
	}
	// Image coordinates: Bottom-Left is shifted down by height [0, 256, 256, 512]
	if bottomLeftChild.ImgBox.Min.X != 0 || bottomLeftChild.ImgBox.Min.Y != 256 || bottomLeftChild.ImgBox.Max.X != 256 || bottomLeftChild.ImgBox.Max.Y != 512 {
		t.Errorf("invalid image box: %+v", bottomLeftChild.ImgBox)
	}
	// Texture coordinates: bottom-left Y is top half in Y-up WebGL space, so Top=0.0, Bottom=0.5
	if bottomLeftChild.NormalizeBox.Left != 0.0 || bottomLeftChild.NormalizeBox.Right != 0.5 || bottomLeftChild.NormalizeBox.Top != 0.0 || bottomLeftChild.NormalizeBox.Bottom != 0.5 {
		t.Errorf("invalid normalizeBox for bottom-left child: %+v", bottomLeftChild.NormalizeBox)
	}
}
