// Package finops provides cost analysis and reporting capabilities.
// Supports multi-dimensional cost attribution, trend forecasting,
// anomaly detection, and actionable optimization recommendations.
package finops

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Cost Analysis Engine
// ============================================================================

// CostAnalysisEngine provides comprehensive cost analysis and reporting.
type CostAnalysisEngine struct {
	costRecords      []CostRecord
	budgets          map[string]*Budget
	anomalies        []CostAnomaly
	optimizations    []OptimizationSuggestion
	config           CostAnalysisConfig
	mu               sync.RWMutex
	logger           *logrus.Logger
}

// CostAnalysisConfig configures the cost analysis engine.
type CostAnalysisConfig struct {
	AnalysisWindow    time.Duration `json:"analysis_window" yaml:"analysisWindow"`
	ForecastHorizon   int           `json:"forecast_horizon_days" yaml:"forecastHorizonDays"`
	AnomalyThreshold  float64       `json:"anomaly_threshold" yaml:"anomalyThreshold"` // std dev multiplier
	BudgetAlertPercent float64      `json:"budget_alert_percent" yaml:"budgetAlertPercent"` // e.g., 0.8 = 80%
}

// DefaultCostAnalysisConfig returns sensible defaults.
func DefaultCostAnalysisConfig() CostAnalysisConfig {
	return CostAnalysisConfig{
		AnalysisWindow:     90 * 24 * time.Hour,
		ForecastHorizon:    30,
		AnomalyThreshold:   2.0,
		BudgetAlertPercent: 0.8,
	}
}

// CostRecord represents a single cost data point.
type CostRecord struct {
	Date          time.Time         `json:"date"`
	TotalCost     float64           `json:"total_cost"`
	Currency      string            `json:"currency"`
	ByService     map[string]float64 `json:"by_service"`
	ByResource    map[string]float64 `json:"by_resource_type"`
	ByTeam        map[string]float64 `json:"by_team"`
	ByProject     map[string]float64 `json:"by_project"`
	ByRegion      map[string]float64 `json:"by_region"`
	ByEnvironment map[string]float64 `json:"by_environment"` // dev, staging, prod
	GPUCost       float64           `json:"gpu_cost"`
	ComputeCost   float64           `json:"compute_cost"`
	StorageCost   float64           `json:"storage_cost"`
	NetworkCost   float64           `json:"network_cost"`
	Provider      string            `json:"provider"`
}

// Budget represents a cost budget for a team/project.
type Budget struct {
	Name          string    `json:"name"`
	Type          string    `json:"type"` // team, project, service
	MonthlyLimit  float64   `json:"monthly_limit"`
	AlertPercent  float64   `json:"alert_percent"`
	CurrentSpend  float64   `json:"current_spend"`
	ForecastSpend float64   `json:"forecast_spend"`
	Status        string    `json:"status"` // ok, warning, exceeded
	Period        string    `json:"period"`
}

// CostAnomaly represents a detected cost anomaly.
type CostAnomaly struct {
	Date          time.Time `json:"date"`
	Service       string    `json:"service"`
	ExpectedCost  float64   `json:"expected_cost"`
	ActualCost    float64   `json:"actual_cost"`
	Deviation     float64   `json:"deviation_percent"`
	Severity      string    `json:"severity"` // low, medium, high, critical
	Description   string    `json:"description"`
	DetectedAt    time.Time `json:"detected_at"`
}

// OptimizationSuggestion represents a cost optimization recommendation.
type OptimizationSuggestion struct {
	ID              string  `json:"id"`
	Category        string  `json:"category"`  // rightsizing, idle, ri, spot, storage
	Resource        string  `json:"resource"`
	Description     string  `json:"description"`
	EstimatedSaving float64 `json:"estimated_monthly_saving"`
	Effort          string  `json:"effort"` // low, medium, high
	Impact          string  `json:"impact"` // low, medium, high
	Confidence      float64 `json:"confidence"` // 0-1
}

