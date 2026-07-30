package edgeautonomy_test

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/edgeautonomy"
	"github.com/stretchr/testify/assert"
)

func TestVersionVector_IntegrationTests(t *testing.T) {
	t.Run("Multiple nodes can update independently", func(t *testing.T) {
		nodeIDs := []string{"central", "edge-1", "edge-2"}
		vv := edgeautonomy.NewVersionVector(nodeIDs)
		
		// Each node updates its own component
		vec1 := vv.Update("central")
		vec2 := vv.Update("edge-1")
		vec3 := vv.Update("edge-2")
		
		assert.Equal(t, 3, len(vec1))
		assert.Equal(t, 1, vec1[0]) // Central incremented
		assert.Equal(t, 0, vec1[1]) // Others unchanged
		
		assert.Equal(t, 1, vec2[1]) // Edge-1 incremented
		assert.Equal(t, 0, vec2[0]) // Central unchanged from its perspective
	})
	
	t.Run("Merge with external vector updates local state", func(t *testing.T) {
		vv := edgeautonomy.NewVersionVector([]string{"n1", "n2"})
		
		// Local vector
		localVec := vv.Update("n1")
		assert.Equal(t, 1, localVec[0])
		
		// External vector from another node
		externalVec := []int{0, 5} // n2 has value 5
		
		err := vv.MergeWith("n2", externalVec)
		assert.NoError(t, err)
		
		// Verify merge occurred
		newVec, _ := vv.GetVector("n2")
		assert.Equal(t, 5, newVec[1])
	})
	
	t.Run("Handle invalid inputs gracefully", func(t *testing.T) {
		vv := edgeautonomy.NewVersionVector([]string{"n1"})
		
		// Compare incompatible sizes
		result := vv.Compare([]int{1}, []int{1, 2})
		assert.Equal(t, edgeautonomy.UNKNOWN_RELATIONSHIP, result)
		
		// Get vector for unknown node
		_, err := vv.GetVector("unknown-node")
		assert.Error(t, err)
	})
}
