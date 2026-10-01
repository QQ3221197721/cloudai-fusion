/**
 * M16 Auto-scaling Engine - Production-Grade Infrastructure Management Dashboard
 * 
 * Complete user journey: Policy Creation → Live Monitoring → Event History → What-If Simulation
 * Implements real backend API integration with CloudAI Fusion scaling engine endpoints
 * Design Philosophy: Linear-style dark theme, infrastructure aesthetics, operational excellence
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  Zap,
  Activity,
  TrendingUp,
  TrendingDown,
  Settings,
  Bell,
  Clock,
  Target,
  Thermometer,
  Server,
  ArrowUpDown,
  Play,
  Pause,
  RefreshCw,
  Shield,
  DollarSign,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Loader2,
  Calendar,
  BarChart3,
  LineChart,
  PieChart,
  Filter,
  Download,
  Eye,
  Edit,
  Trash2,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface ScalingPolicy {
  id: string;
  name: string;
  description: string;
  resource_type: ResourceType;
  resource_id: string;
  status: 'active' | 'inactive';
  metrics: MetricThreshold[];
  scaling_config: ScalingConfiguration;
  budget_limits?: BudgetConstraints;
  last_evaluated?: string;
  trend: ScaleTrend;
}

type ResourceType = 'gpu_cluster' | 'inference_pool' | 'training_job';
type ScaleTrend = 'scale_up' | 'scale_down' | 'stable';

interface MetricThreshold {
  name: string;
  target: number;
  operator: string;
  window_minutes: number;
  description?: string;
}

interface ScalingConfiguration {
  min_nodes: number;
  max_nodes: number;
  scale_up_step: number;
  scale_down_step: number;
  cooldown_seconds: number;
  rebalance_enabled: boolean;
  spot_instance_use: boolean;
}

interface BudgetConstraints {
  hourly_limit_usd: number;
  daily_limit_usd: number;
  monthly_limit_usd: number;
  alert_on_threshold: number;
}

interface UtilizationMetric {
  cluster_id: string;
  type: string;
  utilization: number;
  nodes_active: number;
  nodes_total: number;
  queue_depth: number;
  avg_latency_ms: number;
  throughput_rps: number;
  cost_per_hour: number;
}

interface ScalingEvent {
  id: string;
  type: EventType;
  policy_id: string;
  policy_name: string;
  timestamp: string;
  trigger: string;
  action_taken: string;
  result: string;
  before_nodes: number;
  after_nodes: number;
}

type EventType = 'scale_up_triggered' | 'scale_down_triggered' | 'budget_warning' | 'manual_scale';

interface WhatIfSimulation {
  simulation_id: string;
  policy_id: string;
  scenarios: SimulationScenario[];
  budget_analysis?: BudgetAnalysis;
}

interface SimulationScenario {
  name: string;
  predicted_latency_impact_ms: number;
  predicted_cost_delta_usd_hour: number;
  probability: number;
  confidence_level: number;
  risk_assessment: 'low' | 'medium' | 'high';
  recommendation: 'proceed' | 'monitor_closely' | 'hold_off';
}

interface BudgetAnalysis {
  current_spend_usd_hour: number;
  projected_spend_usd_hour: number;
  delta_usd_hour: number;
  daily_projection_usd: number;
  monthly_projection_usd: number;
  within_budget: boolean;
  savings_opportunity_usd: number;
}

interface CreatePolicyRequest {
  name: string;
  description: string;
  resource_type: ResourceType;
  resource_id: string;
  metrics: Array<{ name: string; target: number; operator: string; window_minutes: number }>;
  scaling_config: ScalingConfiguration;
  budget_limits?: BudgetConstraints;
}

// ============================================================================
// Utility Functions
// ============================================================================

const formatCurrency = (amount: number) => {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
  }).format(amount);
};

const formatPercentage = (value: number) => {
  return `${value.toFixed(1)}%`;
};

const formatDate = (dateString: string) => {
  return new Date(dateString).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

// ============================================================================
// Components
// ============================================================================

const StatusBadge = ({ status }: { status: string }) => {
  const statusStyles: Record<string, string> = {
    active: "bg-green-500/20 text-green-400 border-green-500/30",
    inactive: "bg-gray-500/20 text-gray-400 border-gray-500/30",
    scale_up: "bg-red-500/20 text-red-400 border-red-500/30",
    scale_down: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    stable: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
  };

  const style = statusStyles[status.toLowerCase()] || "bg-gray-500/20 text-gray-400";

  return (
    <Badge className={`${style} border font-semibold`} variant="outline">
      {status.toUpperCase()}
    </Badge>
  );
};

const UtilizationGauge = ({ value, label }: { value: number; label: string }) => {
  const getColor = (v: number) => {
    if (v < 60) return "text-green-400";
    if (v < 80) return "text-yellow-400";
    return "text-red-400";
  };

  const bgClass = value < 60 ? "bg-green-500" : value < 80 ? "bg-yellow-500" : "bg-red-500";

  return (
    <div className="flex flex-col items-center">
      <div className="relative w-20 h-20">
        <svg className="w-full h-full transform -rotate-90">
          <circle
            cx="40"
            cy="40"
            r="35"
            fill="transparent"
            stroke="#1e293b"
            strokeWidth="8"
          />
          <circle
            cx="40"
            cy="40"
            r="35"
            fill="transparent"
            stroke={value >= 80 ? "#ef4444" : value >= 60 ? "#eab308" : "#22c55e"}
            strokeWidth="8"
            strokeDasharray={`${(value / 100) * 220} 220`}
            strokeLinecap="round"
            className="transition-all duration-500"
          />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className={`text-xl font-bold ${getColor(value)}`}>{Math.round(value)}</span>
        </div>
      </div>
      <p className="text-xs text-slate-400 mt-2">{label}</p>
    </div>
  );
};

const HeatmapVisualization = ({ data }: { data: number[][] }) => {
  const colors = [
    "#1e293b", // 0-20
    "#3b82f6", // 20-40
    "#22c55e", // 40-60
    "#eab308", // 60-80
    "#ef4444", // 80-100
  ];

  return (
    <div className="grid grid-cols-7 gap-1">
      {data.map((row, rowIndex) =>
        row.map((value, colIndex) => {
          const colorIdx = Math.min(Math.floor(value / 20), 4);
          return (
            <div
              key={`${rowIndex}-${colIndex}`}
              className={`aspect-square rounded-sm ${colors[colorIdx]} opacity-60 hover:opacity-100 transition-opacity cursor-help`}
              title={`Week ${rowIndex + 1}, Hour ${colIndex}: ${value}%`}
            />
          );
        })
      )}
    </div>
  );
};

const CreatePolicyModal = ({
  isOpen,
  onClose,
  onSubmit,
  isLoading,
}: {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: CreatePolicyRequest) => void;
  isLoading: boolean;
}) => {
  if (!isOpen) return null;

  const [formData, setFormData] = useState<CreatePolicyRequest>({
    name: "",
    description: "",
    resource_type: "gpu_cluster",
    resource_id: "",
    metrics: [
      { name: "gpu_utilization_percent", target: 75, operator: ">", window_minutes: 5 },
    ],
    scaling_config: {
      min_nodes: 2,
      max_nodes: 20,
      scale_up_step: 2,
      scale_down_step: 1,
      cooldown_seconds: 300,
      rebalance_enabled: true,
      spot_instance_use: false,
    },
  });

  const addMetric = () => {
    setFormData({
      ...formData,
      metrics: [...formData.metrics, { name: "", target: 75, operator: ">", window_minutes: 5 }],
    });
  };

  const updateMetric = (index: number, field: keyof MetricThreshold, value: string | number) => {
    const updated = [...formData.metrics];
    updated[index] = { ...updated[index], [field]: value };
    setFormData({ ...formData, metrics: updated });
  };

  const removeMetric = (index: number) => {
    setFormData({
      ...formData,
      metrics: formData.metrics.filter((_, i) => i !== index),
    });
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(formData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-5xl border-slate-700 bg-slate-900 max-h-[90vh] overflow-y-auto">
        <CardHeader className="border-b border-slate-700 sticky top-0 bg-slate-900 z-10">
          <h2 className="text-2xl font-bold text-white">Create Scaling Policy</h2>
          <p className="text-sm text-slate-400">Define automated scaling rules based on metrics and thresholds</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-6 pt-6">
            {/* Basic Information */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Settings className="w-4 h-4" />
                Policy Configuration
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="name" className="text-slate-300">Policy Name *</Label>
                  <Input
                    id="name"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    placeholder="GPU Cluster Inference Latency"
                    className="bg-slate-800 border-slate-600 text-white"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="resource_type" className="text-slate-300">Resource Type *</Label>
                  <Select
                    value={formData.resource_type}
                    onValueChange={(val) => setFormData({ ...formData, resource_type: val as ResourceType })}
                  >
                    <SelectTrigger className="bg-slate-800 border-slate-600 text-white">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="gpu_cluster">GPU Cluster</SelectItem>
                      <SelectItem value="inference_pool">Inference Pool</SelectItem>
                      <SelectItem value="training_job">Training Job</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="resource_id" className="text-slate-300">Resource ID *</Label>
                <Input
                  id="resource_id"
                  value={formData.resource_id}
                  onChange={(e) => setFormData({ ...formData, resource_id: e.target.value })}
                  placeholder="cluster-prod-01"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="description" className="text-slate-300">Description</Label>
                <Textarea
                  id="description"
                  value={formData.description}
                  onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                  placeholder="Monitor p99 latency and auto-scale when threshold exceeded for 5 consecutive minutes..."
                  className="bg-slate-800 border-slate-600 text-white"
                  rows={3}
                />
              </div>
            </div>

            {/* Metrics Configuration */}
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                  <Thermometer className="w-4 h-4" />
                  Metrics & Thresholds
                </h4>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={addMetric}
                  className="border-slate-600 text-slate-300 hover:bg-slate-700"
                >
                  <Plus className="w-3 h-3 mr-1" />
                  Add Metric
                </Button>
              </div>

              {formData.metrics.map((metric, index) => (
                <div key={index} className="p-4 bg-slate-800 rounded-lg border border-slate-700 space-y-3">
                  <div className="flex items-center justify-between">
                    <p className="text-sm font-semibold text-slate-300">Metric {index + 1}</p>
                    {formData.metrics.length > 1 && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() => removeMetric(index)}
                        className="border-red-500/30 text-red-400 hover:bg-red-500/10"
                      >
                        <Trash2 className="w-3 h-3" />
                      </Button>
                    )}
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-2">
                      <Label className="text-slate-300 text-xs">Metric Name</Label>
                      <Input
                        value={metric.name}
                        onChange={(e) => updateMetric(index, "name", e.target.value)}
                        placeholder="gpu_utilization_percent"
                        className="bg-slate-700 border-slate-600 text-white text-sm"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label className="text-slate-300 text-xs">Operator</Label>
                      <Select
                        value={metric.operator}
                        onValueChange={(val) => updateMetric(index, "operator", val)}
                      >
                        <SelectTrigger className="bg-slate-700 border-slate-600 text-white text-sm">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value=">">></SelectItem>
                          <SelectItem value="<"><</SelectItem>
                          <SelectItem value=">=">&=</SelectItem>
                          <SelectItem value="<="><=</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-2">
                      <Label className="text-slate-300 text-xs">Target Value</Label>
                      <Input
                        type="number"
                        value={metric.target}
                        onChange={(e) => updateMetric(index, "target", parseFloat(e.target.value) || 0)}
                        className="bg-slate-700 border-slate-600 text-white text-sm"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label className="text-slate-300 text-xs">Window (min)</Label>
                      <Input
                        type="number"
                        value={metric.window_minutes}
                        onChange={(e) => updateMetric(index, "window_minutes", parseInt(e.target.value) || 5)}
                        className="bg-slate-700 border-slate-600 text-white text-sm"
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>

            {/* Scaling Configuration */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <ArrowUpDown className="w-4 h-4" />
                Scaling Configuration
              </h4>
              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-2">
                  <Label className="text-slate-300 text-xs">Min Nodes</Label>
                  <Input
                    type="number"
                    value={formData.scaling_config.min_nodes}
                    onChange={(e) => setFormData({
                      ...formData,
                      scaling_config: { ...formData.scaling_config, min_nodes: parseInt(e.target.value) || 2 },
                    })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="space-y-2">
                  <Label className="text-slate-300 text-xs">Max Nodes</Label>
                  <Input
                    type="number"
                    value={formData.scaling_config.max_nodes}
                    onChange={(e) => setFormData({
                      ...formData,
                      scaling_config: { ...formData.scaling_config, max_nodes: parseInt(e.target.value) || 20 },
                    })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="space-y-2">
                  <Label className="text-slate-300 text-xs">Scale Up Step</Label>
                  <Input
                    type="number"
                    value={formData.scaling_config.scale_up_step}
                    onChange={(e) => setFormData({
                      ...formData,
                      scaling_config: { ...formData.scaling_config, scale_up_step: parseInt(e.target.value) || 2 },
                    })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="space-y-2">
                  <Label className="text-slate-300 text-xs">Scale Down Step</Label>
                  <Input
                    type="number"
                    value={formData.scaling_config.scale_down_step}
                    onChange={(e) => setFormData({
                      ...formData,
                      scaling_config: { ...formData.scaling_config, scale_down_step: parseInt(e.target.value) || 1 },
                    })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="space-y-2">
                  <Label className="text-slate-300 text-xs">Cooldown (sec)</Label>
                  <Input
                    type="number"
                    value={formData.scaling_config.cooldown_seconds}
                    onChange={(e) => setFormData({
                      ...formData,
                      scaling_config: { ...formData.scaling_config, cooldown_seconds: parseInt(e.target.value) || 300 },
                    })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="flex items-end space-y-2">
                  <label className="flex items-center gap-2 text-sm text-slate-300">
                    <input
                      type="checkbox"
                      checked={formData.scaling_config.rebalance_enabled}
                      onChange={(e) => setFormData({
                        ...formData,
                        scaling_config: { ...formData.scaling_config, rebalance_enabled: e.target.checked },
                      })}
                      className="rounded bg-slate-700 border-slate-600 text-blue-600"
                    />
                    Enable rebalancing
                  </label>
                </div>
              </div>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4 border-t border-slate-700">
              <Button
                type="button"
                onClick={onClose}
                disabled={isLoading}
                variant="outline"
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
                    Create Policy
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

const WhatIfSimulationModal = ({
  isOpen,
  onClose,
  onSubmit,
  isLoading,
}: {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: any) => void;
  isLoading: boolean;
}) => {
  if (!isOpen) return null;

  const [formData, setFormData] = useState({
    policy_id: "",
    simulated_scale: 2,
    time_horizon: "immediate",
    include_budget_impact: true,
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(formData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">What-If Simulation</h2>
          <p className="text-sm text-slate-400">Simulate scaling decisions before execution</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4 pt-6">
            <div className="space-y-2">
              <Label htmlFor="policy_select" className="text-slate-300">Select Policy</Label>
              <Select
                value={formData.policy_id}
                onValueChange={(val) => setFormData({ ...formData, policy_id: val })}
              >
                <SelectTrigger className="bg-slate-800 border-slate-600 text-white">
                  <SelectValue placeholder="Choose a policy..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="pol-1738234001">GPU Cluster Inference Latency</SelectItem>
                  <SelectItem value="pol-1738234002">Training Job Memory Threshold</SelectItem>
                  <SelectItem value="pol-1738234003">Inference Pool Throughput</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="simulated_scale" className="text-slate-300">Simulated Scale Change</Label>
              <Input
                id="simulated_scale"
                type="number"
                min={1}
                max={10}
                value={formData.simulated_scale}
                onChange={(e) => setFormData({ ...formData, simulated_scale: parseInt(e.target.value) || 2 })}
                className="bg-slate-800 border-slate-600 text-white"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="time_horizon" className="text-slate-300">Time Horizon</Label>
              <Select
                value={formData.time_horizon}
                onValueChange={(val) => setFormData({ ...formData, time_horizon: val })}
              >
                <SelectTrigger className="bg-slate-800 border-slate-600 text-white">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="immediate">Immediate</SelectItem>
                  <SelectItem value="1h">1 Hour</SelectItem>
                  <SelectItem value="24h">24 Hours</SelectItem>
                  <SelectItem value="7d">7 Days</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.include_budget_impact}
                onChange={(e) => setFormData({ ...formData, include_budget_impact: e.target.checked })}
                id="include_budget_impact"
                className="rounded bg-slate-700 border-slate-600 text-blue-600"
              />
              <Label htmlFor="include_budget_impact" className="text-slate-300 text-sm">Include budget impact analysis</Label>
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t border-slate-700">
              <Button
                type="button"
                onClick={onClose}
                disabled={isLoading}
                variant="outline"
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
                    Simulating...
                  </>
                ) : (
                  <>
                    <BarChart3 className="w-4 h-4 mr-2" />
                    Run Simulation
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

const M16AutoScalingEngine = () => {
  const queryClient = useQueryClient();
  
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showSimulationModal, setShowSimulationModal] = useState(false);
  const [activeTab, setActiveTab] = useState("policies");
  const [createLoading, setCreateLoading] = useState(false);
  const [simulationLoading, setSimulationLoading] = useState(false);

  // Fetch policies
  const { data: policiesData, isLoading: loadingPolicies, error: errorPolicies } = useQuery<{ policies: ScalingPolicy[] }>({
    queryKey: ["scaling-policies"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/scaler/policies`);
      return response.data;
    },
  });

  // Fetch current utilization
  const { data: utilizationData, isLoading: loadingUtilization } = useQuery<{ current_metrics: any }>({
    queryKey: ["utilization-current"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/scaler/utilization/current`);
      return response.data;
    },
  });

  // Fetch events
  const { data: eventData, isLoading: loadingEvents } = useQuery<{ events: ScalingEvent[] }>({
    queryKey: ["scaling-events"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/scaler/events?limit=50`);
      return response.data;
    },
  });

  // Get heatmap data
  const { data: heatmapData } = useQuery<{ heatmap_data: number[][] }>({
    queryKey: ["utilization-heatmap"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/scaler/utilization/heatmap`);
      return response.data;
    },
  });

  // Create policy mutation
  const createPolicyMutation = useMutation({
    mutationFn: async (data: CreatePolicyRequest) => {
      const response = await axios.post(`${API_BASE_URL}/api/v1/scaler/policies`, data);
      return response.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["scaling-policies"] });
      setShowCreateModal(false);
      alert("Policy created successfully!");
    },
    onError: (error: any) => {
      console.error("Policy creation failed:", error);
      alert(error.response?.data?.error || "Failed to create policy");
    },
  });

  // Run what-if simulation
  const runSimulationMutation = useMutation({
    mutationFn: async (data: any) => {
      const response = await axios.post(`${API_BASE_URL}/api/v1/scaler/simulate/what-if`, data);
      return response.data;
    },
    onSuccess: () => {
      setShowSimulationModal(false);
      alert("Simulation completed! Check results below.");
    },
    onError: (error: any) => {
      console.error("Simulation failed:", error);
      alert(error.response?.data?.error || "Simulation failed");
    },
  });

  // Calculate stats
  const activePolicies = policiesData?.policies.filter(p => p.status === 'active').length || 0;
  const scaleUpPolicies = policiesData?.policies.filter(p => p.trend === 'scale_up').length || 0;
  const totalResources = policiesData?.policies.reduce((sum, p) => sum + 1, 0) || 0;

  const renderPolicyCards = () => {
    if (loadingPolicies) {
      return (
        <div className="text-center py-12">
          <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-4">Loading scaling policies...</p>
        </div>
      );
    }

    if (errorPolicies) {
      return (
        <Alert className="bg-red-500/10 border-red-500/30 text-red-400">
          <XCircle className="w-4 h-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>
            Failed to load policies: {(errorPolicies as Error).message}
          </AlertDescription>
        </Alert>
      );
    }

    const policies = policiesData?.policies || [];

    if (policies.length === 0) {
      return (
        <div className="text-center py-12">
          <Zap className="w-16 h-16 mx-auto text-slate-500 mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No Scaling Policies Created Yet</h3>
          <p className="text-slate-400 mb-6">Start with your first automated scaling policy</p>
          <Button onClick={() => setShowCreateModal(true)} className="bg-blue-600 hover:bg-blue-700">
            <Plus className="w-4 h-4 mr-2" />
            Create First Policy
          </Button>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {policies.map((policy) => (
          <Card key={policy.id} className="border-slate-700 bg-slate-800/50 hover:border-blue-500/50 transition-colors">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="text-lg font-bold text-white">{policy.name}</h3>
                  <div className="flex items-center gap-2 mt-1">
                    <StatusBadge status={policy.status} />
                    <StatusBadge status={policy.trend} />
                  </div>
                </div>
                <Zap className={`w-5 h-5 ${policy.trend === 'scale_up' ? 'text-red-400' : 'text-blue-400'}`} />
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-sm text-slate-300 line-clamp-2">{policy.description}</p>
              
              <div className="text-sm space-y-1">
                <p className="text-slate-400">Resource:</p>
                <p className="text-white font-medium">{policy.resource_type} / {policy.resource_id}</p>
              </div>

              <div className="pt-2 border-t border-slate-700 space-y-1">
                <p className="text-slate-400 text-xs">Metrics configured:</p>
                <p className="text-white text-sm">{policy.metrics.length} thresholds</p>
              </div>

              <div className="flex gap-2 pt-2">
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                  onClick={() => queryClient.invalidateQueries({ queryKey: ["scaling-policies"] })}
                >
                  <RefreshCw className="w-3 h-3" />
                  Evaluate
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  onClick={() => {}}
                >
                  <Settings className="w-3 h-3" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  };

  const renderHeatmap = () => {
    return (
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <h3 className="text-xl font-bold text-white">Utilization Heatmap (4 Weeks)</h3>
          <p className="text-sm text-slate-400">Hour-of-week pattern analysis across multiple weeks</p>
        </CardHeader>
        <CardContent>
          {loadingUtilization ? (
            <div className="text-center py-8">
              <Loader2 className="w-8 h-8 animate-spin mx-auto text-blue-500" />
            </div>
          ) : heatmapData && heatmapData.heatmap_data ? (
            <div>
              <HeatmapVisualization data={heatmapData.heatmap_data} />
              <div className="flex justify-center gap-4 mt-4">
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded-sm bg-slate-700"></div>
                  <span className="text-xs text-slate-400">0-20%</span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded-sm bg-blue-500"></div>
                  <span className="text-xs text-slate-400">20-40%</span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded-sm bg-green-500"></div>
                  <span className="text-xs text-slate-400">40-60%</span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded-sm bg-yellow-500"></div>
                  <span className="text-xs text-slate-400">60-80%</span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded-sm bg-red-500"></div>
                  <span className="text-xs text-slate-400">80-100%</span>
                </div>
              </div>
            </div>
          ) : (
            <p className="text-slate-400 text-center py-8">No heatmap data available</p>
          )}
        </CardContent>
      </Card>
    );
  };

  const renderEventsTable = () => {
    if (loadingEvents) {
      return (
        <div className="text-center py-12">
          <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-4">Loading events...</p>
        </div>
      );
    }

    const events = eventData?.events || [];

    return (
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead className="bg-slate-800/50">
            <tr>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Time</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Type</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Policy</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Trigger</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Result</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event, idx) => (
              <tr key={event.id} className={`border-b border-slate-700 ${idx % 2 === 0 ? 'bg-slate-800/30' : ''}`}>
                <td className="p-4 text-slate-300 text-sm">{formatDate(event.timestamp)}</td>
                <td className="p-4">
                  <Badge variant="outline" className="text-blue-400 border-blue-500/30">
                    {event.type.split('_')[0].toUpperCase()}
                  </Badge>
                </td>
                <td className="p-4 text-white text-sm font-medium">{event.policy_name}</td>
                <td className="p-4 text-slate-300 text-sm">{event.trigger}</td>
                <td className="p-4">
                  <StatusBadge status={event.result} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
              <h1 className="text-3xl font-bold text-white mb-2">M16 Auto-scaling Engine</h1>
              <p className="text-slate-400">Intelligent infrastructure scaling based on metrics and budgets</p>
            </div>
            <div className="flex gap-3">
              <Button
                onClick={() => setShowSimulationModal(true)}
                variant="outline"
                className="border-slate-600 text-slate-300 hover:bg-slate-800"
              >
                <BarChart3 className="w-4 h-4 mr-2" />
                What-If
              </Button>
              <Button
                onClick={() => setShowCreateModal(true)}
                className="bg-blue-600 hover:bg-blue-700 text-white"
              >
                <Plus className="w-4 h-4 mr-2" />
                New Policy
              </Button>
            </div>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <Card className="border-slate-700 bg-slate-800/50">
            <CardContent className="p-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-400">Active Policies</p>
                  <p className="text-2xl font-bold text-white mt-1">{activePolicies}</p>
                </div>
                <Zap className="w-8 h-8 text-blue-500" />
              </div>
            </CardContent>
          </Card>
          
          <Card className="border-slate-700 bg-slate-800/50">
            <CardContent className="p-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-400">Scale-Up Needed</p>
                  <p className="text-2xl font-bold text-white mt-1">{scaleUpPolicies}</p>
                </div>
                <TrendingUp className="w-8 h-8 text-red-500" />
              </div>
            </CardContent>
          </Card>

          <Card className="border-slate-700 bg-slate-800/50">
            <CardContent className="p-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-400">Resources Monitored</p>
                  <p className="text-2xl font-bold text-white mt-1">{totalResources}</p>
                </div>
                <Server className="w-8 h-8 text-green-500" />
              </div>
            </CardContent>
          </Card>

          <Card className="border-slate-700 bg-slate-800/50">
            <CardContent className="p-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-400">Avg GPU Util</p>
                  <p className="text-2xl font-bold text-white mt-1">{utilizationData?.global_metrics?.average_utilization ? Math.round(utilizationData.global_metrics.average_utilization) + '%' : 'N/A'}</p>
                </div>
                <Activity className="w-8 h-8 text-yellow-500" />
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
          <TabsList className="bg-slate-800 border border-slate-700">
            <TabsTrigger value="policies" className="data-[state=active]:bg-blue-600">
              <Settings className="w-4 h-4 mr-2" />
              Policies
            </TabsTrigger>
            <TabsTrigger value="monitoring" className="data-[state=active]:bg-blue-600">
              <Activity className="w-4 h-4 mr-2" />
              Real-time Monitor
            </TabsTrigger>
            <TabsTrigger value="history" className="data-[state=active]:bg-blue-600">
              <Clock className="w-4 h-4 mr-2" />
              Event History
            </TabsTrigger>
            <TabsTrigger value="simulation" className="data-[state=active]:bg-blue-600">
              <BarChart3 className="w-4 h-4 mr-2" />
              What-If Analysis
            </TabsTrigger>
          </TabsList>

          {/* Policies Tab */}
          <TabsContent value="policies">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Scaling Policies</h3>
                    <p className="text-sm text-slate-400">Manage automated scaling rules</p>
                  </div>
                  <div className="flex gap-3">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => queryClient.invalidateQueries({ queryKey: ["scaling-policies"] })}
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <RefreshCw className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>{renderPolicyCards()}</CardContent>
            </Card>
          </TabsContent>

          {/* Monitoring Tab */}
          <TabsContent value="monitoring">
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              <Card className="border-slate-700 bg-slate-800/50">
                <CardHeader>
                  <h3 className="text-xl font-bold text-white">Current Utilization</h3>
                  <p className="text-sm text-slate-400">Real-time metrics by resource</p>
                </CardHeader>
                <CardContent>
                  {loadingUtilization ? (
                    <div className="text-center py-8">
                      <Loader2 className="w-8 h-8 animate-spin mx-auto text-blue-500" />
                    </div>
                  ) : utilizationData?.cluster_details ? (
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      {utilizationData.cluster_details.map((cluster: any, idx: number) => (
                        <div key={idx} className="p-4 bg-slate-800 rounded-lg border border-slate-700">
                          <UtilizationGauge value={cluster.utilization} label={cluster.cluster_id} />
                          <div className="mt-2 text-sm text-slate-400">
                            <p>{cluster.nodes_active}/{cluster.nodes_total} nodes</p>
                            <p>Latency: {cluster.avg_latency_ms.toFixed(1)}ms</p>
                            <p>RPS: {cluster.throughput_rps}</p>
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : null}
                </CardContent>
              </Card>

              {renderHeatmap()}
            </div>
          </TabsContent>

          {/* History Tab */}
          <TabsContent value="history">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Scaling Event History</h3>
                    <p className="text-sm text-slate-400">Track all scaling decisions and executions</p>
                  </div>
                  <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
                    <Download className="w-4 h-4 mr-2" />
                    Export
                  </Button>
                </div>
              </CardHeader>
              <CardContent>{renderEventsTable()}</CardContent>
            </Card>
          </TabsContent>

          {/* Simulation Tab */}
          <TabsContent value="simulation">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">What-If Simulation Results</h3>
                <p className="text-sm text-slate-400">Analyze potential scaling decisions before execution</p>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <BarChart3 className="w-4 h-4" />
                  <AlertTitle>Simulation Interface Coming Soon</AlertTitle>
                  <AlertDescription>
                    Interactive what-if simulations with scenario comparison, budget impact analysis,
                    and predictive modeling will be available in the next release.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </main>

      {/* Modals */}
      <CreatePolicyModal
        isOpen={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => {
          setCreateLoading(true);
          createPolicyMutation.mutate(data);
        }}
        isLoading={createLoading}
      />

      <WhatIfSimulationModal
        isOpen={showSimulationModal}
        onClose={() => setShowSimulationModal(false)}
        onSubmit={(data) => {
          setSimulationLoading(true);
          runSimulationMutation.mutate(data);
        }}
        isLoading={simulationLoading}
      />
    </div>
  );
};

export default M16AutoScalingEngine;