// CostReport is the comprehensive cost analysis report.
type CostReport struct {
	Period            string                  `json:"period"`
	TotalCost         float64                 `json:"total_cost"`
	PreviousPeriodCost float64                `json:"previous_period_cost"`
	CostChange        float64                 `json:"cost_change_percent"`
	Currency          string                  `json:"currency"`

	// Breakdown
	ByService         map[string]float64      `json:"by_service"`
	ByResource        map[string]float64      `json:"by_resource_type"`
	ByTeam            map[string]float64      `json:"by_team"`
	ByProject         map[string]float64      `json:"by_project"`
	ByRegion          map[string]float64      `json:"by_region"`
	ByEnvironment     map[string]float64      `json:"by_environment"`

	// GPU specific
	GPUCostBreakdown  *GPUCostBreakdown       `json:"gpu_cost_breakdown"`

	// Trends
	DailyTrend        []DailyCost             `json:"daily_trend"`
	Forecast          []DailyCost             `json:"forecast"`

	// Analysis
	TopCostDrivers    []CostDriver            `json:"top_cost_drivers"`
	Anomalies         []CostAnomaly           `json:"anomalies"`
	Optimizations     []OptimizationSuggestion `json:"optimizations"`
	BudgetStatus      []*Budget               `json:"budget_status"`

	// Summary
	TotalOptSavings   float64                 `json:"total_optimization_savings"`
	EfficiencyScore   float64                 `json:"efficiency_score"` // 0-100

	GeneratedAt       time.Time               `json:"generated_at"`
}

// DailyCost represents cost for a single day.
type DailyCost struct {
	Date time.Time `json:"date"`
	Cost float64   `json:"cost"`
}

// CostDriver represents a significant cost contributor.
type CostDriver struct {
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	Cost       float64 `json:"cost"`
	Percentage float64 `json:"percentage"`
	Trend      string  `json:"trend"` // increasing, decreasing, stable
	Change     float64 `json:"change_percent"`
}

// GPUCostBreakdown provides detailed GPU cost analysis.
type GPUCostBreakdown struct {
	TotalGPUCost     float64            `json:"total_gpu_cost"`
	PercentOfTotal   float64            `json:"percent_of_total"`
	ByGPUType        map[string]float64 `json:"by_gpu_type"`
	AvgUtilization   float64            `json:"avg_utilization"`
	IdleGPUCost      float64            `json:"idle_gpu_cost"`
	CostPerGPUHour   float64            `json:"cost_per_gpu_hour"`
}

// NewCostAnalysisEngine creates a new cost analysis engine.
func NewCostAnalysisEngine(cfg CostAnalysisConfig, logger *logrus.Logger) *CostAnalysisEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &CostAnalysisEngine{
		budgets: make(map[string]*Budget),
		config:  cfg,
		logger:  logger,
	}
}

// IngestCostData adds historical cost data.
func (e *CostAnalysisEngine) IngestCostData(records []CostRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.costRecords = append(e.costRecords, records...)

	// Sort by date
	sort.Slice(e.costRecords, func(i, j int) bool {
		return e.costRecords[i].Date.Before(e.costRecords[j].Date)
	})

	// Trim old data
	cutoff := time.Now().Add(-e.config.AnalysisWindow)
	trimmed := make([]CostRecord, 0, len(e.costRecords))
	for _, r := range e.costRecords {
		if r.Date.After(cutoff) {
			trimmed = append(trimmed, r)
		}
	}
	e.costRecords = trimmed
}

// SetBudget sets or updates a budget.
func (e *CostAnalysisEngine) SetBudget(name, budgetType string, monthlyLimit float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.budgets[name] = &Budget{
		Name:         name,
		Type:         budgetType,
		MonthlyLimit: monthlyLimit,
		AlertPercent: e.config.BudgetAlertPercent,
		Status:       "ok",
		Period:       time.Now().Format("2006-01"),
	}
}

