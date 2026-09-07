// Package helpers provides common utility functions used across Red Team modules.
// ALL duplicate min/max functions in other files should be removed and replaced with:
//   - helpers.MinInt()
//   - helpers.MaxInt()
//   - helpers.MinFloat64()
//   - helpers.MaxFloat64()
//   - helpers.MinDuration()
//   - helpers.MaxDuration()
// This package is the SINGLE SOURCE OF TRUTH for helper functions.
package helpers