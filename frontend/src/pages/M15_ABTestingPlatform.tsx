/**
 * M15 A/B Testing Platform & Experiment Management - Production-Grade Dashboard
 * 
 * Complete user journey: Experiment Configuration → Live Results → Traffic Routing → Analytics Insights
 * Implements real backend API integration with CloudAI Fusion experiment tracking endpoints
 * Design Philosophy: Linear-style dark theme, statistical rigor, data science aesthetics
 */

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import axios from "axios";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import {
  Target,
  Activity,
  Plus,
  Play,
  Pause,
  RefreshCw,
  TrendingUp,
  Settings,
  BarChart3,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  Database,
  Code,
  Zap,
  ArrowUpDown,
  Scale,
  GitBranch,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface Experiment {
  id: string;
  name: string;
  hypothesis: string;
  variants: Array<{
    id: string;
    name: string;
    traffic_percentage: number;
    status: 'active' | 'stopped';
  }>;
  primary_metric: string;
  secondary_metrics: string[];
  start_time?: string;
  end_time?: string;
  status: 'planning' | 'running' | 'completed' | 'stopped';
  results?: ExperimentResults;
}

interface ExperimentResults {
  variant_id: string;
  sample_size: number;
  metric_value: number;
  confidence_interval: [number, number];
  p_value: number;
  statistically_significant: boolean;
  lift_vs_baseline?: number;
}

interface CreateExperimentRequest {
  name: string;
  hypothesis: string;
  baseline_variant: string;
  challenger_variants: Array<{ name: string; traffic_percentage: number }>;
  primary_metric: string;
  secondary_metrics: string[];
  target_sample_size: number;
  duration_days: number;
}

interface TrafficSplit {
  experiment_id: string;
  variant_id: string;
  current_percentage: number;
  routed_requests: number;
  last_updated: string;
}

// ============================================================================
// Components
// ============================================================================

const StatusBadge = ({ status }: { status: string }) => {
  const statusStyles: Record<string, string> = {
    planning: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
    running: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    completed: "bg-green-500/20 text-green-400 border-green-500/30",
    stopped: "bg-gray-500/20 text-gray-400 border-gray-500/30",
  };

  const style = statusStyles[status.toLowerCase()] || "bg-gray-500/20 text-gray-400";

  return (
    <Badge className={`${style} border font-semibold`} variant="outline">
      {status.toUpperCase()}
    </Badge>
  );
};

const SignificanceBadge = ({ significant, p_value }: { significant: boolean; p_value: number }) => (
  <div className="flex items-center gap-2">
    {significant ? (
      <Badge className="bg-green-500/20 text-green-400 border-green-500/30">
        <CheckCircle2 className="w-3 h-3 mr-1" />
        Significant (p={p_value.toFixed(4)})
      </Badge>
    ) : (
      <Badge className="bg-orange-500/20 text-orange-400 border-orange-500/30">
        <XCircle className="w-3 h-3 mr-1" />
        Not Significant (p={p_value.toFixed(4)})
      </Badge>
    )}
  </div>
);

const MetricCard = ({ icon: Icon, title, value, trend, color = "blue" }: any) => (
  <Card className={`border-slate-700 bg-slate-800/50`}>
    <CardContent className="p-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium text-slate-400">{title}</p>
          <p className="text-2xl font-bold text-white mt-1">{value}</p>
          {trend && (
            <p className="text-xs text-green-400 mt-1 flex items-center gap-1">
              <TrendingUp className="w-3 h-3" />
              {trend}
            </p>
          )}
        </div>
        <Icon className={`w-8 h-8 text-${color}-500`} />
      </div>
    </CardContent>
  </Card>
);

const TrafficPieChart = ({ variants }: { variants: Array<{ name: string; percentage: number; color: string }> }) => {
  const total = variants.reduce((sum, v) => sum + v.percentage, 0);
  
  let cumulativePercent = 0;
  
  return (
    <div className="flex items-center justify-center space-x-8">
      {/* Pie Chart Visualization */}
      <div className="relative w-48 h-48">
        <svg viewBox="0 0 100 100" className="w-full h-full transform -rotate-90">
          {variants.map((variant, index) => {
            const slicePercent = (variant.percentage / total) * 100;
            const circumference = 2 * Math.PI * 40;
            const strokeDasharray = (slicePercent / 100) * circumference;
            const strokeDashoffset = -cumulativePercent / 100 * circumference;
            
            cumulativePercent += variant.percentage;
            
            return (
              <circle
                key={variant.name}
                cx="50"
                cy="50"
                r="40"
                fill="transparent"
                stroke={variant.color}
                strokeWidth="20"
                strokeDasharray={`${strokeDasharray} ${circumference}`}
                strokeDashoffset={strokeDashoffset}
                className="transition-all duration-300"
              />
            );
          })}
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="text-center">
            <div className="text-2xl font-bold text-white">100%</div>
            <div className="text-xs text-slate-400">Total</div>
          </div>
        </div>
      </div>

      {/* Legend */}
      <div className="space-y-2">
        {variants.map((variant, index) => (
          <div key={variant.name} className="flex items-center gap-2">
            <div className="w-3 h-3 rounded-full" style={{ backgroundColor: variant.color }} />
            <div className="text-sm">
              <span className="text-white font-medium">{variant.name}</span>
              <span className="text-slate-400 ml-2">{variant.percentage}%</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

const CreateExperimentModal = ({
  isOpen,
  onClose,
  onSubmit,
  isLoading,
}: {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: CreateExperimentRequest) => void;
  isLoading: boolean;
}) => {
  if (!isOpen) return null;

  const [formData, setFormData] = useState<CreateExperimentRequest>({
    name: "",
    hypothesis: "",
    baseline_variant: "control",
    challenger_variants: [{ name: "variant-a", traffic_percentage: 50 }],
    primary_metric: "conversion_rate",
    secondary_metrics: [],
    target_sample_size: 10000,
    duration_days: 14,
  });

  const addChallenger = () => {
    setFormData({
      ...formData,
      challenger_variants: [...formData.challenger_variants, { name: `variant-${formData.challenger_variants.length + 1}`, traffic_percentage: 0 }]
    });
  };

  const updateChallenger = (index: number, field: keyof typeof formData.challenger_variants[0], value: string | number) => {
    const updated = [...formData.challenger_variants];
    updated[index] = { ...updated[index], [field]: value };
    setFormData({ ...formData, challenger_variants: updated });
  };

  const removeChallenger = (index: number) => {
    setFormData({
      ...formData,
      challenger_variants: formData.challenger_variants.filter((_, i) => i !== index)
    });
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(formData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-4xl border-slate-700 bg-slate-900 max-h-[90vh] overflow-y-auto">
        <CardHeader className="border-b border-slate-700 sticky top-0 bg-slate-900 z-10">
          <h2 className="text-2xl font-bold text-white">Create A/B Test Experiment</h2>
          <p className="text-sm text-slate-400">Design and configure multi-variant experiments with statistical rigor</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-6 pt-6">
            {/* Basic Information */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Target className="w-4 h-4" />
                Experiment Overview
              </h4>
              <div className="space-y-2">
                <Label htmlFor="name" className="text-slate-300">Experiment Name *</Label>
                <Input
                  id="name"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  placeholder="llm-inference-latency-optimization"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="hypothesis" className="text-slate-300">Hypothesis *</Label>
                <Input
                  id="hypothesis"
                  value={formData.hypothesis}
                  onChange={(e) => setFormData({ ...formData, hypothesis: e.target.value })}
                  placeholder="Using speculative decoding reduces LLM inference latency by 30% without accuracy degradation..."
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
            </div>

            {/* Variants Configuration */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <GitBranch className="w-4 h-4" />
                Variant Configuration
              </h4>
              
              {/* Baseline */}
              <div className="p-4 bg-green-500/10 border border-green-500/30 rounded-lg">
                <p className="text-sm font-semibold text-green-400 mb-2">Baseline (Control)</p>
                <p className="text-sm text-slate-300">{formData.baseline_variant}</p>
                <p className="text-xs text-slate-400 mt-1">Reference implementation - no traffic splitting</p>
              </div>

              {/* Challengers */}
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <p className="text-sm font-semibold text-slate-300">Challenger Variants</p>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={addChallenger}
                    className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <Plus className="w-3 h-3 mr-1" />
                    Add Variant
                  </Button>
                </div>
                
                {formData.challenger_variants.map((challenger, index) => (
                  <div key={index} className="p-4 bg-slate-800 rounded-lg border border-slate-700 space-y-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <Input
                          value={challenger.name}
                          onChange={(e) => updateChallenger(index, 'name', e.target.value)}
                          placeholder="variant-name"
                          className="bg-slate-700 border-slate-600 text-white w-48"
                        />
                        {formData.challenger_variants.length > 1 && (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            onClick={() => removeChallenger(index)}
                            className="border-red-500/30 text-red-400 hover:bg-red-500/10"
                          >
                            <XCircle className="w-3 h-3" />
                          </Button>
                        )}
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-sm text-slate-400">Traffic %:</span>
                        <Input
                          type="number"
                          min={0}
                          max={100}
                          value={challenger.traffic_percentage}
                          onChange={(e) => updateChallenger(index, 'traffic_percentage', parseInt(e.target.value) || 0)}
                          className="bg-slate-700 border-slate-600 text-white w-20"
                        />
                      </div>
                    </div>
                    <Slider
                      min={0}
                      max={100}
                      step={5}
                      value={[challenger.traffic_percentage]}
                      onValueChange={([val]) => updateChallenger(index, 'traffic_percentage', val)}
                      className="py-2"
                    />
                  </div>
                ))}
              </div>

              {/* Traffic Distribution Warning */}
              {formData.challenger_variants.reduce((sum, v) => sum + v.traffic_percentage, 0) !== 100 && (
                <Alert className="bg-orange-500/10 border-orange-500/30 text-orange-400">
                  <Scale className="w-4 h-4" />
                  <AlertTitle>Traffic Split Validation</AlertTitle>
                  <AlertDescription>
                    Total challenger traffic must equal 100%. Current allocation: {formData.challenger_variants.reduce((sum, v) => sum + v.traffic_percentage, 0)}%
                  </AlertDescription>
                </Alert>
              )}
            </div>

            {/* Metrics Selection */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Activity className="w-4 h-4" />
                Success Metrics
              </h4>
              <div className="space-y-2">
                <Label htmlFor="primary_metric" className="text-slate-300">Primary Metric *</Label>
                <select
                  id="primary_metric"
                  value={formData.primary_metric}
                  onChange={(e) => setFormData({ ...formData, primary_metric: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="conversion_rate">Conversion Rate</option>
                  <option value="latency_ms">Latency (ms)</option>
                  <option value="throughput_rps">Throughput (RPS)</option>
                  <option value="accuracy">Accuracy (%)</option>
                  <option value="cost_per_inference">Cost per Inference ($)</option>
                </select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="secondary_metrics" className="text-slate-300">Secondary Metrics (Optional)</Label>
                <div className="grid grid-cols-2 gap-2">
                  {['latency_p99', 'error_rate', 'gpu_utilization', 'cost_per_request'].map(metric => (
                    <label key={metric} className="flex items-center gap-2 text-sm text-slate-300">
                      <input
                        type="checkbox"
                        checked={formData.secondary_metrics.includes(metric)}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setFormData({ ...formData, secondary_metrics: [...formData.secondary_metrics, metric] });
                          } else {
                            setFormData({
                              ...formData,
                              secondary_metrics: formData.secondary_metrics.filter(m => m !== metric)
                            });
                          }
                        }}
                        className="rounded bg-slate-700 border-slate-600 text-blue-600"
                      />
                      {metric.replace(/_/g, ' ').split(' ').map(word => word.charAt(0).toUpperCase() + word.slice(1)).join(' ')}
                    </label>
                  ))}
                </div>
              </div>
            </div>

            {/* Sample Size & Duration */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Database className="w-4 h-4" />
                Statistical Power Configuration
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="target_sample_size" className="text-slate-300">Target Sample Size *</Label>
                  <Input
                    id="target_sample_size"
                    type="number"
                    value={formData.target_sample_size}
                    onChange={(e) => setFormData({ ...formData, target_sample_size: parseInt(e.target.value) || 10000 })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                  <p className="text-xs text-slate-400">Minimum samples needed for statistical significance</p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="duration_days" className="text-slate-300">Duration (days) *</Label>
                  <Input
                    id="duration_days"
                    type="number"
                    min={1}
                    max={90}
                    value={formData.duration_days}
                    onChange={(e) => setFormData({ ...formData, duration_days: parseInt(e.target.value) || 14 })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                  <p className="text-xs text-slate-400">Expected experiment duration</p>
                </div>
              </div>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4 border-t border-slate-700">
              <Button
                type="button"
                onClick={onClose}
                variant="outline"
                disabled={isLoading}
                className="border-slate-600 text-slate-300 hover:bg-slate-800"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isLoading}
                className="bg-blue-600 hover:bg-blue-700 text-white"
              >
                {isLoading ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Creating...
                  </>
                ) : (
                  <>
                    <Plus className="w-4 h-4 mr-2" />
                    Create Experiment
                  </>
                )}
              </Button>
            </div>
          </CardContent>
        </form>
      </Card>
    </div>
  );
};

// ============================================================================
// Main Component
// ============================================================================

const M15ABTestingPlatform = () => {
  const queryClient = useQueryClient();
  
  // State
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [activeTab, setActiveTab] = useState("experiments");
  const [createLoading, setCreateLoading] = useState(false);

  // Fetch experiments
  const { data: experimentsData, isLoading: loadingExperiments, error: errorExperiments } = useQuery<{ experiments: Experiment[] }>({
    queryKey: ["experiments"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/experiments`);
      return response.data;
    },
  });

  // Fetch traffic splits
  const { data: trafficSplits, isLoading: loadingSplits } = useQuery<{ splits: TrafficSplit[] }>({
    queryKey: ["traffic-splits"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/traffic/splits`);
      return response.data;
    },
  });

  // Create experiment mutation
  const createExperimentMutation = useMutation({
    mutationFn: async (data: CreateExperimentRequest) => {
      const response = await axios.post(`${API_BASE_URL}/api/v1/experiments`, data);
      return response.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["experiments"] });
      queryClient.invalidateQueries({ queryKey: ["traffic-splits"] });
      setShowCreateModal(false);
      alert("Experiment created successfully!");
    },
    onError: (error: any) => {
      console.error("Experiment creation failed:", error);
      alert(error.response?.data?.error || "Failed to create experiment");
    },
  });

  // Calculate stats
  const activeExperiments = experimentsData?.experiments.filter(e => e.status === 'running').length || 0;
  const completedExperiments = experimentsData?.experiments.filter(e => e.status === 'completed').length || 0;
  const totalVariants = experimentsData?.experiments.reduce((sum, e) => sum + e.variants.length, 0) || 0;

  // Format date helper
  const formatDate = (dateString?: string) => {
    if (!dateString) return 'N/A';
    return new Date(dateString).toLocaleDateString();
  };

  // Render experiment list
  const renderExperimentCards = () => {
    if (loadingExperiments) {
      return (
        <div className="text-center py-12">
          <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-4">Loading experiments...</p>
        </div>
      );
    }

    if (errorExperiments) {
      return (
        <Alert className="bg-red-500/10 border-red-500/30 text-red-400">
          <XCircle className="w-4 h-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>
            Failed to load experiments: {(errorExperiments as Error).message}
          </AlertDescription>
        </Alert>
      );
    }

    const experiments = experimentsData?.experiments || [];

    if (experiments.length === 0) {
      return (
        <div className="text-center py-12">
          <Target className="w-16 h-16 mx-auto text-slate-500 mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No Experiments Created Yet</h3>
          <p className="text-slate-400 mb-6">Start testing hypotheses with your first A/B experiment</p>
          <Button
            onClick={() => setShowCreateModal(true)}
            className="bg-blue-600 hover:bg-blue-700 text-white"
          >
            <Plus className="w-4 h-4 mr-2" />
            Create First Experiment
          </Button>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {experiments.map((experiment) => (
          <Card key={experiment.id} className="border-slate-700 bg-slate-800/50 hover:border-blue-500/50 transition-colors">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="text-lg font-bold text-white">{experiment.name}</h3>
                  <div className="flex items-center gap-2 mt-1">
                    <StatusBadge status={experiment.status} />
                    <Badge variant="outline" className="text-slate-300 border-slate-600">
                      {experiment.variants.length} variants
                    </Badge>
                  </div>
                </div>
                {experiment.status === 'running' && (
                  <Zap className="w-5 h-5 text-blue-400 animate-pulse" />
                )}
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Hypothesis Preview */}
              <div className="text-sm text-slate-300 line-clamp-2">
                {experiment.hypothesis}
              </div>

              {/* Primary Metric */}
              <div className="flex items-center justify-between text-sm">
                <span className="text-slate-400">Primary Metric:</span>
                <span className="text-white font-medium">
                  {experiment.primary_metric.replace(/_/g, ' ')}
                </span>
              </div>

              {/* Traffic Distribution */}
              {experiment.variants.some(v => v.traffic_percentage > 0) && (
                <div className="pt-2 border-t border-slate-700">
                  <TrafficPieChart
                    variants={experiment.variants.map(v => ({
                      name: v.name,
                      percentage: v.traffic_percentage,
                      color: v.id === experiment.variants[0].id ? '#10b981' : '#3b82f6'
                    }))}
                  />
                </div>
              )}

              {/* Progress Stats */}
              {experiment.results && (
                <div className="pt-2 border-t border-slate-700 space-y-2">
                  {experiment.results.lift_vs_baseline !== undefined && (
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-slate-400">Lift vs Baseline</span>
                      <span className={`text-sm font-bold ${experiment.results.lift_vs_baseline > 0 ? 'text-green-400' : 'text-red-400'}`}>
                        {experiment.results.lift_vs_baseline > 0 ? '+' : ''}{experiment.results.lift_vs_baseline.toFixed(2)}%
                      </span>
                    </div>
                  )}
                  {experiment.results.statistically_significant !== undefined && (
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-slate-400">Significance</span>
                      <SignificanceBadge
                        significant={experiment.results!.statistically_significant!}
                        p_value={experiment.results!.p_value!}
                      />
                    </div>
                  )}
                </div>
              )}

              {/* Dates */}
              <div className="flex items-center justify-between text-sm text-slate-400 pt-2 border-t border-slate-700">
                <div className="flex items-center gap-1">
                  <Clock className="w-3 h-3" />
                  <span>Started: {formatDate(experiment.start_time)}</span>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 pt-2">
                {experiment.status === 'running' ? (
                  <Button
                    size="sm"
                    variant="outline"
                    className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <Pause className="w-3 h-3" />
                    Pause
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <Play className="w-3 h-3" />
                    Start
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  className="border-slate-600 text-slate-300 hover:bg-slate-700"
                >
                  <BarChart3 className="w-4 h-4" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950">
      {/* Header */}
      <header className="border-b border-slate-800 bg-slate-900/50 backdrop-blur">
        <div className="container mx-auto px-6 py-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold text-white mb-2">M15 A/B Testing Platform</h1>
              <p className="text-slate-400">Statistical experiment design and deployment validation</p>
            </div>
            <Button
              onClick={() => setShowCreateModal(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4"
            >
              <Plus className="w-4 h-4 mr-2" />
              New Experiment
            </Button>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <MetricCard
            icon={Target}
            title="Active Experiments"
            value={activeExperiments}
            trend="+3 this week"
            color="blue"
          />
          <MetricCard
            icon={GitBranch}
            title="Total Variants"
            value={totalVariants}
            color="green"
          />
          <MetricCard
            icon={Activity}
            title="Completed"
            value={completedExperiments}
            color="purple"
          />
          <MetricCard
            icon={Scale}
            title="Avg Duration"
            value="12 days"
            trend="-2 days vs last week"
            color="orange"
          />
        </div>

        {/* Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
          <TabsList className="bg-slate-800 border border-slate-700">
            <TabsTrigger value="experiments" className="data-[state=active]:bg-blue-600">
              <Activity className="w-4 h-4 mr-2" />
              Experiments
            </TabsTrigger>
            <TabsTrigger value="live-results" className="data-[state=active]:bg-blue-600">
              <BarChart3 className="w-4 h-4 mr-2" />
              Live Results
            </TabsTrigger>
            <TabsTrigger value="routing" className="data-[state=active]:bg-blue-600">
              <ArrowUpDown className="w-4 h-4 mr-2" />
              Traffic Routing
            </TabsTrigger>
            <TabsTrigger value="analytics" className="data-[state=active]:bg-blue-600">
              <Database className="w-4 h-4 mr-2" />
              Analytics Insights
            </TabsTrigger>
          </TabsList>

          {/* Experiments Tab */}
          <TabsContent value="experiments">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Experiment List</h3>
                    <p className="text-sm text-slate-400">Manage all A/B test experiments</p>
                  </div>
                  <div className="flex gap-3">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => queryClient.invalidateQueries({ queryKey: ["experiments"] })}
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <RefreshCw className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>{renderExperimentCards()}</CardContent>
            </Card>
          </TabsContent>

          {/* Live Results Placeholder */}
          <TabsContent value="live-results">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Live Experiment Results</h3>
                <p className="text-sm text-slate-400">Real-time metrics and statistical significance tracking</p>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <BarChart3 className="w-4 h-4" />
                  <AlertTitle>Live Metrics Coming Soon</AlertTitle>
                  <AlertDescription>
                    Real-time result dashboards with confidence intervals, p-values, and early stopping recommendations
                    will be available in the next release.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>

          {/* Traffic Routing Placeholder */}
          <TabsContent value="routing">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Traffic Routing Rules</h3>
                <p className="text-sm text-slate-400">Configure and monitor traffic distribution across variants</p>
              </CardHeader>
              <CardContent>
                {loadingSplits ? (
                  <div className="text-center py-8">
                    <Loader2 className="w-8 h-8 animate-spin mx-auto text-blue-500" />
                    <p className="text-slate-400 mt-2">Loading routing rules...</p>
                  </div>
                ) : trafficSplits && trafficSplits.splits.length > 0 ? (
                  <div className="overflow-x-auto">
                    <table className="w-full">
                      <thead className="bg-slate-800/50">
                        <tr>
                          <th className="p-4 text-left text-sm font-semibold text-slate-300">Experiment</th>
                          <th className="p-4 text-left text-sm font-semibold text-slate-300">Variant</th>
                          <th className="p-4 text-left text-sm font-semibold text-slate-300">Traffic %</th>
                          <th className="p-4 text-left text-sm font-semibold text-slate-300">Routed Requests</th>
                          <th className="p-4 text-left text-sm font-semibold text-slate-300">Last Updated</th>
                        </tr>
                      </thead>
                      <tbody>
                        {trafficSplits.splits.map((split, idx) => (
                          <tr key={split.experiment_id} className={`border-b border-slate-700 ${idx % 2 === 0 ? 'bg-slate-800/30' : ''}`}>
                            <td className="p-4 text-white font-medium">{split.experiment_id.split('-')[0]}</td>
                            <td className="p-4 text-slate-300">{split.variant_id}</td>
                            <td className="p-4">
                              <Badge variant="outline" className="text-blue-400 border-blue-500/30">
                                {split.current_percentage}%
                              </Badge>
                            </td>
                            <td className="p-4 text-white">{split.routed_requests.toLocaleString()}</td>
                            <td className="p-4 text-slate-400 text-sm">
                              {new Date(split.last_updated).toLocaleTimeString()}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="text-center text-slate-400 py-8">No traffic split data available</div>
                )}
              </CardContent>
            </Card>
          </TabsContent>

          {/* Analytics Insights Placeholder */}
          <TabsContent value="analytics">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Analytics & Insights</h3>
                <p className="text-sm text-slate-400">Deep dive into experiment performance and learnings</p>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <Database className="w-4 h-4" />
                  <AlertTitle>Advanced Analytics Coming Soon</AlertTitle>
                  <AlertDescription>
                    Retrospective reports, segment-level breakdowns, feature interaction heatmaps,
                    and AI-powered recommendations for next experiments will be available soon.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </main>

      {/* Create Experiment Modal */}
      <CreateExperimentModal
        isOpen={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => {
          setCreateLoading(true);
          createExperimentMutation.mutate(data);
        }}
        isLoading={createLoading}
      />
    </div>
  );
};

export default M15ABTestingPlatform;