// GenerateReport creates a comprehensive cost analysis report.
func (e *CostAnalysisEngine) GenerateReport(ctx context.Context, periodDays int) (*CostReport, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.costRecords) == 0 {
		return nil, fmt.Errorf("no cost data available")
	}

	now := time.Now()
	periodStart := now.AddDate(0, 0, -periodDays)
	prevPeriodStart := periodStart.AddDate(0, 0, -periodDays)

	// Filter records for current and previous periods
	var currentRecords, prevRecords []CostRecord
	for _, r := range e.costRecords {
		if r.Date.After(periodStart) {
			currentRecords = append(currentRecords, r)
		} else if r.Date.After(prevPeriodStart) {
			prevRecords = append(prevRecords, r)
		}
	}

	report := &CostReport{
		Period:        fmt.Sprintf("Last %d days", periodDays),
		Currency:      "USD",
		ByService:     make(map[string]float64),
		ByResource:    make(map[string]float64),
		ByTeam:        make(map[string]float64),
		ByProject:     make(map[string]float64),
		ByRegion:      make(map[string]float64),
		ByEnvironment: make(map[string]float64),
		GeneratedAt:   now,
	}

	// Aggregate current period
	for _, r := range currentRecords {
		report.TotalCost += r.TotalCost
		for k, v := range r.ByService {
			report.ByService[k] += v
		}
		for k, v := range r.ByResource {
			report.ByResource[k] += v
		}
		for k, v := range r.ByTeam {
			report.ByTeam[k] += v
		}
		for k, v := range r.ByProject {
			report.ByProject[k] += v
		}
		for k, v := range r.ByRegion {
			report.ByRegion[k] += v
		}
		for k, v := range r.ByEnvironment {
			report.ByEnvironment[k] += v
		}
	}

	// Previous period total
	for _, r := range prevRecords {
		report.PreviousPeriodCost += r.TotalCost
	}

	// Cost change
	if report.PreviousPeriodCost > 0 {
		report.CostChange = (report.TotalCost - report.PreviousPeriodCost) / report.PreviousPeriodCost * 100
	}

	// Daily trend
	report.DailyTrend = e.buildDailyTrend(currentRecords)

	// GPU breakdown
	report.GPUCostBreakdown = e.buildGPUBreakdown(currentRecords, report.TotalCost)

	// Top cost drivers
	report.TopCostDrivers = e.identifyCostDrivers(report)

	// Anomaly detection
	report.Anomalies = e.detectAnomalies(currentRecords)

	// Optimization suggestions
	report.Optimizations = e.generateOptimizations(currentRecords, report)
	for _, opt := range report.Optimizations {
		report.TotalOptSavings += opt.EstimatedSaving
	}

	// Forecast
	report.Forecast = e.forecastCosts(report.DailyTrend)

	// Budget status
	report.BudgetStatus = e.evaluateBudgets(currentRecords, periodDays)

	// Efficiency score
	report.EfficiencyScore = e.calculateEfficiencyScore(report)

	e.logger.WithFields(logrus.Fields{
		"period":           periodDays,
		"total_cost":       fmt.Sprintf("$%.2f", report.TotalCost),
		"cost_change":      fmt.Sprintf("%.1f%%", report.CostChange),
		"anomalies":        len(report.Anomalies),
		"optimizations":    len(report.Optimizations),
		"efficiency_score": report.EfficiencyScore,
	}).Info("Cost analysis report generated")

	return report, nil
}

func (e *CostAnalysisEngine) buildDailyTrend(records []CostRecord) []DailyCost {
	daily := make(map[string]float64)
	for _, r := range records {
		key := r.Date.Format("2006-01-02")
		daily[key] += r.TotalCost
	}

	trend := make([]DailyCost, 0, len(daily))
	for dateStr, cost := range daily {
		date, _ := time.Parse("2006-01-02", dateStr)
		trend = append(trend, DailyCost{Date: date, Cost: cost})
	}

	sort.Slice(trend, func(i, j int) bool {
		return trend[i].Date.Before(trend[j].Date)
	})
	return trend
}

func (e *CostAnalysisEngine) buildGPUBreakdown(records []CostRecord, totalCost float64) *GPUCostBreakdown {
	breakdown := &GPUCostBreakdown{
		ByGPUType: make(map[string]float64),
	}

	for _, r := range records {
		breakdown.TotalGPUCost += r.GPUCost
	}

	if totalCost > 0 {
		breakdown.PercentOfTotal = breakdown.TotalGPUCost / totalCost * 100
	}

	return breakdown
}

