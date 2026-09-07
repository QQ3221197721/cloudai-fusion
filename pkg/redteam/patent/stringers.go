package patent

import "strconv"

// Float64Min returns the minimum of two float64 values (Go 1.21+ has math.Min, but we need sentinel)
// MinFloat64 returns the minimum valid float64 value
const MinFloat64 = float64(-(1 << 53) + 1)

// String implements fmt.Stringer interface for StateID
func (sid StateID) String() string {
	return strconv.FormatUint(uint64(sid), 10)
}

// String implements fmt.Stringer interface for ActionID
func (aid ActionID) String() string {
	return strconv.Itoa(int(aid))
}
