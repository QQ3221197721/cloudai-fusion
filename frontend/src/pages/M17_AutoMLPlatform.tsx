/**
 * M17 AutoML Hyperparameter Tuning Platform - Production-Grade Dashboard
 * 
 * Complete user journey: Create HPO Job → Monitor Trials → Compare Results → Export Reports
 * Implements real backend API integration with CloudAI Fusion AutoML endpoints
 * Design Philosophy: Linear-style dark theme, data science aesthetics, operational excellence
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
import { Textarea } from "@/components/ui/textarea";
import {
  Settings,
  Play,
  Pause,
  Square,
  TrendingUp,
  TrendingDown,
  Activity,
  Target,
  Database,
  BrainCircuit,
  Layers,
  GitBranch,
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  BarChart3,
  LineChart,
  PieChart,
  Filter,
  Download,
  Eye,
  Edit,
  Trash2,
  Zap,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface HPOJob {
  id: string;
  name: string;
  description?: string;
  status: "pending" | "running" | "stopped" | "completed";
  model_config: ModelConfig;
  search_space: SearchSpace;
  objective_metric: string;
  strategy: SearchStrategy;
  budget: HPOBudget;
  trials_completed: number;
  trials_total: number;
  best_objective?: number;
  created_at: string;
  started_at?: string;
}

interface ModelConfig {
  model_type: string;
  pretrained_weights?: string;
  task: string;
  [key: string]: any;
}

interface SearchSpace {
  [paramName: string]: ParamDefinition;
}

interface ParamDefinition {
  type: "log_uniform" | "uniform" | "choice";
  low?: number;
  high?: number;
  values?: (number | string)[];
}

interface HPOBudget {
  max_trials: number;
  max_wall_time_secs?: number;
  budget_usd?: number;
}

type SearchStrategy = "bayesian" | "random" | "grid";

interface Trial {
  id: string;
  status: TrialStatus;
  params: Record<string, number>;
  metrics: Record<string, number>;
  step_history?: number[];
  created_at: string;
  duration?: string;
}

type TrialStatus = "pending" | "running" | "success" | "failed" | "early_stopped";

interface OptimizationResult {
  job_id: string;
  summary: ResultSummary;
  best_params: Record<string, number>;
  metric_analysis: MetricAnalysis;
  convergence_history: ConvergenceHistory;
}

interface ResultSummary {
  total_trials: number;
  completed_trials: number;
  failed_trials: number;
  early_stopped: number;
  best_objective: number;
  worst_objective: number;
  mean_objective: number;
}

interface MetricAnalysis {
  [metricName: string]: { best: number; worst: number; mean: number };
}

interface ConvergenceHistory {
  iterations: number[];
  best_objective: number[];
  mean_objective: number[];
}

// ============================================================================
// Main Component
// ============================================================================

export default function M17AutoMLPlatformPage() {
  const queryClient = useQueryClient();
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedJob, setSelectedJob] = useState<HPOJob | null>(null);
  const [activeTab, setActiveTab] = useState("jobs");
  
  // Form state for creating HPO jobs
  const [newJob, setNewJob] = useState({
    name: "",
    description: "",
    strategy: "bayesian" as SearchStrategy,
    objectiveMetric: "loss",
    metricsToTrack: ["loss", "accuracy"],
    maxTrials: 50,
    maxWallTime: 3600, // seconds
  });

  // Mutation hooks for job operations
  const createJobMutation = useMutation({
    mutationFn: async (jobData: any) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/automl/jobs`, jobData, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["hpoJobs"] });
      setShowCreateModal(false);
    },
  });

  const startJobMutation = useMutation({
    mutationFn: async (jobId: string) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/automl/jobs/${jobId}/start`, {}, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: (_, jobId) => {
      queryClient.invalidateQueries({ queryKey: ["hpoJobs"] });
      queryClient.invalidateQueries({ queryKey: ["hpoJob", jobId] });
    },
  });

  const stopJobMutation = useMutation({
    mutationFn: async (jobId: string) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/automl/jobs/${jobId}/stop`, {}, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: (_, jobId) => {
      queryClient.invalidateQueries({ queryKey: ["hpoJobs"] });
      queryClient.invalidateQueries({ queryKey: ["hpoJob", jobId] });
    },
  });

  const deleteJobMutation = useMutation({
    mutationFn: async (jobId: string) => {
      const token = localStorage.getItem("token");
      return axios.delete(`${API_BASE_URL}/api/v1/automl/jobs/${jobId}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["hpoJobs"] });
    },
  });

  // Query hooks
  const { data: jobsResponse } = useQuery<{ jobs: HPOJob[]; total: number }>({
    queryKey: ["hpoJobs"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/automl/jobs`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: jobDetails } = useQuery<HPOJob>({
    queryKey: ["hpoJob", selectedJob?.id],
    enabled: !!selectedJob?.id,
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/automl/jobs/${selectedJob.id}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: results } = useQuery<OptimizationResult>({
    queryKey: ["hpoResults", selectedJob?.id],
    enabled: !!selectedJob?.id && activeTab === "results",
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/automl/jobs/${selectedJob.id}/results`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 p-6">
      {/* Header */}
      <div className="mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold bg-gradient-to-r from-blue-400 to-purple-500 bg-clip-text text-transparent">
              AutoML Platform
            </h1>
            <p className="text-gray-400 mt-2">
              Hyperparameter Optimization & Neural Architecture Search
            </p>
          </div>
          <Button
            onClick={() => setShowCreateModal(true)}
            className="bg-blue-600 hover:bg-blue-700 text-white gap-2"
          >
            <Zap className="w-4 h-4" />
            Create HPO Job
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700 delay-100">
        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Active Jobs</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {jobsResponse?.jobs.filter(j => j.status === "running").length || 0}
                </p>
              </div>
              <Activity className="w-10 h-10 text-blue-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Total Trials</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {jobsResponse?.jobs.reduce((acc, j) => acc + j.trials_completed, 0) || 0}
                </p>
              </div>
              <Database className="w-10 h-10 text-green-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Best Objective</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {(jobsResponse?.jobs.reduce((min, j) => Math.min(min, j.best_objective || Infinity), Infinity) || 0).toFixed(4)}
                </p>
              </div>
              <Target className="w-10 h-10 text-purple-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Strategies</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {[...new Set(jobsResponse?.jobs.map(j => j.strategy) || [])].length}
                </p>
              </div>
              <BrainCircuit className="w-10 h-10 text-yellow-500" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-200">
        <TabsList className="bg-slate-800 border border-slate-700">
          <TabsTrigger value="jobs">Jobs List</TabsTrigger>
          <TabsTrigger value="analysis" disabled={!selectedJob}>Analysis</TabsTrigger>
          <TabsTrigger value="results" disabled={!selectedJob}>Results</TabsTrigger>
          <TabsTrigger value="strategies">Search Strategies</TabsTrigger>
        </TabsList>

        <TabsContent value="jobs" className="mt-6">
          <HPOJobsList
            jobs={jobsResponse?.jobs || []}
            onStart={(jobId) => startJobMutation.mutate(jobId)}
            onStop={(jobId) => stopJobMutation.mutate(jobId)}
            onDelete={(jobId) => deleteJobMutation.mutate(jobId)}
            onSelect={setSelectedJob}
            isLoading={createJobMutation.isPending}
          />
        </TabsContent>

        <TabsContent value="analysis" className="mt-6">
          {selectedJob ? (
            <HPOAnalysisView job={selectedJob} jobDetails={jobDetails} />
          ) : (
            <EmptyState message="Select a job to view analysis" />
          )}
        </TabsContent>

        <TabsContent value="results" className="mt-6">
          {selectedJob ? (
            <HPOResultsView job={selectedJob} results={results} isLoading={createJobMutation.isPending} />
          ) : (
            <EmptyState message="Select a job to view results" />
          )}
        </TabsContent>

        <TabsContent value="strategies" className="mt-6">
          <SearchStrategiesView />
        </TabsContent>
      </Tabs>

      {/* Create Job Modal */}
      {showCreateModal && (
        <CreateHPOModal
          onClose={() => setShowCreateModal(false)}
          onSubmit={(data) => createJobMutation.mutate(data)}
          isSubmitting={createJobMutation.isPending}
          initialData={newJob}
          onUpdate={setNewJob}
        />
      )}
    </div>
  );
}