func (e *CostAnalysisEngine) identifyCostDrivers(report *CostReport) []CostDriver {
	var drivers []CostDriver

	for svc, cost := range report.ByService {
		pct := 0.0
		if report.TotalCost > 0 {
			pct = cost / report.TotalCost * 100
		}
		drivers = append(drivers, CostDriver{
			Name:       svc,
			Category:   "service",
			Cost:       cost,
			Percentage: pct,
			Trend:      "stable",
		})
	}

	sort.Slice(drivers, func(i, j int) bool {
		return drivers[i].Cost > drivers[j].Cost
	})

	if len(drivers) > 10 {
		drivers = drivers[:10]
	}
	return drivers
}

func (e *CostAnalysisEngine) detectAnomalies(records []CostRecord) []CostAnomaly {
	if len(records) < 7 {
		return nil
	}

	// Calculate rolling average and std dev
	costs := make([]float64, len(records))
	for i, r := range records {
		costs[i] = r.TotalCost
	}

	mean := calcMean(costs)
	stddev := calcStdDev(costs, mean)

	var anomalies []CostAnomaly
	for _, r := range records {
		deviation := (r.TotalCost - mean) / stddev
		if math.Abs(deviation) > e.config.AnomalyThreshold {
			severity := "low"
			if math.Abs(deviation) > 3 {
				severity = "high"
			} else if math.Abs(deviation) > 4 {
				severity = "critical"
			} else if math.Abs(deviation) > 2.5 {
				severity = "medium"
			}

			anomalies = append(anomalies, CostAnomaly{
				Date:         r.Date,
				Service:      "aggregate",
				ExpectedCost: mean,
				ActualCost:   r.TotalCost,
				Deviation:    deviation * 100,
				Severity:     severity,
				Description:  fmt.Sprintf("Cost %.0f%% above expected ($%.2f vs $%.2f)", (r.TotalCost-mean)/mean*100, r.TotalCost, mean),
				DetectedAt:   time.Now(),
			})
		}
	}
	return anomalies
}

func (e *CostAnalysisEngine) generateOptimizations(records []CostRecord, report *CostReport) []OptimizationSuggestion {
	var suggestions []OptimizationSuggestion
	idCounter := 1

	// Idle resource detection
	if report.GPUCostBreakdown != nil && report.GPUCostBreakdown.AvgUtilization < 0.3 {
		suggestions = append(suggestions, OptimizationSuggestion{
			ID:              fmt.Sprintf("OPT-%03d", idCounter),
			Category:        "idle",
			Resource:        "GPU instances",
			Description:     fmt.Sprintf("GPU utilization is low (%.0f%%). Consider rightsizing or using spot instances.", report.GPUCostBreakdown.AvgUtilization*100),
			EstimatedSaving: report.GPUCostBreakdown.TotalGPUCost * 0.3,
			Effort:          "medium",
			Impact:          "high",
			Confidence:      0.8,
		})
		idCounter++
	}

	// Environment cost optimization
	if devCost, ok := report.ByEnvironment["dev"]; ok {
		if devCost > report.TotalCost*0.3 {
			suggestions = append(suggestions, OptimizationSuggestion{
				ID:              fmt.Sprintf("OPT-%03d", idCounter),
				Category:        "rightsizing",
				Resource:        "Development environment",
				Description:     fmt.Sprintf("Dev environment costs %.0f%% of total. Consider scheduling dev resources to auto-stop off-hours.", devCost/report.TotalCost*100),
				EstimatedSaving: devCost * 0.5,
				Effort:          "low",
				Impact:          "medium",
				Confidence:      0.9,
			})
			idCounter++
		}
	}

	// Storage optimization
	if storageCost, ok := report.ByResource["storage"]; ok {
		if storageCost > report.TotalCost*0.2 {
			suggestions = append(suggestions, OptimizationSuggestion{
				ID:              fmt.Sprintf("OPT-%03d", idCounter),
				Category:        "storage",
				Resource:        "Storage volumes",
				Description:     "Storage costs are significant. Consider lifecycle policies, compression, or tiered storage.",
				EstimatedSaving: storageCost * 0.25,
				Effort:          "medium",
				Impact:          "medium",
				Confidence:      0.7,
			})
			idCounter++
		}
	}

	// Network cost optimization
	if networkCost, ok := report.ByResource["network"]; ok {
		if networkCost > report.TotalCost*0.15 {
			suggestions = append(suggestions, OptimizationSuggestion{
				ID:              fmt.Sprintf("OPT-%03d", idCounter),
				Category:        "network",
				Resource:        "Data transfer",
				Description:     "High data transfer costs. Consider CDN, data compression, or regional data placement.",
				EstimatedSaving: networkCost * 0.2,
				Effort:          "high",
				Impact:          "medium",
				Confidence:      0.6,
			})
			idCounter++
		}
	}

	// Spot instance suggestion
	if computeCost, ok := report.ByResource["compute"]; ok {
		suggestions = append(suggestions, OptimizationSuggestion{
			ID:              fmt.Sprintf("OPT-%03d", idCounter),
			Category:        "spot",
			Resource:        "Compute instances",
			Description:     "Evaluate fault-tolerant workloads for spot instance migration. Typical savings: 60-90%.",
			EstimatedSaving: computeCost * 0.3,
			Effort:          "medium",
			Impact:          "high",
			Confidence:      0.75,
		})
		idCounter++
	}

	// Sort by estimated savings
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].EstimatedSaving > suggestions[j].EstimatedSaving
	})

	return suggestions
}

