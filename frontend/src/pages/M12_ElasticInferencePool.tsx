/**
 * M12 Elastic Inference Pool Orchestration - Production-Grade Dashboard
 * 
 * Complete user journey: Pool Management → Endpoint Monitoring → Scaling Policies → Performance Analytics
 * Implements real backend API integration with CloudAI Fusion inference pool endpoints
 * Design Philosophy: Linear-style dark theme, bold typography, refined minimalism, GPU-focused aesthetics
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
  Cpu,
  Network,
  Activity,
  Plus,
  RefreshCw,
  TrendingUp,
  Zap,
  Settings,
  BarChart3,
  Server,
  CheckCircle2,
  XCircle,
  Loader2,
  Gauge,
  Clock,
  ArrowUpDown,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface InferencePool {
  id: string;
  name: string;
  model_id: string;
  model_name: string;
  instance_type: string;
  min_replicas: number;
  max_replicas: number;
  current_replicas: number;
  scaling_policy?: ScalingPolicy;
  status: 'healthy' | 'warning' | 'critical';
  endpoint_url?: string;
  gpu_utilization?: number;
  memory_utilization?: number;
}

interface ScalingPolicy {
  metric_type: 'cpu' | 'memory' | 'queue_length' | 'custom';
  target_value: number;
  evaluation_period_sec: number;
  cooldown_period_sec: number;
}

interface Endpoint {
  id: string;
  pool_id: string;
  url: string;
  latency_ms: number;
  requests_per_second: number;
  error_rate: number;
  health_status: 'healthy' | 'degraded' | 'unhealthy';
  last_check: string;
}

interface CreatePoolRequest {
  name: string;
  model_id: string;
  instance_type: string;
  min_replicas: number;
  max_replicas: number;
  scaling_policy?: ScalingPolicy;
}

interface ScaleRequest {
  replicas: number;
}

interface CostMetrics {
  total_gpu_hours: number;
  cost_savings_percentage: number;
  avg_response_time_improvement: number;
  utilization_efficiency: number;
}

// ============================================================================
// Components
// ============================================================================

const StatusBadge = ({ status }: { status: string }) => {
  const statusStyles: Record<string, string> = {
    healthy: "bg-green-500/20 text-green-400 border-green-500/30",
    warning: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
    critical: "bg-red-500/20 text-red-400 border-red-500/30",
    degraded: "bg-orange-500/20 text-orange-400 border-orange-500/30",
    unhealthy: "bg-red-500/20 text-red-400 border-red-500/30",
  };

  const style = statusStyles[status.toLowerCase()] || "bg-gray-500/20 text-gray-400";

  return (
    <Badge className={`${style} border font-semibold`} variant="outline">
      {status.toUpperCase()}
    </Badge>
  );
};

const UtilizationBar = ({ value, color = "blue" }: { value: number; color?: string }) => (
  <div className="w-full bg-slate-700 rounded-full h-2">
    <div
      className={`bg-${color}-500 h-2 rounded-full transition-all duration-300`}
      style={{ width: `${Math.min(value, 100)}%` }}
    />
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

const CreatePoolModal = ({
  isOpen,
  onClose,
  onSubmit,
  isLoading,
}: {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: CreatePoolRequest) => void;
  isLoading: boolean;
}) => {
  if (!isOpen) return null;

  const [formData, setFormData] = useState<CreatePoolRequest>({
    name: "",
    model_id: "",
    instance_type: "a10g.large",
    min_replicas: 1,
    max_replicas: 10,
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(formData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">Create Elastic Inference Pool</h2>
          <p className="text-sm text-slate-400">Configure GPU resource pooling for model inference</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-6 pt-6">
            {/* Pool Name */}
            <div className="space-y-2">
              <Label htmlFor="name" className="text-slate-300">Pool Name</Label>
              <Input
                id="name"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="production-inference-pool"
                className="bg-slate-800 border-slate-600 text-white"
                required
              />
            </div>

            {/* Model Selection */}
            <div className="space-y-2">
              <Label htmlFor="model_id" className="text-slate-300">Associated Model</Label>
              <select
                id="model_id"
                value={formData.model_id}
                onChange={(e) => setFormData({ ...formData, model_id: e.target.value })}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                required
              >
                <option value="">Select a registered model...</option>
                <option value="resnet50-v1">resnet50-v1</option>
                <option value="bert-base-uncased">bert-base-uncased</option>
                <option value="llama-2-7b">llama-2-7b</option>
              </select>
            </div>

            {/* Instance Type */}
            <div className="space-y-2">
              <Label htmlFor="instance_type" className="text-slate-300">GPU Instance Type</Label>
              <select
                id="instance_type"
                value={formData.instance_type}
                onChange={(e) => setFormData({ ...formData, instance_type: e.target.value })}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="a10g.large">AWS Inferentia a10g.large (1 GPU)</option>
                <option value="a10g.xlarge">AWS Inferentia a10g.xlarge (4 GPUs)</option>
                <option value="v100.small">NVIDIA V100 Small (1 GPU)</option>
                <option value="v100.medium">NVIDIA V100 Medium (2 GPUs)</option>
                <option value="a100.huge">NVIDIA A100 Huge (8 GPUs)</option>
              </select>
            </div>

            {/* Replicas Range */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="min_replicas" className="text-slate-300">Min Replicas</Label>
                <Input
                  id="min_replicas"
                  type="number"
                  min={1}
                  max={formData.max_replicas}
                  value={formData.min_replicas}
                  onChange={(e) => setFormData({ ...formData, min_replicas: parseInt(e.target.value) || 1 })}
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="max_replicas" className="text-slate-300">Max Replicas</Label>
                <Input
                  id="max_replicas"
                  type="number"
                  min={formData.min_replicas}
                  value={formData.max_replicas}
                  onChange={(e) => setFormData({ ...formData, max_replicas: parseInt(e.target.value) || 10 })}
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
            </div>

            {/* Auto-scaling Toggle */}
            <div className="flex items-center justify-between p-4 bg-slate-800 rounded-lg border border-slate-700">
              <div>
                <Label className="text-slate-300 font-semibold">Enable Auto-Scaling</Label>
                <p className="text-xs text-slate-400 mt-1">Automatically scale based on traffic metrics</p>
              </div>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  if (!formData.scaling_policy) {
                    setFormData({
                      ...formData,
                      scaling_policy: {
                        metric_type: 'cpu',
                        target_value: 70,
                        evaluation_period_sec: 30,
                        cooldown_period_sec: 60,
                      },
                    });
                  } else {
                    setFormData({ ...formData, scaling_policy: undefined });
                  }
                }}
                className={`border-slate-600 ${formData.scaling_policy ? 'bg-blue-600 text-white' : 'text-slate-300 hover:bg-slate-700'}`}
              >
                {formData.scaling_policy ? 'Enabled' : 'Disabled'}
              </Button>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4">
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
                    Creating Pool...
                  </>
                ) : (
                  <>
                    <Plus className="w-4 h-4 mr-2" />
                    Create Pool
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

const M12ElasticInferencePool = () => {
  const queryClient = useQueryClient();
  
  // State
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [activeTab, setActiveTab] = useState("pools");
  const [createLoading, setCreateLoading] = useState(false);
  const [selectedPool, setSelectedPool] = useState<InferencePool | null>(null);

  // Fetch pools
  const { data: poolsData, isLoading: loadingPools, error: errorPools } = useQuery<{ pools: InferencePool[] }>({
    queryKey: ["inference-pools"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/inference/pools`);
      return response.data;
    },
  });

  // Fetch endpoints
  const { data: endpointsData, isLoading: loadingEndpoints, error: errorEndpoints } = useQuery<{ endpoints: Endpoint[] }>({
    queryKey: ["inference-endpoints"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/inference/endpoints`);
      return response.data;
    },
  });

  // Fetch cost metrics
  const { data: costMetrics, isLoading: loadingCosts } = useQuery<CostMetrics>({
    queryKey: ["cost-metrics"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/inference/cost-metrics`);
      return response.data;
    },
  });

  // Create pool mutation
  const createPoolMutation = useMutation({
    mutationFn: async (data: CreatePoolRequest) => {
      const response = await axios.post(`${API_BASE_URL}/api/v1/inference/pools`, data);
      return response.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["inference-pools"] });
      setShowCreateModal(false);
      alert("Inference pool created successfully!");
    },
    onError: (error: any) => {
      console.error("Pool creation failed:", error);
      alert(error.response?.data?.error || "Failed to create pool");
    },
  });

  // Format bytes helper
  const formatGPUs = (count: number) => `${count} GPU${count !== 1 ? 's' : ''}`;

  // Render pool cards
  const renderPoolCards = () => {
    if (loadingPools) {
      return (
        <div className="text-center py-12">
          <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-4">Loading pools...</p>
        </div>
      );
    }

    if (errorPools) {
      return (
        <Alert className="bg-red-500/10 border-red-500/30 text-red-400">
          <XCircle className="w-4 h-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>
            Failed to load pools: {(errorPools as Error).message}
          </AlertDescription>
        </Alert>
      );
    }

    const pools = poolsData?.pools || [];

    if (pools.length === 0) {
      return (
        <div className="text-center py-12">
          <Server className="w-16 h-16 mx-auto text-slate-500 mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No Inference Pools Created Yet</h3>
          <p className="text-slate-400 mb-6">Create your first elastic inference pool to start serving models efficiently</p>
          <Button
            onClick={() => setShowCreateModal(true)}
            className="bg-blue-600 hover:bg-blue-700 text-white"
          >
            <Plus className="w-4 h-4 mr-2" />
            Create First Pool
          </Button>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {pools.map((pool) => (
          <Card key={pool.id} className="border-slate-700 bg-slate-800/50 hover:border-blue-500/50 transition-colors">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="text-lg font-bold text-white">{pool.name}</h3>
                  <p className="text-sm text-slate-400">{pool.model_name}</p>
                </div>
                <StatusBadge status={pool.status} />
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Resource Info */}
              <div className="space-y-3">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-slate-400">Instance Type</span>
                  <Badge variant="outline" className="text-slate-300 border-slate-600">
                    {pool.instance_type}
                  </Badge>
                </div>
                
                {/* Replicas */}
                <div className="space-y-1">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-slate-400">Replicas</span>
                    <span className="text-white font-medium">
                      {pool.current_replicas} / {pool.max_replicas}
                    </span>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {}}
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <ArrowUpDown className="w-3 h-3" />
                    </Button>
                    <div className="flex-1 bg-slate-700 rounded-full h-2">
                      <div
                        className="bg-blue-500 h-2 rounded-full"
                        style={{ width: `${(pool.current_replicas / pool.max_replicas) * 100}%` }}
                      />
                    </div>
                  </div>
                </div>

                {/* GPU Utilization */}
                {pool.gpu_utilization !== undefined && (
                  <div className="space-y-1">
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-slate-400">GPU Utilization</span>
                      <span className={pool.gpu_utilization > 80 ? "text-red-400" : "text-green-400"}>
                        {pool.gpu_utilization.toFixed(1)}%
                      </span>
                    </div>
                    <UtilizationBar value={pool.gpu_utilization} color={pool.gpu_utilization > 80 ? "red" : "green"} />
                  </div>
                )}

                {/* Memory Utilization */}
                {pool.memory_utilization !== undefined && (
                  <div className="space-y-1">
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-slate-400">Memory Usage</span>
                      <span className="text-slate-300">{pool.memory_utilization.toFixed(1)}%</span>
                    </div>
                    <UtilizationBar value={pool.memory_utilization} color="blue" />
                  </div>
                )}
              </div>

              {/* Scaling Policy Indicator */}
              {pool.scaling_policy && (
                <div className="pt-3 border-t border-slate-700">
                  <div className="flex items-center gap-2 text-xs text-green-400">
                    <Zap className="w-3 h-3" />
                    <span>Auto-scaling enabled</span>
                  </div>
                </div>
              )}

              {/* Actions */}
              <div className="flex gap-2 pt-2">
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                  onClick={() => {
                    setSelectedPool(pool);
                    setActiveTab("endpoints");
                  }}
                >
                  Endpoints
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="border-slate-600 text-slate-300 hover:bg-slate-700"
                >
                  <Settings className="w-4 h-4" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  };

  // Render endpoints table
  const renderEndpointsTable = () => {
    if (loadingEndpoints) {
      return (
        <div className="text-center py-8">
          <Loader2 className="w-8 h-8 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-2">Loading endpoints...</p>
        </div>
      );
    }

    const endpoints = endpointsData?.endpoints || [];

    return (
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead className="bg-slate-800/50">
            <tr>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Endpoint</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Latency</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">RPS</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Error Rate</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Status</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Last Check</th>
              <th className="p-4 text-left text-sm font-semibold text-slate-300">Actions</th>
            </tr>
          </thead>
          <tbody>
            {endpoints.map((ep, idx) => (
              <tr key={ep.id} className={`border-b border-slate-700 ${idx % 2 === 0 ? 'bg-slate-800/30' : ''}`}>
                <td className="p-4">
                  <div className="font-medium text-white">{ep.url.split('/').pop()}</div>
                  <div className="text-xs text-slate-500 font-mono">{ep.url}</div>
                </td>
                <td className="p-4">
                  <div className="flex items-center gap-1">
                    <Gauge className="w-3 h-3 text-slate-400" />
                    <span className="text-white">{ep.latency_ms.toFixed(1)}ms</span>
                  </div>
                </td>
                <td className="p-4">
                  <div className="text-white">{ep.requests_per_second.toFixed(1)}</div>
                </td>
                <td className="p-4">
                  <Badge
                    className={
                      ep.error_rate > 5
                        ? "bg-red-500/20 text-red-400 border-red-500/30"
                        : ep.error_rate > 1
                        ? "bg-yellow-500/20 text-yellow-400 border-yellow-500/30"
                        : "bg-green-500/20 text-green-400 border-green-500/30"
                    }
                  >
                    {ep.error_rate.toFixed(2)}%
                  </Badge>
                </td>
                <td className="p-4">
                  <StatusBadge status={ep.health_status} />
                </td>
                <td className="p-4">
                  <div className="flex items-center gap-1 text-sm text-slate-300">
                    <Clock className="w-3 h-3" />
                    {new Date(ep.last_check).toLocaleTimeString()}
                  </div>
                </td>
                <td className="p-4">
                  <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
                    <RefreshCw className="w-3 h-3" />
                  </Button>
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
              <h1 className="text-3xl font-bold text-white mb-2">M12 Elastic Inference Pool</h1>
              <p className="text-slate-400">Orchestrate GPU resources for efficient model serving</p>
            </div>
            <Button
              onClick={() => setShowCreateModal(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4"
            >
              <Plus className="w-4 h-4 mr-2" />
              Create Pool
            </Button>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <MetricCard
            icon={Server}
            title="Total Pools"
            value={poolsData?.pools.length || 0}
            trend="+3 this week"
            color="blue"
          />
          <MetricCard
            icon={Cpu}
            title="Active GPUs"
            value={formatGPUs(poolsData?.pools.reduce((sum, p) => sum + p.current_replicas, 0) || 0)}
            color="green"
          />
          <MetricCard
            icon={Network}
            title="Endpoints"
            value={endpointsData?.endpoints.length || 0}
            color="purple"
          />
          <MetricCard
            icon={Zap}
            title="Avg Latency"
            value={endpointsData?.endpoints.length ? `${(endpointsData.endpoints.reduce((sum, e) => sum + e.latency_ms, 0) / endpointsData.endpoints.length).toFixed(1)}ms` : '-'}
            trend="-12% vs last week"
            color="orange"
          />
        </div>

        {/* Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
          <TabsList className="bg-slate-800 border border-slate-700">
            <TabsTrigger value="pools" className="data-[state=active]:bg-blue-600">
              <Server className="w-4 h-4 mr-2" />
              Pool Management
            </TabsTrigger>
            <TabsTrigger value="endpoints" className="data-[state=active]:bg-blue-600">
              <Network className="w-4 h-4 mr-2" />
              Endpoint Dashboard
            </TabsTrigger>
            <TabsTrigger value="scaling" className="data-[state=active]:bg-blue-600">
              <Activity className="w-4 h-4 mr-2" />
              Scaling Policies
            </TabsTrigger>
            <TabsTrigger value="analytics" className="data-[state=active]:bg-blue-600">
              <BarChart3 className="w-4 h-4 mr-2" />
              Performance Analytics
            </TabsTrigger>
          </TabsList>

          {/* Pool Management Tab */}
          <TabsContent value="pools">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Inference Pools</h3>
                    <p className="text-sm text-slate-400">Manage your GPU resource pools</p>
                  </div>
                  <div className="flex gap-3">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => queryClient.invalidateQueries({ queryKey: ["inference-pools"] })}
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <RefreshCw className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>{renderPoolCards()}</CardContent>
            </Card>
          </TabsContent>

          {/* Endpoint Dashboard Tab */}
          <TabsContent value="endpoints">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Active Endpoints</h3>
                    <p className="text-sm text-slate-400">Monitor inference endpoint health and performance</p>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <RefreshCw className="w-4 h-4 mr-2" />
                    Refresh Metrics
                  </Button>
                </div>
              </CardHeader>
              <CardContent>{renderEndpointsTable()}</CardContent>
            </Card>
          </TabsContent>

          {/* Scaling Policies & Analytics Placeholders */}
          <TabsContent value="scaling">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Scaling Policies Configuration</h3>
                <p className="text-sm text-slate-400">Configure auto-scaling thresholds and triggers</p>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <Settings className="w-4 h-4" />
                  <AlertTitle>Configuration Coming Soon</AlertTitle>
                  <AlertDescription>
                    Advanced scaling policies will be available in the next release. Current pools use default settings.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="analytics">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Performance & Cost Analytics</h3>
                <p className="text-sm text-slate-400">Track efficiency gains and cost savings</p>
              </CardHeader>
              <CardContent>
                {loadingCosts ? (
                  <div className="text-center py-8">
                    <Loader2 className="w-8 h-8 animate-spin mx-auto text-blue-500" />
                    <p className="text-slate-400 mt-2">Loading analytics...</p>
                  </div>
                ) : costMetrics ? (
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
                    <div className="text-center">
                      <div className="text-3xl font-bold text-white">{costMetrics.total_gpu_hours.toFixed(1)}h</div>
                      <p className="text-sm text-slate-400 mt-1">Total GPU Hours</p>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-green-400">{costMetrics.cost_savings_percentage.toFixed(1)}%</div>
                      <p className="text-sm text-slate-400 mt-1">Cost Savings</p>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-blue-400">{costMetrics.avg_response_time_improvement.toFixed(1)}%</div>
                      <p className="text-sm text-slate-400 mt-1">Response Time Improvement</p>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-purple-400">{costMetrics.utilization_efficiency.toFixed(1)}%</div>
                      <p className="text-sm text-slate-400 mt-1">Utilization Efficiency</p>
                    </div>
                  </div>
                ) : (
                  <div className="text-center text-slate-400 py-8">No analytics data available</div>
                )}
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </main>

      {/* Create Pool Modal */}
      <CreatePoolModal
        isOpen={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => {
          setCreateLoading(true);
          createPoolMutation.mutate(data);
        }}
        isLoading={createLoading}
      />
    </div>
  );
};

export default M12ElasticInferencePool;
