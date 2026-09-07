package metrics

import (
	"testing"
)

// TestTreeRebalanceSanity checks if the AVL tree is actually balanced
func TestTreeRebalanceSanity(t *testing.T) {
	w := newTreeSlidingWindow(100000)
	
	// Insert sorted values - worst case for unbalanced trees
	for i := 0; i < 1000; i++ {
		w.insert(float64(i))
	}
	
	// Query multiple times - should be fast if balanced
	for i := 0; i < 100; i++ {
		w.percentile(0.5 + float64(i)*0.005)
	}
	
	t.Log("Completed 100 queries on tree of size", w.totalRecords())
	if w.tree != nil {
		t.Logf("Tree height after 1000 inserts = %d (expected ~10-12 if balanced)", w.tree.height)
		if w.tree.height > 25 {
			t.Fatalf("TREE IS UNBALANCED: height=%d for n=%d items!", w.tree.height, w.totalRecords())
		}
	}
	
	// Also test delete operation
	for i := 0; i < 100; i++ {
		w.deleteValueAtEdge()
		w.insert(float64(1000 + i))
	}
	t.Log("Wraparound completed successfully")
}

// Helper method added just for this test (won't persist)
func (w *treeSlidingWindow) deleteValueAtEdge() {
	// Delete smallest value
	if w.tree != nil {
		minNode := findMin(w.tree)
		if minNode != nil {
			w.tree, _ = w.tree.deleteValue(minNode.value)
		}
	}
}

func findMin(node *avlNode) *avlNode {
	for node.left != nil {
		node = node.left
	}
	return node
}