func (e *CostAnalysisEngine) forecastCosts(dailyTrend []DailyCost) []DailyCost {
	if len(dailyTrend) < 7 {
		return nil
	}

	// Extract values for forecasting
	values := make([]float64, len(dailyTrend))
	for i, d := range dailyTrend {
		values[i] = d.Cost
	}

	// Simple linear regression for trend + EMA for short-term
	trend := calcTrend(values)
	lastEMA := calcEMA(values, 0.3)
	lastDate := dailyTrend[len(dailyTrend)-1].Date

	forecast := make([]DailyCost, e.config.ForecastHorizon)
	for i := 0; i < e.config.ForecastHorizon; i++ {
		date := lastDate.AddDate(0, 0, i+1)
		predictedCost := lastEMA + trend*float64(i+1)
		if predictedCost < 0 {
			predictedCost = 0
		}
		forecast[i] = DailyCost{Date: date, Cost: predictedCost}
	}

	return forecast
}

func (e *CostAnalysisEngine) evaluateBudgets(records []CostRecord, periodDays int) []*Budget {
	budgets := make([]*Budget, 0, len(e.budgets))

	// Calculate per-team/project spending
	teamSpend := make(map[string]float64)
	projectSpend := make(map[string]float64)
	for _, r := range records {
		for team, cost := range r.ByTeam {
			teamSpend[team] += cost
		}
		for proj, cost := range r.ByProject {
			projectSpend[proj] += cost
		}
	}

	for name, budget := range e.budgets {
		b := *budget
		switch b.Type {
		case "team":
			b.CurrentSpend = teamSpend[name]
		case "project":
			b.CurrentSpend = projectSpend[name]
		}

		// Forecast monthly spending
		if periodDays > 0 {
			dailyRate := b.CurrentSpend / float64(periodDays)
			b.ForecastSpend = dailyRate * 30
		}

		// Status
		if b.MonthlyLimit > 0 {
			ratio := b.ForecastSpend / b.MonthlyLimit
			if ratio > 1.0 {
				b.Status = "exceeded"
			} else if ratio > b.AlertPercent {
				b.Status = "warning"
			} else {
				b.Status = "ok"
			}
		}

		budgets = append(budgets, &b)
	}

	return budgets
}

func (e *CostAnalysisEngine) calculateEfficiencyScore(report *CostReport) float64 {
	score := 100.0

	// Penalize for anomalies
	score -= float64(len(report.Anomalies)) * 5

	// Penalize for cost increase
	if report.CostChange > 20 {
		score -= (report.CostChange - 20) * 0.5
	}

	// Reward for optimizations implemented (based on optimization potential)
	if report.TotalCost > 0 && report.TotalOptSavings > 0 {
		savingsRatio := report.TotalOptSavings / report.TotalCost
		if savingsRatio > 0.3 {
			score -= 10 // Too many unimplemented optimizations
		}
	}

	// Penalize for budget overruns
	for _, b := range report.BudgetStatus {
		if b.Status == "exceeded" {
			score -= 10
		} else if b.Status == "warning" {
			score -= 3
		}
	}

	return math.Max(0, math.Min(100, score))
}
