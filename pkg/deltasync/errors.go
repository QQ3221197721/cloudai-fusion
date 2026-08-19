package deltasync

import "errors"

var (
	// ErrInvalidChunkSize is returned when Min/Normal/Max bounds are not a
	// valid non-decreasing positive triple.
	ErrInvalidChunkSize = errors.New("deltasync: invalid chunk size (require 0 < min <= normal <= max)")
	// ErrEmptyTree is returned when a Merkle operation is attempted on a tree
	// with no leaves.
	ErrEmptyTree = errors.New("deltasync: empty merkle tree")
	// ErrShapeMismatch is returned when two Merkle trees with incompatible
	// leaf counts are diffed structurally.
	ErrShapeMismatch = errors.New("deltasync: merkle tree leaf-count mismatch")
)
