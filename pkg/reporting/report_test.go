package reporting

// Helper for report comparison without worrying about GeneratedAt timestamps.
func equalReports(a, b *Report) bool {
	if a.RowCount != b.RowCount {
		return false
	}
	if len(a.Rows) != len(b.Rows) {
		return false
	}
	for i := range a.Rows {
		x, y := &a.Rows[i], &b.Rows[i]
		if x.Count != y.Count || x.Quantity != y.Quantity || x.Cost != y.Cost {
			return false
		}
		if len(x.Keys) != len(y.Keys) {
			return false
		}
		for k := range x.Keys {
			if x.Keys[k] != y.Keys[k] {
				return false
			}
		}
	}
	return true
}
