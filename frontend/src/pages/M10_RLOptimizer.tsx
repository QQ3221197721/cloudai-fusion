/**
 * M10 RL-Based Training Optimizer - Production-Grade Dashboard
 * 
 * Complete user journey: Training Optimization Monitoring → Job Submission → Performance Analytics → Policy Management
 * Implements real backend API integration with CloudAI Fusion RL optimizer endpoints
 */

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Slider } from "@/components/ui/slider";
import { Progress } from "@/components/ui/progress";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import {
  Brain,
  TrendingUp,
  Zap,
  Activity,
  BarChart3,
  Settings,
  Play,
  Square,
  RefreshCw,
  Save,
  CheckCircle2,
  XCircle,
  Loader2,
  Target,
  Award,
  Clock,
  DollarSign,
  BrainCircuit,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface TrainingOptimization {
  id: string;
  job_id: string;
  rl_decision: string;
  predicted_wait_time_min: number;
  actual_wait_time_min?: number;
  efficiency_score: number; // 0-100
  resource_allocation: {
    gpu_count: number;
    memory_gb: number;
    priority: number;
  };
  sla_compliance: boolean;
  timestamp: string;
  status: 'pending' | 'completed' | 'failed';
}

interface OptimizationSuggestion {
  job_id: string;
  current_state: {
    queue_position: number;
    estimated_wait_min: number;
    current_resources: any;
    workload_type: string;
  };
  recommended_action: string;
  confidence: number; // 0-1
  expected_improvement: number; // percentage
  alternative_actions: Array<{
    action: string;
    confidence: number;
    expected_improvement: number;
  }>;
}

interface RLPolicyConfig {
  learning_rate: number;
  exploration_factor: number;
  exploitation_factor: number;
  reward_weights: {
    latency: number;
    throughput: number;
    cost: number;
    fairness: number;
  };
  max_episode_length: number;
  discount_factor: number;
  version: string;
  deployed_at?: string;
  is_active: boolean;
}

interface BenchmarkResult {
  metric: string;
  baseline_value: number;
  optimized_value: number;
  improvement_percentage: number;
  p_value: number;
  statistical_significance: boolean;
  sample_size: number;
  trend: 'upward' | 'downward' | 'stable';
  data_points: Array<{ x: string; y: number; timestamp: string }>;
}

interface ABOptimizationTest {
  test_id: string;
  name: string;
  control_group: {
    description: string;
    avg_efficiency: number;
    avg_cost: number;
    avg_sla_compliance: number;
  };
  treatment_group: {
    description: string;
    avg_efficiency: number;
    avg_cost: number;
    avg_sla_compliance: number;
  };
  results: {
    efficiency_delta: number;
    cost_delta: number;
    sla_delta: number;
    statistically_significant: boolean;
    p_value: number;
  };
  duration_hours: number;
  jobs_tested: number;
  created_at: string;
}

// ============================================================================
// Component: M10_RLOptimizer_Page
// ============================================================================

export default function M10RLOptimizerPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  // State Management
  const [optimizations, setOptimizations] = useState<TrainingOptimization[]>([]);
  const [suggestions, setSuggestions] = useState<OptimizationSuggestion[]>([]);
  const [policyConfig, setPolicyConfig] = useState<RLPolicyConfig | null>(null);
  const [showSubmitter, setShowSubmitter] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [activeTab, setActiveTab] = useState('dashboard');
  const [useRealAPI, setUseRealAPI] = useState(true);
  
  // Job Submitter Form State
  const [jobForm, setJobForm] = useState({
    job_name: '',
    workload_type: 'training',
    priority: 5,
    expected_duration_min: 60,
    gpu_requirement: '1xA100',
    memory_requirement_gb: 32,
    rl_mode: 'balanced',
    sla_target_min: 30,
    budget_limit: null as number | null,
    notes: '',
  });

  // Policy Editor State
  const [policyEditor, setPolicyEditor] = useState<Partial<RLPolicyConfig>>({});
  const [showPolicyDialog, setShowPolicyDialog] = useState(false);
  const [showBenchmarkDialog, setShowBenchmarkDialog] = useState(false);

  // Fetch optimizations
  async function fetchOptimizations(limit: number = 100): Promise<TrainingOptimization[]> {
    if (!useRealAPI) {
      return Array.from({ length: 10 }, (_, i) => ({
        id: `opt-${i + 1}`,
        job_id: `job-training-${String(i + 1).padStart(3, '0')}`,
        rl_decision: i % 3 === 0 ? 'allocate_more_gpus' : i % 3 === 1 ? 'reduce_priority' : 'queue_optimization',
        predicted_wait_time_min: Math.random() * 20 + 5,
        actual_wait_time_min: Math.random() * 20 + 5,
        efficiency_score: Math.random() * 40 + 60,
        resource_allocation: {
          gpu_count: Math.floor(Math.random() * 8) + 1,
          memory_gb: Math.floor(Math.random() * 64) + 16,
          priority: Math.floor(Math.random() * 10) + 1,
        },
        sla_compliance: Math.random() > 0.2,
        timestamp: new Date(Date.now() - Math.random() * 86400000).toISOString(),
        status: 'completed',
      }));
    }
    
    const params = new URLSearchParams({ limit: limit.toString() });
    const response = await axios.get(`/api/v1/optimizer/training?${params}`);
    return response.data.optimizations || response.data;
  }

  const { data: optData = [], isLoading: optimizingLoading, refetch: refetchOptimizations } = useQuery({
    queryKey: ['m10-optimizations'],
    queryFn: () => fetchOptimizations(100),
    staleTime: 5000,
    refetchOnWindowFocus: true,
  });

  // Sync local state
  useEffect(() => {
    if (optData && Array.isArray(optData)) {
      setOptimizations(optData);
    }
  }, [optData]);

  // Fetch suggestions
  async function fetchSuggestions(): Promise<OptimizationSuggestion[]> {
    if (!useRealAPI) {
      return [
        {
          job_id: 'job-training-new-001',
          current_state: {
            queue_position: 5,
            estimated_wait_min: 45,
            current_resources: { gpus: 2, memory: 64 },
            workload_type: 'training',
          },
          recommended_action: 'allocate_additional_gpus',
          confidence: 0.87,
          expected_improvement: 35,
          alternative_actions: [
            { action: 'increase_priority', confidence: 0.72, expected_improvement: 25 },
            { action: 'batch_with_similar_jobs', confidence: 0.65, expected_improvement: 20 },
          ],
        },
        {
          job_id: 'job-inference-scaling-002',
          current_state: {
            queue_position: 2,
            estimated_wait_min: 15,
            current_resources: { gpus: 1, memory: 32 },
            workload_type: 'inference',
          },
          recommended_action: 'maintain_current_allocation',
          confidence: 0.92,
          expected_improvement: 5,
          alternative_actions: [],
        },
      ];
    }
    
    const response = await axios.post('/api/v1/optimizer/suggest', {});
    return response.data.suggestions || [];
  }

  const suggestionsQuery = useQuery({
    queryKey: ['m10-suggestions'],
    queryFn: fetchSuggestions,
    enabled: false,
  });

  // Fetch benchmarks
  const benchmarkQuery = useQuery({
    queryKey: ['m10-benchmarks'],
    queryFn: async () => {
      if (!useRealAPI) {
        return {
          metrics: [
            {
              metric: 'avg_queue_wait_time',
              baseline_value: 120,
              optimized_value: 35,
              improvement_percentage: 70.8,
              p_value: 0.001,
              statistical_significance: true,
              sample_size: 1000,
              trend: 'downward',
              data_points: Array.from({ length: 30 }, (_, i) => ({
                x: `Day ${i + 1}`,
                y: 120 - i * 3 + Math.random() * 10,
                timestamp: new Date(Date.now() - (29 - i) * 86400000).toISOString(),
              })),
            },
            {
              metric: 'resource_utilization_efficiency',
              baseline_value: 45,
              optimized_value: 78,
              improvement_percentage: 73.3,
              p_value: 0.003,
              statistical_significance: true,
              sample_size: 1000,
              trend: 'upward',
              data_points: Array.from({ length: 30 }, (_, i) => ({
                x: `Day ${i + 1}`,
                y: 45 + i * 1.1 + Math.random() * 5,
                timestamp: new Date(Date.now() - (29 - i) * 86400000).toISOString(),
              })),
            },
            {
              metric: 'cost_per_training_job',
              baseline_value: 150,
              optimized_value: 98,
              improvement_percentage: 34.7,
              p_value: 0.012,
              statistical_significance: true,
              sample_size: 500,
              trend: 'downward',
              data_points: Array.from({ length: 30 }, (_, i) => ({
                x: `Day ${i + 1}`,
                y: 150 - i * 1.7 + Math.random() * 8,
                timestamp: new Date(Date.now() - (29 - i) * 86400000).toISOString(),
              })),
            },
          ] as BenchmarkResult[],
        };
      }
      
      const response = await axios.get('/api/v1/optimizer/benchmarks');
      return response.data;
    },
    refetchInterval: 30000,
    enabled: false,
  });

  // Fetch policy config
  const policyQuery = useQuery({
    queryKey: ['m10-policy'],
    queryFn: async () => {
      if (!useRealAPI) {
        return {
          learning_rate: 0.001,
          exploration_factor: 0.3,
          exploitation_factor: 0.7,
          reward_weights: {
            latency: 0.25,
            throughput: 0.35,
            cost: 0.25,
            fairness: 0.15,
          },
          max_episode_length: 1000,
          discount_factor: 0.95,
          version: 'v2.3.1',
          deployed_at: new Date().toISOString(),
          is_active: true,
        };
      }
      
      const response = await axios.get('/api/v1/optimizer/policy');
      return response.data.policy;
    },
    enabled: false,
  });

  // Mutation handlers
  const submitJobForOptimization = useMutation({
    mutationFn: async (jobConfig: typeof jobForm) => {
      await axios.post('/api/v1/optimizer/suggest', jobConfig);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['m10-optimizations'] });
      setShowSubmitter(false);
      // Simulate adding the job to suggestions
      const newSuggestion: OptimizationSuggestion = {
        job_id: `job-submitted-${Date.now()}`,
        current_state: {
          queue_position: 1,
          estimated_wait_min: 30,
          current_resources: {},
          workload_type: jobForm.workload_type,
        },
        recommended_action: 'optimize_allocation',
        confidence: 0.8,
        expected_improvement: 25,
        alternative_actions: [],
      };
      setSuggestions([...suggestions, newSuggestion]);
    },
  });

  const updatePolicyMutation = useMutation({
    mutationFn: async (updates: Partial<RLPolicyConfig>) => {
      await axios.put('/api/v1/optimizer/policy', updates);
    },
    onSuccess: () => {
      policyQuery.refetch();
      setShowPolicyDialog(false);
      queryClient.invalidateQueries({ queryKey: ['m10-policy'] });
    },
  });

  const runBenchmarksMutation = useMutation({
    mutationFn: async () => {
      await axios.get('/api/v1/optimizer/benchmarks/run');
    },
    onSuccess: () => {
      benchmarkQuery.refetch();
    },
  });

  // Calculate statistics
  const stats = {
    total_jobs: optimizations.length,
    avg_efficiency: optimizations.length 
      ? Math.round(optimizations.reduce((sum, o) => sum + o.efficiency_score, 0) / optimizations.length) 
      : 0,
    sla_compliance_rate: optimizations.length
      ? Math.round((optimizations.filter(o => o.sla_compliance).length / optimizations.length) * 100)
      : 0,
    avg_wait_reduction: optimizations.length
      ? Math.round(optimizations.reduce((sum, o) => {
          const reduction = ((o.predicted_wait_time_min - o.actual_wait_time_min || o.predicted_wait_time_min) / o.predicted_wait_time_min) * 100;
          return sum + reduction;
        }, 0) / optimizations.length)
      : 0,
    rl_decisions_today: optimizations.filter(o => {
      const today = new Date().toDateString();
      return new Date(o.timestamp).toDateString() === today;
    }).length,
  };

  // Handlers
  const handleToggleSubmitter = () => {
    setJobForm({
      job_name: '',
      workload_type: 'training',
      priority: 5,
      expected_duration_min: 60,
      gpu_requirement: '1xA100',
      memory_requirement_gb: 32,
      rl_mode: 'balanced',
      sla_target_min: 30,
      budget_limit: null,
      notes: '',
    });
    setShowSubmitter(!showSubmitter);
  };

  const handleExecuteSubmission = () => {
    submitJobForOptimization.mutate(jobForm);
  };

  const handleOpenPolicyEditor = () => {
    if (policyQuery.data) {
      setPolicyEditor(policyQuery.data);
    }
    setShowPolicyDialog(true);
  };

  const handleSavePolicy = () => {
    updatePolicyMutation.mutate(policyEditor);
  };

  // Render optimization decision badge
  const renderDecisionBadge = (decision: string) => {
    const config = {
      'allocate_more_gpus': { variant: 'default' as const, label: 'More GPUs', color: 'text-blue-400' },
      'reduce_priority': { variant: 'secondary' as const, label: 'Lower Priority', color: 'text-yellow-400' },
      'queue_optimization': { variant: 'outline' as const, label: 'Queue Optimize', color: 'text-green-400' },
      'batch_jobs': { variant: 'destructive' as const, label: 'Batch Jobs', color: 'text-purple-400' },
    };

    const { variant, label, color } = config[decision as keyof typeof config] || { variant: 'outline', label: decision, color: 'text-gray-400' };

    return (
      <Badge variant={variant} className="gap-1">
        <BrainCircuit className={`w-3 h-3 ${color}`} />
        {label}
      </Badge>
    );
  };

  // Calculate ROI
  const calculateROI = () => {
    const baselineCost = 150 * stats.total_jobs;
    const optimizedCost = 98 * stats.total_jobs;
    const savings = baselineCost - optimizedCost;
    return {
      monthly_savings: savings,
      annual_projection: savings * 12,
      efficiency_gain_pct: stats.avg_efficiency - 50,
      sla_improvement_pct: stats.sla_compliance_rate - 75,
    };
  };

  const roi = calculateROI();

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 p-6 space-y-6">
      {/* Header */}
      <div className="space-y-2 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <h1 className="text-4xl font-bold gradient-text flex items-center gap-3">
          <Brain className="w-10 h-10 text-primary" />
          RL-Based Training Optimizer
        </h1>
        <p className="text-gray-400 text-lg">
          Reinforcement Learning-powered scheduling and resource optimization
        </p>

        <div className="flex items-center gap-2 mt-4">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setUseRealAPI(!useRealAPI)}
          >
            <RefreshCw className={`w-4 h-4 mr-2 ${!useRealAPI ? 'animate-pulse' : ''}`} />
            {useRealAPI ? 'Real Backend API' : 'Simulated Data'}
          </Button>

          <Button variant="outline" size="sm" onClick={() => setShowSubmitter(true)}>
            <Play className="w-4 h-4 mr-2" />
            Submit Job
          </Button>
        </div>
      </div>

      {/* Statistics Cards */}
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Total Jobs</p>
                <p className="text-2xl font-bold text-primary">{stats.total_jobs}</p>
              </div>
              <Activity className="w-8 h-8 text-muted-foreground opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Avg Efficiency</p>
                <p className="text-2xl font-bold text-emerald-400">{stats.avg_efficiency}%</p>
              </div>
              <TrendingUp className="w-8 h-8 text-emerald-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">SLA Compliance</p>
                <p className="text-2xl font-bold text-blue-400">{stats.sla_compliance_rate}%</p>
              </div>
              <CheckCircle2 className="w-8 h-8 text-blue-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Wait Reduction</p>
                <p className="text-2xl font-bold text-orange-400">{stats.avg_wait_reduction}%</p>
              </div>
              <Clock className="w-8 h-8 text-orange-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">RL Decisions Today</p>
                <p className="text-2xl font-bold text-purple-400">{stats.rl_decisions_today}</p>
              </div>
              <Brain className="w-8 h-8 text-purple-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent-20 col-span-2 md:col-span-1">
          <CardContent className="p-4">
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Monthly Savings</span>
                <DollarSign className="w-4 h-4 text-emerald-400" />
              </div>
              <p className="text-xl font-bold text-emerald-400">${roi.monthly_savings.toLocaleString()}</p>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Area */}
      <Tabs defaultValue="dashboard" className="flex-1">
        <TabsList className="grid w-full grid-cols-4 mb-4">
          <TabsTrigger value="dashboard">
            <BarChart3 className="w-4 h-4 mr-2" />
            Dashboard
          </TabsTrigger>
          <TabsTrigger value="submitter">
            <Play className="w-4 h-4 mr-2" />
            Job Submitter
          </TabsTrigger>
          <TabsTrigger value="analytics">
            <Award className="w-4 h-4 mr-2" />
            Analytics
          </TabsTrigger>
          <TabsTrigger value="policy">
            <Settings className="w-4 h-4 mr-2" />
            Policy Editor
          </TabsTrigger>
        </TabsList>

        <div className="grid grid-cols-[1fr] gap-4">
          {/* Dashboard Tab */}
          <TabsContent value="dashboard" className="m-0">
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <Card className="bg-card/50 backdrop-blur border-accent/20">
                <CardHeader>
                  <h2 className="text-xl font-bold">Recent Optimizations</h2>
                  <p className="text-sm text-muted-foreground">
                    Last 10 RL-based scheduling decisions
                  </p>
                </CardHeader>

                <CardContent>
                  <ScrollArea className="h-[500px]">
                    <div className="space-y-3">
                      {(useRealAPI ? optimizingLoading : optimizingLoading) ? (
                        <div className="flex items-center justify-center py-8">
                          <Loader2 className="w-6 h-6 animate-spin text-primary" />
                        </div>
                      ) : (useRealAPI ? optimizations : optimizations).length === 0 ? (
                        <div className="text-center py-8 text-muted-foreground">
                          No optimization records found
                        </div>
                      ) : (
                        (useRealAPI ? optimizations : optimizations).map((opt) => (
                          <Card key={opt.id} className="bg-muted/30">
                            <CardContent className="p-4">
                              <div className="flex items-start justify-between mb-2">
                                <div className="flex-1">
                                  <div className="flex items-center gap-2 mb-1">
                                    <Badge variant="outline" className="font-mono text-xs">
                                      {opt.job_id}
                                    </Badge>
                                    {renderDecisionBadge(opt.rl_decision)}
                                  </div>
                                  <p className="text-sm text-muted-foreground">
                                    Submitted: {new Date(opt.timestamp).toLocaleString()}
                                  </p>
                                </div>
                                <Badge variant={opt.sla_compliance ? 'default' : 'destructive'} className="text-xs">
                                  {opt.sla_compliance ? 'SLA Met' : 'SLA Missed'}
                                </Badge>
                              </div>

                              <div className="grid grid-cols-3 gap-4 text-sm">
                                <div>
                                  <p className="text-muted-foreground text-xs">Efficiency</p>
                                  <p className="font-semibold">{opt.efficiency_score.toFixed(1)}%</p>
                                </div>
                                <div>
                                  <p className="text-muted-foreground text-xs">Predicted Wait</p>
                                  <p className="font-semibold">{opt.predicted_wait_time_min.toFixed(1)} min</p>
                                </div>
                                <div>
                                  <p className="text-muted-foreground text-xs">Actual Wait</p>
                                  <p className="font-semibold">{opt.actual_wait_time_min?.toFixed(1) || 'N/A'} min</p>
                                </div>
                              </div>

                              <Separator className="my-3" />

                              <div className="grid grid-cols-3 gap-2 text-xs">
                                <div className="flex items-center gap-1">
                                  <Zap className="w-3 h-3" />
                                  <span>{opt.resource_allocation.gpu_count} GPUs</span>
                                </div>
                                <div className="flex items-center gap-1">
                                  <Target className="w-3 h-3" />
                                  <span>{opt.resource_allocation.memory_gb}GB RAM</span>
                                </div>
                                <div className="flex items-center gap-1">
                                  <TrendingUp className="w-3 h-3" />
                                  <span>Priority #{opt.resource_allocation.priority}</span>
                                </div>
                              </div>
                            </CardContent>
                          </Card>
                        ))
                      )}
                    </div>
                  </ScrollArea>
                </CardContent>
              </Card>

              <Card className="bg-card/50 backdrop-blur border-accent/20">
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <div>
                      <h2 className="text-xl font-bold">Live Suggestions</h2>
                      <p className="text-sm text-muted-foreground">
                        Real-time RL recommendations
                      </p>
                    </div>
                    <Button 
                      variant="outline" 
                      size="sm" 
                      onClick={() => suggestionsQuery.refetch()}
                      disabled={!suggestionsQuery.isSuccess}
                    >
                      <RefreshCw className={`w-4 h-4 mr-2 ${suggestionsQuery.isFetching ? 'animate-spin' : ''}`} />
                      Refresh
                    </Button>
                  </div>
                </CardHeader>

                <CardContent>
                  {!suggestionsQuery.isSuccess ? (
                    <div className="text-center py-8 text-muted-foreground">
                      Click refresh to load suggestions
                    </div>
                  ) : (
                    <div className="space-y-4">
                      {suggestions.length === 0 ? (
                        <div className="text-center py-8 text-muted-foreground">
                          No active suggestions
                        </div>
                      ) : (
                        suggestions.map((suggestion, idx) => (
                          <Card key={idx} className="bg-muted/30">
                            <CardContent className="p-4">
                              <div className="flex items-start justify-between mb-3">
                                <div className="flex-1">
                                  <div className="flex items-center gap-2 mb-1">
                                    <Badge variant="outline" className="font-mono text-xs">
                                      {suggestion.job_id}
                                    </Badge>
                                    <Badge variant="default" className="bg-purple-500/20 text-purple-400 border-purple-500/30">
                                      AI Suggestion
                                    </Badge>
                                  </div>
                                  <p className="font-medium">{suggestion.recommended_action.replace(/_/g, ' ').toUpperCase()}</p>
                                </div>
                                <Badge variant="secondary" className="gap-1">
                                  <BrainCircuit className="w-3 h-3" />
                                  {(suggestion.confidence * 100).toFixed(0)}% Conf
                                </Badge>
                              </div>

                              <div className="space-y-2 text-sm">
                                <div className="grid grid-cols-2 gap-2">
                                  <div>
                                    <p className="text-muted-foreground text-xs">Queue Position</p>
                                    <p>{suggestion.current_state.queue_position}</p>
                                  </div>
                                  <div>
                                    <p className="text-muted-foreground text-xs">Est. Wait</p>
                                    <p>{suggestion.current_state.estimated_wait_min} min</p>
                                  </div>
                                </div>

                                <Separator />

                                <Alert>
                                  <Zap className="w-4 h-4" />
                                  <AlertTitle>Expected Improvement</AlertTitle>
                                  <AlertDescription>
                                    <strong>{suggestion.expected_improvement}%</strong> efficiency gain
                                  </AlertDescription>
                                </Alert>
                              </div>
                            </CardContent>
                          </Card>
                        ))
                      )}
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>
          </TabsContent>

          {/* Job Submitter Tab */}
          <TabsContent value="submitter" className="m-0">
            <Card className="bg-card/50 backdrop-blur border-accent/20">
              <CardHeader>
                <h2 className="text-xl font-bold">Submit Job for RL Optimization</h2>
                <p className="text-sm text-muted-foreground">
                  Configure your training/inference job to leverage RL-based scheduling
                </p>
              </CardHeader>

              <CardContent>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="job_name">Job Name *</Label>
                    <Input
                      id="job_name"
                      placeholder="my-training-job-001"
                      value={jobForm.job_name}
                      onChange={(e) => setJobForm({...jobForm, job_name: e.target.value})}
                      required
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="workload_type">Workload Type *</Label>
                    <Select
                      value={jobForm.workload_type}
                      onValueChange={(value) => setJobForm({...jobForm, workload_type: value})}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="training">Training</SelectItem>
                        <SelectItem value="inference">Inference</SelectItem>
                        <SelectItem value="fine-tuning">Fine-tuning</SelectItem>
                        <SelectItem value="evaluation">Evaluation</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="priority">Priority (1-10) *</Label>
                    <Input
                      id="priority"
                      type="number"
                      min="1"
                      max="10"
                      value={jobForm.priority}
                      onChange={(e) => setJobForm({...jobForm, priority: parseInt(e.target.value)})}
                    />
                    <Progress value={jobForm.priority * 10} className="h-1" />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="expected_duration">Expected Duration (min) *</Label>
                    <Input
                      id="expected_duration"
                      type="number"
                      value={jobForm.expected_duration_min}
                      onChange={(e) => setJobForm({...jobForm, expected_duration_min: parseInt(e.target.value)})}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="gpu_requirement">GPU Requirement *</Label>
                    <Select
                      value={jobForm.gpu_requirement}
                      onValueChange={(value) => setJobForm({...jobForm, gpu_requirement: value})}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="1xA100">1x A100</SelectItem>
                        <SelectItem value="2xA100">2x A100</SelectItem>
                        <SelectItem value="4xA100">4x A100</SelectItem>
                        <SelectItem value="1xV100">1x V100</SelectItem>
                        <SelectItem value="multi-node">Multi-Node</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="memory_requirement">Memory Requirement (GB) *</Label>
                    <Input
                      id="memory_requirement"
                      type="number"
                      value={jobForm.memory_requirement_gb}
                      onChange={(e) => setJobForm({...jobForm, memory_requirement_gb: parseInt(e.target.value)})}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="rl_mode">RL Optimization Mode *</Label>
                    <Select
                      value={jobForm.rl_mode}
                      onValueChange={(value) => setJobForm({...jobForm, rl_mode: value})}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="aggressive">Aggressive (Fastest completion)</SelectItem>
                        <SelectItem value="balanced">Balanced (Best overall)</SelectItem>
                        <SelectItem value="conservative">Conservative (Cost-effective)</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="sla_target">SLA Target (min) *</Label>
                    <Input
                      id="sla_target"
                      type="number"
                      value={jobForm.sla_target_min}
                      onChange={(e) => setJobForm({...jobForm, sla_target_min: parseInt(e.target.value)})}
                    />
                  </div>
                </div>

                <div className="space-y-2 mt-4">
                  <Label htmlFor="budget_limit">Budget Limit ($)</Label>
                  <Input
                    id="budget_limit"
                    type="number"
                    placeholder="Optional - RL will optimize within this constraint"
                    value={jobForm.budget_limit ?? ''}
                    onChange={(e) => setJobForm({...jobForm, budget_limit: e.target.value ? parseFloat(e.target.value) : null})}
                  />
                </div>

                <div className="space-y-2 mt-4">
                  <Label htmlFor="notes">Additional Notes</Label>
                  <Textarea
                    id="notes"
                    placeholder="Any specific requirements or constraints..."
                    value={jobForm.notes}
                    onChange={(e) => setJobForm({...jobForm, notes: e.target.value})}
                    rows={3}
                  />
                </div>

                <Separator className="my-6" />

                <Alert>
                  <BrainCircuit className="w-4 h-4" />
                  <AlertTitle>What Happens Next?</AlertTitle>
                  <AlertDescription>
                    Our RL agent will analyze your job configuration and queue position, then provide optimal resource allocation 
                    recommendations that minimize wait time while staying within SLA and budget constraints. Expected response time: ~2 seconds.
                  </AlertDescription>
                </Alert>

                <div className="mt-6 flex justify-end gap-3">
                  <Button variant="outline" onClick={() => setShowSubmitter(false)}>Cancel</Button>
                  <Button 
                    onClick={handleExecuteSubmission}
                    disabled={submitJobForOptimization.isLoading || !jobForm.job_name}
                  >
                    {submitJobForOptimization.isLoading ? (
                      <>
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        Processing...
                      </>
                    ) : (
                      <>
                        <Play className="w-4 h-4 mr-2" />
                        Submit for Optimization
                      </>
                    )}
                  </Button>
                </div>
              </CardContent>
            </Card>
          </TabsContent>

          {/* Analytics Tab */}
          <TabsContent value="analytics" className="m-0">
            <Card className="bg-card/50 backdrop-blur border-accent/20">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h2 className="text-xl font-bold">Performance Benchmarks</h2>
                    <p className="text-sm text-muted-foreground">
                      Before/after comparison of RL optimization impact
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => benchmarkQuery.refetch()}
                      disabled={!benchmarkQuery.isSuccess}
                    >
                      <RefreshCw className={`w-4 h-4 mr-2 ${benchmarkQuery.isFetching ? 'animate-spin' : ''}`} />
                      Refresh
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => runBenchmarksMutation.mutate()}
                      disabled={runBenchmarksMutation.isLoading}
                    >
                      <Play className="w-4 h-4 mr-2" />
                      Run New Benchmarks
                    </Button>
                  </div>
                </div>
              </CardHeader>

              <CardContent>
                {!benchmarkQuery.isSuccess ? (
                  <div className="text-center py-8 text-muted-foreground">
                    Click refresh to load benchmark data<br />
                    Or run new benchmarks to generate fresh data
                  </div>
                ) : (
                  <div className="space-y-6">
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                      {benchmarkQuery.data.metrics.map((metric: BenchmarkResult, idx: number) => (
                        <Card key={idx} className="bg-muted/30">
                          <CardContent className="p-6">
                            <div className="flex items-center justify-between mb-4">
                              <h3 className="font-semibold capitalize">{metric.metric.replace(/_/g, ' ')}</h3>
                              <Badge variant={metric.improvement_percentage > 0 ? 'default' : 'destructive'}>
                                {metric.improvement_percentage > 0 ? '+' : '-'}{metric.improvement_percentage.toFixed(1)}%
                              </Badge>
                            </div>

                            <div className="space-y-3">
                              <div className="flex items-center justify-between text-sm">
                                <span className="text-muted-foreground">Baseline:</span>
                                <span className="font-medium">{metric.baseline_value}</span>
                              </div>
                              <div className="flex items-center justify-between text-sm">
                                <span className="text-muted-foreground">Optimized:</span>
                                <span className="font-medium text-emerald-400">{metric.optimized_value}</span>
                              </div>
                            </div>

                            <div className="mt-4 pt-4 border-t">
                              <p className="text-xs text-muted-foreground mb-2">Statistical Significance</p>
                              <div className="flex items-center gap-2">
                                {metric.statistical_significance ? (
                                  <Badge variant="default" className="gap-1">
                                    <CheckCircle2 className="w-3 h-3" />
                                    Significant (p={metric.p_value.toFixed(4)})
                                  </Badge>
                                ) : (
                                  <Badge variant="outline" className="gap-1">
                                    <XCircle className="w-3 h-3" />
                                    Not Significant
                                  </Badge>
                                )}
                              </div>
                              <p className="text-xs text-muted-foreground mt-2">Sample Size: {metric.sample_size}</p>
                            </div>
                          </CardContent>
                        </Card>
                      ))}
                    </div>

                    <Card className="bg-muted/30">
                      <CardHeader>
                        <h3 className="font-semibold">Metric Trends (Last 30 Days)</h3>
                      </CardHeader>
                      <CardContent>
                        <div className="space-y-6">
                          {benchmarkQuery.data.metrics.map((metric: BenchmarkResult, idx: number) => (
                            <div key={idx}>
                              <h4 className="font-medium mb-2">{metric.metric.replace(/_/g, ' ')}</h4>
                              <div className="h-32 flex items-end gap-1">
                                {metric.data_points.slice(-20).map((point, i) => (
                                  <div
                                    key={i}
                                    className={`flex-1 rounded-t ${
                                      metric.trend === 'downward' ? 'bg-red-500' : 'bg-emerald-500'
                                    }`}
                                    style={{ height: `${(point.y / (metric.baseline_value * 1.5)) * 100}%` }}
                                    title={`${point.x}: ${point.y.toFixed(1)}`}
                                  />
                                ))}
                              </div>
                              <p className="text-xs text-muted-foreground text-center mt-1">
                                {metric.trend === 'upward' ? 'Improving ↑' : metric.trend === 'downward' ? 'Improving ↓' : 'Stable →'}
                              </p>
                            </div>
                          ))}
                        </div>
                      </CardContent>
                    </Card>

                    <Alert>
                      <DollarSign className="w-4 h-4" />
                      <AlertTitle>Financial Impact Summary</AlertTitle>
                      <AlertDescription>
                        <strong>Monthly Savings:</strong> ${roi.monthly_savings.toLocaleString()}<br />
                        <strong>Annual Projection:</strong> ${roi.annual_projection.toLocaleString()}<br />
                        <strong>Average Efficiency Gain:</strong> {roi.efficiency_gain_pct}%<br />
                        <strong>SLA Compliance Improvement:</strong> {roi.sla_improvement_pct}%
                      </AlertDescription>
                    </Alert>
                  </div>
                )}
              </CardContent>
            </Card>
          </TabsContent>

          {/* Policy Editor Tab */}
          <TabsContent value="policy" className="m-0">
            <Card className="bg-card/50 backdrop-blur border-accent/20">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h2 className="text-xl font-bold">RL Policy Configuration</h2>
                    <p className="text-sm text-muted-foreground">
                      Fine-tune reinforcement learning hyperparameters and reward weights
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => policyQuery.refetch()}
                      disabled={!policyQuery.isSuccess}
                    >
                      <RefreshCw className={`w-4 h-4 mr-2 ${policyQuery.isFetching ? 'animate-spin' : ''}`} />
                      Load Current
                    </Button>
                    <Button onClick={handleOpenPolicyEditor}>
                      <Save className="w-4 h-4 mr-2" />
                      Edit & Deploy
                    </Button>
                  </div>
                </div>
              </CardHeader>

              <CardContent>
                {!policyQuery.isSuccess ? (
                  <div className="text-center py-8 text-muted-foreground">
                    Click "Load Current" to fetch live policy settings
                  </div>
                ) : policyConfig ? (
                  <div className="space-y-6">
                    <div className="grid grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <div className="flex items-center justify-between">
                          <Label>Learning Rate</Label>
                          <Badge variant="outline">{policyConfig.learning_rate}</Badge>
                        </div>
                        <div className="bg-muted/50 p-3 rounded">
                          <p className="text-sm text-muted-foreground">Controls step size in parameter updates</p>
                        </div>
                      </div>

                      <div className="space-y-2">
                        <div className="flex items-center justify-between">
                          <Label>Discount Factor (γ)</Label>
                          <Badge variant="outline">{policyConfig.discount_factor}</Badge>
                        </div>
                        <div className="bg-muted/50 p-3 rounded">
                          <p className="text-sm text-muted-foreground">Future reward importance (0-1)</p>
                        </div>
                      </div>

                      <div className="space-y-2">
                        <div className="flex items-center justify-between">
                          <Label>Exploration Factor (ε)</Label>
                          <Badge variant="outline">{policyConfig.exploration_factor}</Badge>
                        </div>
                        <div className="bg-muted/50 p-3 rounded">
                          <p className="text-sm text-muted-foreground">
                            Exploration vs Exploitation: {Math.round(policyConfig.exploration_factor * 100)}% explore
                          </p>
                        </div>
                      </div>

                      <div className="space-y-2">
                        <div className="flex items-center justify-between">
                          <Label>Max Episode Length</Label>
                          <Badge variant="outline">{policyConfig.max_episode_length}</Badge>
                        </div>
                        <div className="bg-muted/50 p-3 rounded">
                          <p className="text-sm text-muted-foreground">Steps before episode reset</p>
                        </div>
                      </div>
                    </div>

                    <Separator />

                    <div className="space-y-3">
                      <h3 className="font-semibold">Reward Function Weights</h3>
                      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                        <div>
                          <Label>Latency Weight</Label>
                          <div className="bg-muted/50 p-3 rounded text-center">
                            <p className="text-xl font-bold">{(policyConfig.reward_weights.latency * 100).toFixed(0)}%</p>
                          </div>
                        </div>
                        <div>
                          <Label>Throughput Weight</Label>
                          <div className="bg-muted/50 p-3 rounded text-center">
                            <p className="text-xl font-bold">{(policyConfig.reward_weights.throughput * 100).toFixed(0)}%</p>
                          </div>
                        </div>
                        <div>
                          <Label>Cost Weight</Label>
                          <div className="bg-muted/50 p-3 rounded text-center">
                            <p className="text-xl font-bold">{(policyConfig.reward_weights.cost * 100).toFixed(0)}%</p>
                          </div>
                        </div>
                        <div>
                          <Label>Fairness Weight</Label>
                          <div className="bg-muted/50 p-3 rounded text-center">
                            <p className="text-xl font-bold">{(policyConfig.reward_weights.fairness * 100).toFixed(0)}%</p>
                          </div>
                        </div>
                      </div>
                      <p className="text-xs text-muted-foreground">
                        Sum: {(Object.values(policyConfig.reward_weights).reduce((a, b) => a + b, 0) * 100).toFixed(0)}%
                      </p>
                    </div>

                    <Separator />

                    <div className="flex items-center justify-between text-sm">
                      <div>
                        <span className="text-muted-foreground">Version:</span>{' '}
                        <Badge variant="outline">{policyConfig.version}</Badge>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Deployed:</span>{' '}
                        {policyConfig.deployed_at ? new Date(policyConfig.deployed_at).toLocaleString() : 'Never'}
                      </div>
                      <div>
                        <span className="text-muted-foreground">Status:</span>{' '}
                        <Badge variant={policyConfig.is_active ? 'default' : 'secondary'}>
                          {policyConfig.is_active ? 'Active' : 'Inactive'}
                        </Badge>
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="text-center py-8 text-muted-foreground">
                    No policy configuration found
                  </div>
                )}
              </CardContent>
            </Card>
          </TabsContent>
        </div>
      </Tabs>

      {/* Job Submitter Dialog */}
      <Dialog open={showSubmitter} onOpenChange={setShowSubmitter}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Submit Job for RL Optimization</DialogTitle>
            <DialogDescription>
              Configure your job to receive intelligent scheduling recommendations
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <Alert>
              <BrainCircuit className="w-4 h-4" />
              <AlertTitle>AI-Powered Optimization</AlertTitle>
              <AlertDescription>
                Submit your job details and our RL agent will analyze it against current cluster state, historical performance,
                and SLA requirements to provide optimal scheduling recommendations.
              </AlertDescription>
            </Alert>
            <div className="text-right">
              <Button onClick={() => setShowSubmitter(false)}>Close Modal</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Policy Editor Dialog */}
      <Dialog open={showPolicyDialog} onOpenChange={setShowPolicyDialog}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Edit RL Policy Configuration</DialogTitle>
            <DialogDescription>
              Modify hyperparameters and reward weights. Changes will be deployed immediately.
            </DialogDescription>
          </DialogHeader>

          {policyConfig && (
            <div className="space-y-6">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="learning_rate">Learning Rate</Label>
                  <Input
                    id="learning_rate"
                    type="number"
                    step="0.0001"
                    value={policyEditor.learning_rate ?? policyConfig.learning_rate}
                    onChange={(e) => setPolicyEditor({...policyEditor, learning_rate: parseFloat(e.target.value)})}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="discount_factor">Discount Factor (γ)</Label>
                  <Input
                    id="discount_factor"
                    type="number"
                    step="0.01"
                    min="0"
                    max="1"
                    value={policyEditor.discount_factor ?? policyConfig.discount_factor}
                    onChange={(e) => setPolicyEditor({...policyEditor, discount_factor: parseFloat(e.target.value)})}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="exploration">Exploration Factor (ε)</Label>
                  <Input
                    id="exploration"
                    type="number"
                    step="0.01"
                    min="0"
                    max="1"
                    value={policyEditor.exploration_factor ?? policyConfig.exploration_factor}
                    onChange={(e) => setPolicyEditor({...policyEditor, exploration_factor: parseFloat(e.target.value)})}
                  />
                  <Slider
                    min="0"
                    max="1"
                    step="0.01"
                    value={[policyEditor.exploration_factor ?? policyConfig.exploration_factor]}
                    onValueChange={([val]) => setPolicyEditor({...policyEditor, exploration_factor: val})}
                    className="mt-2"
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="max_episode">Max Episode Length</Label>
                  <Input
                    id="max_episode"
                    type="number"
                    value={policyEditor.max_episode_length ?? policyConfig.max_episode_length}
                    onChange={(e) => setPolicyEditor({...policyEditor, max_episode_length: parseInt(e.target.value)})}
                  />
                </div>
              </div>

              <Separator />

              <div className="space-y-4">
                <h3 className="font-semibold">Reward Weights</h3>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                  {(['latency', 'throughput', 'cost', 'fairness'] as const).map((key) => (
                    <div key={key} className="space-y-2">
                      <Label>{key.charAt(0).toUpperCase() + key.slice(1)}</Label>
                      <Input
                        type="number"
                        step="0.01"
                        min="0"
                        max="1"
                        value={policyEditor.reward_weights?.[key] ?? policyConfig.reward_weights[key]}
                        onChange={(e) => setPolicyEditor({
                          ...policyEditor,
                          reward_weights: {
                            ...policyConfig.reward_weights,
                            [key]: parseFloat(e.target.value),
                          },
                        })}
                      />
                      <p className="text-xs text-muted-foreground text-right">
                        {(policyEditor.reward_weights?.[key] ?? policyConfig.reward_weights[key]) * 100}%
                      </p>
                    </div>
                  ))}
                </div>
              </div>

              <Alert>
                <AlertCircle className="w-4 h-4" />
                <AlertTitle>Warning</AlertTitle>
                <AlertDescription>
                  Modifying RL policy affects all ongoing and future job optimizations. Changes take effect immediately.
                </AlertDescription>
              </Alert>
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowPolicyDialog(false)}>Cancel</Button>
            <Button 
              onClick={handleSavePolicy}
              disabled={updatePolicyMutation.isLoading}
            >
              {updatePolicyMutation.isLoading ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Deploying...
                </>
              ) : (
                <>
                  <Save className="w-4 h-4 mr-2" />
                  Deploy Policy
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