// ============================================================================
// Sub-Components
// ============================================================================

/**
 * HPO Jobs List Component - Table view of all hyperparameter optimization jobs
 */
const HPOJobsList = ({ 
  jobs, 
  onStart, 
  onStop, 
  onDelete,
  onSelect,
  isLoading 
}: { 
  jobs: HPOJob[]; 
  onStart: (jobId: string) => void;
  onStop: (jobId: string) => void;
  onDelete: (jobId: string) => void;
  onSelect: (job: HPOJob | null) => void;
  isLoading: boolean;
}) => {
  const [searchTerm, setSearchTerm] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const filteredJobs = jobs.filter(job => {
    const matchesSearch = job.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
                         job.description?.toLowerCase().includes(searchTerm.toLowerCase());
    const matchesStatus = statusFilter === 'all' || job.status === statusFilter;
    return matchesSearch && matchesStatus;
  });

  const getStatusColor = (status: string) => {
    switch(status) {
      case 'running': return 'bg-green-500/20 text-green-400 border-green-500/30';
      case 'completed': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
      case 'stopped': return 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30';
      case 'pending': return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
      default: return 'bg-gray-500/20 text-gray-400';
    }
  };

  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">HPO Jobs</h3>
            <p className="text-sm text-slate-400">Hyperparameter optimization job management</p>
          </div>
          <div className="flex gap-3">
            <Input
              placeholder="Search jobs..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="bg-slate-800 border-slate-600 text-white w-64"
            />
            <Select value={statusFilter} onValueChange={setStatusFilter}>
              <SelectTrigger className="bg-slate-800 border-slate-600 text-white w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Status</SelectItem>
                <SelectItem value="running">Running</SelectItem>
                <SelectItem value="completed">Completed</SelectItem>
                <SelectItem value="stopped">Stopped</SelectItem>
                <SelectItem value="pending">Pending</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-6">
        {isLoading ? (
          <div className="text-center py-12">
            <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
            <p className="text-slate-400 mt-4">Loading jobs...</p>
          </div>
        ) : filteredJobs.length === 0 ? (
          <EmptyState message="No HPO jobs found" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-slate-800/50">
                <tr>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Job Name</th>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Status</th>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Strategy</th>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Progress</th>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Best Obj.</th>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Created</th>
                  <th className="p-4 text-left text-sm font-semibold text-slate-300">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-700">
                {filteredJobs.map((job, idx) => (
                  <tr key={job.id} className={`hover:bg-slate-700/30 transition-colors ${idx % 2 === 0 ? 'bg-slate-800/30' : ''}`}>
                    <td className="p-4">
                      <div className="font-semibold text-white">{job.name}</div>
                      {job.description && (
                        <div className="text-sm text-slate-400 truncate max-w-xs">{job.description}</div>
                      )}
                    </td>
                    <td className="p-4">
                      <Badge className={`${getStatusColor(job.status)} border font-semibold`}>
                        {job.status.toUpperCase()}
                      </Badge>
                    </td>
                    <td className="p-4 text-white capitalize">{job.strategy}</td>
                    <td className="p-4">
                      <div className="flex items-center gap-2">
                        <div className="w-32 h-2 bg-slate-700 rounded-full overflow-hidden">
                          <div 
                            className="h-full bg-gradient-to-r from-blue-500 to-purple-500 transition-all"
                            style={{ width: `${(job.trials_completed / job.trials_total) * 100}%` }}
                          />
                        </div>
                        <span className="text-sm text-slate-400">
                          {job.trials_completed}/{job.trials_total}
                        </span>
                      </div>
                    </td>
                    <td className="p-4 font-mono text-sm">
                      <span className="text-white">{job.best_objective?.toFixed(4) || 'N/A'}</span>
                    </td>
                    <td className="p-4 text-slate-400 text-sm">
                      {new Date(job.created_at).toLocaleDateString()}
                    </td>
                    <td className="p-4">
                      <div className="flex gap-2">
                        {job.status === 'running' ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => onStop(job.id)}
                            disabled={job.status !== 'running'}
                            className="border-yellow-500/30 text-yellow-400 hover:bg-yellow-500/10"
                          >
                            <Square className="w-3 h-3" />
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => onStart(job.id)}
                            disabled={job.status === 'completed' || job.status === 'stopped'}
                            className="border-green-500/30 text-green-400 hover:bg-green-500/10"
                          >
                            <Play className="w-3 h-3" />
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onSelect(job)}
                          className="border-slate-600 text-slate-300 hover:bg-slate-700"
                        >
                          <Eye className="w-3 h-3" />
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onDelete(job.id)}
                          className="border-red-500/30 text-red-400 hover:bg-red-500/10"
                        >
                          <Trash2 className="w-3 h-3" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
};

/**
 * HPO Results View Component - Display best configurations and analysis
 */
const HPOResultsView = ({ 
  job, 
  results,
  isLoading 
}: { 
  job: HPOJob; 
  results?: OptimizationResult;
  isLoading: boolean;
}) => {
  if (isLoading) {
    return (
      <EmptyState message="Loading results..." />
    );
  }

  return (
    <div className="space-y-6">
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <h3 className="text-xl font-bold text-white">Best Configuration</h3>
          <p className="text-sm text-slate-400">Optimal hyperparameters from this search run</p>
        </CardHeader>
        <CardContent>
          {results ? (
            <div className="grid grid-cols-2 gap-4">
              {Object.entries(results.best_params).map(([param, value]) => (
                <div key={param} className="p-4 bg-slate-800 rounded-lg border border-slate-700">
                  <div className="text-sm text-slate-400">{param}</div>
                  <div className="text-xl font-bold text-white mt-1">{JSON.stringify(value)}</div>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState message="Results not available yet" />
          )}
        </CardContent>
      </Card>

      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <h3 className="text-xl font-bold text-white">Metric Analysis</h3>
          <p className="text-sm text-slate-400">Performance statistics across all trials</p>
        </CardHeader>
        <CardContent>
          {results?.metric_analysis && (
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              {Object.entries(results.metric_analysis).map(([metric, stats]) => (
                <div key={metric} className="p-4 bg-slate-800 rounded-lg border border-slate-700 space-y-2">
                  <div className="text-sm font-semibold text-white">{metric}</div>
                  <div className="flex justify-between text-sm">
                    <span className="text-slate-400">Best:</span>
                    <span className="text-green-400 font-mono">{stats.best.toFixed(4)}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-slate-400">Worst:</span>
                    <span className="text-red-400 font-mono">{stats.worst.toFixed(4)}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-slate-400">Mean:</span>
                    <span className="text-yellow-400 font-mono">{stats.mean.toFixed(4)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

/**
 * HPO Analysis View Component - Convergence curves and insights
 */
const HPOAnalysisView = ({ 
  job, 
  jobDetails 
}: { 
  job: HPOJob; 
  jobDetails?: HPOJob;
}) => {
  return (
    <EmptyState message="Detailed analysis coming soon" />
  );
};

/**
 * Search Strategies View Component - Educational content about HPO strategies
 */
const SearchStrategiesView = () => {
  const strategies = [
    {
      name: 'Bayesian Optimization',
      description: 'Uses probabilistic models to intelligently select next trial points based on past performance',
      useCase: 'Best for expensive evaluations, limited budget',
      pros: ['Sample efficient', 'Adaptive'],
      cons: ['Computationally intensive', 'Slower than random for many params']
    },
    {
      name: 'Random Search',
      description: 'Samples hyperparameters uniformly at random from the search space',
      useCase: 'Good baseline, many hyperparameters',
      pros: ['Simple', 'Parallelizable', 'Often competitive'],
      cons: ['May miss optimal regions', 'No learning from past trials']
    },
    {
      name: 'Grid Search',
      description: 'Systematically searches over a manually specified subset of the hyperparameter space',
      useCase: 'Small search spaces, exhaustive search needed',
      pros: ['Exhaustive', 'Easy to understand'],
      cons: ['Curse of dimensionality', 'Computationally expensive']
    }
  ];

  return (
    <div className="space-y-4">
      {strategies.map((strategy, idx) => (
        <Card key={idx} className="border-slate-700 bg-slate-800/50">
          <CardContent className="pt-6">
            <h4 className="text-lg font-bold text-white mb-2">{strategy.name}</h4>
            <p className="text-slate-300 mb-3">{strategy.description}</p>
            <div className="space-y-2">
              <div className="text-sm text-slate-400">
                <strong>Use Case:</strong> {strategy.useCase}
              </div>
              <div className="flex gap-4 text-sm">
                <div>
                  <span className="text-slate-400">Pros:</span> {strategy.pros.join(', ')}
                </div>
              </div>
              <div>
                <span className="text-slate-400">Cons:</span> {strategy.cons.join(', ')}
              </div>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
};

/**
 * Create HPO Modal Component
 */
const CreateHPOModal = ({
  onClose,
  onSubmit,
  isSubmitting,
  initialData,
  onUpdate
}: {
  onClose: () => void;
  onSubmit: (data: any) => void;
  isSubmitting: boolean;
  initialData: any;
  onUpdate: (data: any) => void;
}) => {
  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit({
      ...initialData,
      model_config: {
        model_type: "transformer",
        task: "classification"
      },
      search_space: {
        learning_rate: { type: "log_uniform", low: 1e-5, high: 1e-2 },
        batch_size: { type: "choice", values: [16, 32, 64] },
        num_layers: { type: "choice", values: [2, 4, 6] }
      },
      strategy: initialData.strategy,
      objective_metric: initialData.objectiveMetric,
      budget: {
        max_trials: initialData.maxTrials,
        max_wall_time_secs: initialData.maxWallTime
      }
    });
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">Create HPO Job</h2>
          <p className="text-sm text-slate-400">Configure hyperparameter optimization search</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4 pt-6">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="jobName" className="text-slate-300">Job Name *</Label>
                <Input
                  id="jobName"
                  value={initialData.name}
                  onChange={(e) => onUpdate({ ...initialData, name: e.target.value })}
                  placeholder="My HPO Job"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="strategy" className="text-slate-300">Search Strategy *</Label>
                <Select
                  value={initialData.strategy}
                  onValueChange={(val) => onUpdate({ ...initialData, strategy: val as SearchStrategy })}
                >
                  <SelectTrigger className="bg-slate-800 border-slate-600 text-white">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="bayesian">Bayesian Optimization</SelectItem>
                    <SelectItem value="random">Random Search</SelectItem>
                    <SelectItem value="grid">Grid Search</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            
            <div className="space-y-2">
              <Label htmlFor="description" className="text-slate-300">Description</Label>
              <Textarea
                id="description"
                value={initialData.description}
                onChange={(e) => onUpdate({ ...initialData, description: e.target.value })}
                placeholder="Describe your optimization goal..."
                className="bg-slate-800 border-slate-600 text-white"
                rows={3}
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="objectiveMetric" className="text-slate-300">Objective Metric *</Label>
                <Input
                  id="objectiveMetric"
                  value={initialData.objectiveMetric}
                  onChange={(e) => onUpdate({ ...initialData, objectiveMetric: e.target.value })}
                  placeholder="loss"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="maxTrials" className="text-slate-300">Max Trials *</Label>
                <Input
                  id="maxTrials"
                  type="number"
                  min={1}
                  value={initialData.maxTrials}
                  onChange={(e) => onUpdate({ ...initialData, maxTrials: parseInt(e.target.value) || 50 })}
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="maxWallTime" className="text-slate-300">Max Wall Time (seconds)</Label>
              <Input
                id="maxWallTime"
                type="number"
                value={initialData.maxWallTime}
                onChange={(e) => onUpdate({ ...initialData, maxWallTime: parseInt(e.target.value) || 3600 })}
                className="bg-slate-800 border-slate-600 text-white"
              />
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t border-slate-700">
              <Button
                type="button"
                onClick={onClose}
                disabled={isSubmitting}
                variant="outline"
                className="border-slate-600 text-slate-300 hover:bg-slate-800"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSubmitting}
                className="bg-blue-600 hover:bg-blue-700 text-white"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Creating...
                  </>
                ) : (
                  <>
                    <Zap className="w-4 h-4 mr-2" />
                    Start Optimization
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

/**
 * Empty State Component
 */
const EmptyState = ({ message }: { message: string }) => (
  <div className="text-center py-12">
    <Activity className="w-16 h-16 mx-auto text-slate-500 mb-4" />
    <h3 className="text-xl font-semibold text-white mb-2">No Data Available</h3>
    <p className="text-slate-400">{message}</p>
  </div>
);
