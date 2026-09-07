package defensive

import (
	"time"
	
	"github.com/google/uuid"
)

// RequestContext holds typed request metadata for safe access
type RequestContext struct {
	RequestID string
	Method    string
	Path      string
	UserID    string
	IP        string
}

func generateRequestID() string {
	return uuid.New().String()
}

// Time utilities
func ParseTime(s, layout string) (time.Time, error) {
	return time.ParseInLocation(layout, s, time.Local)
}

func NowUTC() time.Time {
	return time.Now().UTC()
}

func CoalesceDuration(d1, d2 time.Duration) time.Duration {
	if d1 > 0 {
		return d1
	}
	return d2
}
