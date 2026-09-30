// Package pipeline includes validation utilities for Pipeline and PipelineRun.
package pipeline

import "fmt"

// Validate checks that a Pipeline has required fields configured correctly.
func (p *Pipeline) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}
	
	if p.Source.Type == "" {
		return fmt.Errorf("source type is required")
	}
	
	if p.Source.ConnStr == "" {
		return fmt.Errorf("source connection string is required")
	}
	
	if p.Target.Type == "" {
		return fmt.Errorf("target type is required")
	}
	
	if p.Target.ConnStr == "" {
		return fmt.Errorf("target connection string is required")
	}
	
	// Validate schedule if present
	if p.Schedule != nil {
		if err := validateSchedule(p.Schedule); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
	}
	
	// Validate transformations
	for i, rule := range p.Transformations {
		if rule.ID == "" {
			return fmt.Errorf("transformation rule %d missing ID", i)
		}
		if rule.Name == "" {
			return fmt.Errorf("transformation rule %d (%s) missing name", i, rule.ID)
		}
		if rule.Order < 0 {
			return fmt.Errorf("transformation rule %d has invalid order: %d", i, rule.Order)
		}
	}
	
	return nil
}

func validateSchedule(s *ScheduleConfig) error {
	switch s.Type {
	case ScheduleCron:
		if s.CronExpr == "" {
			return fmt.Errorf("cron expression required for cron schedule")
		}
		// TODO: validate cron expression format
	case ScheduleInterval:
		if s.Interval == "" {
			return fmt.Errorf("interval required for interval schedule")
		}
	case ScheduleOnce:
		// Once schedules are valid without additional config
	default:
		return fmt.Errorf("unknown schedule type: %s", s.Type)
	}
	return nil
}
