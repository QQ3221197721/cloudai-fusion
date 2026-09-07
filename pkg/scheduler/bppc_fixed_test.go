package scheduler

import "testing"

// Fixed test with correct StartBounds - allow items to start at any valid position
func TestBPPCFixedCorrected(t *testing.T) {
	instance := BPPCInstance{
		NumBins: 2,
		BinCap:  5,
		Items: []BPPCItem{
			{ID: "a", Size: 3, StartBounds: []int{0, 1, 2}},  // Can start at 0,1,2 (end at 3,4,5)
			{ID: "b", Size: 2, StartBounds: []int{0, 1, 2, 3}},  // Can start at 0-3 (end at 2-5)
			{ID: "c", Size: 4, StartBounds: []int{0, 1}},  // Can start at 0-1 (end at 4-5)
		},
	}
	
	result := SolveBPPCBruteForce(instance)
	if result == nil {
		t.Fatal("Expected feasible=true but got nil")
	}
	
	t.Logf("✓ Success! Assignment: %v", result)
	
	// Verify placement fits in bins
	binUsage := make(map[int][]int)
	for itemID, placement := range result {
		binIdx := placement[0]
		startPos := placement[1]
		
		itemSize := getItemSizeByName(itemID, instance.Items)
		endPos := startPos + itemSize
		
		if endPos > instance.BinCap {
			t.Errorf("%s exceeds capacity: Bin%d [%d:%d) > Cap=%d", 
				itemID, binIdx, startPos, endPos, instance.BinCap)
		}
		
		binUsage[binIdx] = append(binUsage[binIdx], itemSize)
	}
}
