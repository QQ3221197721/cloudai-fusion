// Package formatter implements consistent time formatting across the application
package formatter

import (
	"strings"
	"time"
)

const (
	// ISO8601 format for API responses (strict requirement)
	ISO8601Format = "2006-01-02T15:04:05Z"
	
	// Natural language format for reports (Chinese user-friendly)
	ReportTimeFormatCN = "1 月 2 日 15:04"    // e.g., "7 月 9 日 07:00"
	ReportDateOnlyCN   = "2006 年 1 月 2 日"  // e.g., "2026 年 7 月 9 日"
	
	// Full datetime with date only (for detailed reports)
	FullDatetimeCN = "2006 年 1 月 2 日 15:04" // e.g., "2026 年 7 月 9 日 07:00"
)

// TimeFormatter provides unified time formatting capabilities
type TimeFormatter struct {
	timezone *time.Location
}

func NewTimeFormatter(timezone string) (*TimeFormatter, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.Local
	}
	
	return &TimeFormatter{timezone: loc}, nil
}

// FormatForAPI converts time to strict ISO 8601 UTC format
// Used for ALL API response fields (conflictResolutions.newStart/newEnd, planned_start/end, etc.)
func (tf *TimeFormatter) FormatForAPI(t time.Time) string {
	return t.In(tf.timezone).UTC().Format(time.RFC3339)
}

// FormatForReport converts time to Chinese natural language format
// Used ONLY in human-readable reports (adjustment comparison reports, scheduling basis reports)
func (tf *TimeFormatter) FormatForReport(t time.Time, useDateOnly bool) string {
	if useDateOnly {
		return t.In(tf.timezone).Format(ReportDateOnlyCN)
	}
	
	return t.In(tf.timezone).Format(ReportTimeFormatCN)
}

// FormatFullDateTimeChinese returns full datetime in Chinese format
func (tf *TimeFormatter) FormatFullDateTimeChinese(t time.Time) string {
	return t.In(tf.timezone).Format(FullDatetimeCN)
}

// ValidateAndNormalize ensures time is in correct format based on context
func (tf *TimeFormatter) ValidateAndNormalize(input string, context string) (time.Time, error) {
	var parsedTime time.Time
	var err error
	
	switch context {
	case "api_response", "schedule_api", "conflict_resolution":
		// Strict ISO 8601 required
		parsedTime, err = time.Parse(time.RFC3339, input)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid ISO8601 format for API: %w", err)
		}
		
	case "report_display", "human_report":
		// Can accept multiple formats for user convenience
		formats := []string{
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05",
			"2006/01/02 15:04:05",
		}
		
		for _, fmt := range formats {
			parsedTime, err = time.Parse(fmt, input)
			if err == nil {
				break
			}
		}
		
		if err != nil {
			return time.Time{}, fmt.Errorf("unknown time format: %s", input)
		}
		
	default:
		return time.Time{}, fmt.Errorf("unknown validation context: %s", context)
	}
	
	return parsedTime, nil
}

// NormalizeAllTimesInSlice iterates through slice of times and ensures all are properly formatted
func (tf *TimeFormatter) NormalizeAllTimesInSlice(times []time.Time) []string {
	formatted := make([]string, len(times))
	
	for i, t := range times {
		formatted[i] = tf.FormatForAPI(t)
	}
	
	return formatted
}
