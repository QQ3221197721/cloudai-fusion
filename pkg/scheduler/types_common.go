package scheduler

// Logger is a common logging interface for the scheduler package.
type Logger interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Warnf(format string, args ...interface{})
}

// TaskSpec defines a GPU migration task specification.
type TaskSpec struct {
	ID           string `json:"id"`
	TargetHost   string `json:"target_host"`
	ContainerPID int    `json:"container_pid"`
	GPUID        string `json:"gpu_id"`
	Priority     int    `json:"priority"`
}
