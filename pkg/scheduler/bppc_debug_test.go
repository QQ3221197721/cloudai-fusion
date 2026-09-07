package scheduler

import "testing"

// Debug test for BPPC instance with detailed tracing
func TestBPPCDetailedDebug(t *testing.T) {
	t.Log("=== Testing BPPC Backtracking Algorithm ===")
	
	instance := BPPCInstance{
		NumBins: 2,
		BinCap:  5,
		Items: []BPPCItem{
			{ID: "a", Size: 3, StartBounds: []int{0}},
			{ID: "b", Size: 2, StartBounds: []int{0}},
			{ID: "c", Size: 4, StartBounds: []int{0}},
		},
	}
	
	t.Logf("Instance: NumBins=%d, Cap=%v, Items=%v", instance.NumBins, instance.BinCap, instance.Items)
	
	result := SolveBPPCBruteForceWithTrace(instance, t)
	if result == nil {
		t.Fatal("Expected feasible=true but got nil - algorithm BUG!")
	}
	
	t.Logf("✓ Success! Assignment: %v", result)
	
	// Verify the solution
	binUsed := make(map[int]int)
	for itemID, placement := range result {
		binIdx := placement[0]
		startPos := placement[1]
		itemSize := getItemSizeByName(itemID, instance.Items)
		endPos := startPos + itemSize
		
		t.Logf("  Item %s: Bin%d [%d:%d)", itemID, binIdx, startPos, endPos)
		
		// Check if this placement is valid
		if endPos > instance.BinCap {
			t.Errorf("Placement exceeds bin capacity: %s ends at %d > Cap %d", itemID, endPos, instance.BinCap)
		}
		
		// Track bin usage
		if binUsed[binIdx] < endPos {
			binUsed[binIdx] = endPos
		}
	}
	
	// Check if all bins within capacity
	for binIdx, maxPos := range binUsed {
		if maxPos > instance.BinCap {
			t.Errorf("Bin%d usage %d exceeds capacity %d", binIdx, maxPos, instance.BinCap)
		}
	}
}

// getItemSizeByName helper
func getItemSizeByName(id string, items []BPPCItem) int {
	for _, item := range items {
		if item.ID == id {
			return item.Size
		}
	}
	return 0
}

// SolveBPPCBruteForceWithTrace version with debug output
func SolveBPPCBruteForceWithTrace(inst BPPCInstance, t *testing.T) map[string][2]int {
	nItems := len(inst.Items)
	t.Logf("Starting backtracking with %d items", nItems)
	
	binState := make([][]bool, inst.NumBins)
	for b := range binState {
		binState[b] = make([]bool, inst.BinCap+1)
		t.Logf("Bin%d state initialized to all false (size %d)", b, len(binState[b]))
	}
	
	assignment := make(map[string][2]int)
	var backtrack func(idx int, depth int) bool
	backtrack = func(idx int, depth int) bool {
		t.Logf("Depth %d: idx=%d (%s), trying placements...", depth, idx, inst.Items[idx].ID)
		
		if idx == nItems {
			t.Logf("✓ All %d items placed successfully!", nItems)
			return true
		}
		
		item := inst.Items[idx]
		
		for b := 0; b < inst.NumBins; b++ {
			for _, start := range item.StartBounds {
				end := start + item.Size
				
				t.Logf("  Trying Bin%d, Start=%d, End=%d (Size=%d)", b, start, end, item.Size)
				
				if end > inst.BinCap || start < 0 {
					t.Logf("    ✗ Invalid: end(%d)>Cap(%d) or start(%d)<0", end, inst.BinCap, start)
					continue
				}
				
				feasible := true
				for p := start; p < end && feasible; p++ {
					if binState[b][p] {
						feasible = false
						t.Logf("    Position %d occupied in Bin%d", p, b)
					}
				}
				
				if !feasible {
					t.Logf("  ✗ Bin%d not feasible for item %s", b, item.ID)
					continue
				}
				
				// Place item
				t.Logf("  ✓ Placing %s in Bin%d at %d", item.ID, b, start)
				for p := start; p < end; p++ {
					binState[b][p] = true
				}
				assignment[item.ID] = [2]int{b, start}
				
				if backtrack(idx+1, depth+1) {
					return true
				}
				
				// Undo placement
				t.Logf("  ✗ Backtracking from Bin%d", b)
				delete(assignment, item.ID)
				for p := start; p < end; p++ {
					binState[b][p] = false
				}
			}
		}
		
		t.Logf("✗ No valid placement found for item %s at idx %d", item.ID, idx)
		return false
	}
	
	if backtrack(0, 0) {
		return assignment
	}
	return nil
}
