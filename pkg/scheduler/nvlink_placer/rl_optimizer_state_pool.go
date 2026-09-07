package scheduler

import "sync"

// schedulingStatePool reuses SchedulingState structs across calls (zero-allocation hot path)
var schedulingStatePool = sync.Pool{
	New: func() interface{} {
		return &SchedulingState{} // Pre-allocate reusable state struct
	},
}

// acquireState gets a reusable SchedulingState from pool
func acquireState() *SchedulingState {
	return schedulingStatePool.Get().(*SchedulingState)
}

// releaseState returns SchedulingState to pool after zeroing out fields
func releaseState(s *SchedulingState) {
	// Zero out fields before returning to pool
	s.WorkloadType = ""
	s.GPUCountBucket = ""
	s.PriorityBucket = ""
	s.ClusterLoadLevel = ""
	s.TimeOfDay = ""
	
	schedulingStatePool.Put(s)
}
