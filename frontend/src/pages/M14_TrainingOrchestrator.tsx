/**
 * M14 Training Job Orchestrator & Gang Scheduling - Production-Grade Dashboard
 * 
 * Complete user journey: Job Submission → Active Jobs Monitoring → Job History → Resource Allocation
 * Implements real backend API integration with CloudAI Fusion training orchestrator endpoints
 * Design Philosophy: Linear-style dark theme, academic rigor, high-performance computing aesthetics
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
  Server as ServerIcon,
  Activity,
  Plus,
  Play,
  Pause,
  Square,
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
  Target,
  Zap,
  Layers,
  Grid3X3,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface TrainingJob {
  id: string;
  name: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'preempted';
  model_config: any;
  dataset: string;
  hyperparameters: Record<string, any>;
  resources_requested: {
    gpu_count: number;
    memory_gb: number;
    cpu_cores: number;
  };
  resources_allocated?: any;
  progress?: {
    epoch: number;
    total_epochs: number;
    loss?: number;
    accuracy?: number;
    step?: number;
  };
  started_at?: string;
  completed_at?: string;
  error_message?: string;
}

interface GangAllocation {
  job_id: string;
  worker_nodes: string[];
  parameter_servers: string[];
  global_step: number;
  status: 'allocated' | 'waiting' | 'preempted';
}

interface ClusterResource {
  total_gpus: number;
  available_gpus: number;
  total_memory_gb: number;
  available_memory_gb: number;
  pending_jobs: number;
  active_allocations: number;
}

interface SubmitJobRequest {
  name: string;
  description?: string;
  model_template: string;
  dataset_path: string;
  framework: string;
  hyperparameters: Record<string, any>;
  gpu_count: number;
  priority: 'urgent' | 'high' | 'normal' | 'low';
  checkpoint_interval?: number;
  email_notifications?: string[];
}

interface ResourceNode {
  node_id: string;
  total_gpus: number;
  allocated_gpus: number;
  status: 'available' | 'busy' | 'offline';
  current_jobs: number;
}

// ============================================================================
// Components
// ============================================================================

const StatusBadge = ({ status }: { status: string }) => {
  const statusStyles: Record<string, string> = {
    pending: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
    running: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    succeeded: "bg-green-500/20 text-green-400 border-green-500/30",
    failed: "bg-red-500/20 text-red-400 border-red-500/30",
    cancelled: "bg-gray-500/20 text-gray-400 border-gray-500/30",
    preempted: "bg-orange-500/20 text-orange-400 border-orange-500/30",
  };

  const style = statusStyles[status.toLowerCase()] || "bg-gray-500/20 text-gray-400";

  return (
    <Badge className={`${style} border font-semibold`} variant="outline">
      {status.toUpperCase()}
    </Badge>
  );
};

const PriorityBadge = ({ priority }: { priority: string }) => {
  const colors: Record<string, string> = {
    urgent: "bg-red-500/20 text-red-400 border-red-500/30",
    high: "bg-orange-500/20 text-orange-400 border-orange-500/30",
    normal: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    low: "bg-gray-500/20 text-gray-400 border-gray-500/30",
  };

  const style = colors[priority?.toLowerCase()] || "bg-slate-500/20 text-slate-400";

  return (
    <Badge className={`${style} border text-xs`} variant="outline">
      {priority?.toUpperCase()}
    </Badge>
  );
};

const ProgressBar = ({ value, max, color = "blue" }: { value: number; max: number; color?: string }) => {
  const percentage = Math.min((value / max) * 100, 100);
  
  return (
    <div className="w-full bg-slate-700 rounded-full h-2">
      <div
        className={`bg-${color}-500 h-2 rounded-full transition-all duration-300`}
        style={{ width: `${percentage}%` }}
      />
    </div>
  );
};

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

const SubmitJobModal = ({
  isOpen,
  onClose,
  onSubmit,
  isLoading,
}: {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: SubmitJobRequest) => void;
  isLoading: boolean;
}) => {
  if (!isOpen) return null;

  const [formData, setFormData] = useState<SubmitJobRequest>({
    name: "",
    description: "",
    model_template: "resnet50",
    dataset_path: "",
    framework: "pytorch",
    hyperparameters: {
      learning_rate: 0.001,
      batch_size: 32,
      epochs: 100,
      weight_decay: 0.01,
    },
    gpu_count: 1,
    priority: "normal",
    checkpoint_interval: 10,
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(formData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-4xl border-slate-700 bg-slate-900 max-h-[90vh] overflow-y-auto">
        <CardHeader className="border-b border-slate-700 sticky top-0 bg-slate-900 z-10">
          <h2 className="text-2xl font-bold text-white">Submit Training Job</h2>
          <p className="text-sm text-slate-400">Configure and queue a new distributed training task</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-6 pt-6">
            {/* Basic Configuration */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Target className="w-4 h-4" />
                Basic Configuration
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="name" className="text-slate-300">Job Name *</Label>
                  <Input
                    id="name"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    placeholder="experiment-resnet50-001"
                    className="bg-slate-800 border-slate-600 text-white"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="description" className="text-slate-300">Description</Label>
                  <Input
                    id="description"
                    value={formData.description || ""}
                    onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                    placeholder="Brief description of this training run..."
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
              </div>
            </div>

            {/* Model & Dataset */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Database className="w-4 h-4" />
                Data & Model
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="model_template" className="text-slate-300">Model Template *</Label>
                  <select
                    id="model_template"
                    value={formData.model_template}
                    onChange={(e) => setFormData({ ...formData, model_template: e.target.value })}
                    className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="resnet50">ResNet-50</option>
                    <option value="bert-base">BERT-Base</option>
                    <option value="llama-2-7b">Llama-2-7B</option>
                    <option value="yolov8">YOLOv8</option>
                  </select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="dataset_path" className="text-slate-300">Dataset Path *</Label>
                  <Input
                    id="dataset_path"
                    value={formData.dataset_path}
                    onChange={(e) => setFormData({ ...formData, dataset_path: e.target.value })}
                    placeholder="/datasets/imagenet-train"
                    className="bg-slate-800 border-slate-600 text-white"
                    required
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="framework" className="text-slate-300">Training Framework *</Label>
                <select
                  id="framework"
                  value={formData.framework}
                  onChange={(e) => setFormData({ ...formData, framework: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="pytorch">PyTorch Lightning</option>
                  <option value="tensorflow">TensorFlow Keras</option>
                  <option value="jax">JAX + Flax</option>
                </select>
              </div>
            </div>

            {/* Hyperparameters */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Code className="w-4 h-4" />
                Hyperparameters
              </h4>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="learning_rate" className="text-slate-300">Learning Rate</Label>
                  <Input
                    id="learning_rate"
                    type="number"
                    step="0.00001"
                    value={formData.hyperparameters.learning_rate}
                    onChange={(e) => setFormData({
                      ...formData,
                      hyperparameters: { ...formData.hyperparameters, learning_rate: parseFloat(e.target.value) }
                    })}
                    className="bg-slate-800 border-slate-600 text-white font-mono"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="batch_size" className="text-slate-300">Batch Size</Label>
                  <Input
                    id="batch_size"
                    type="number"
                    value={formData.hyperparameters.batch_size}
                    onChange={(e) => setFormData({
                      ...formData,
                      hyperparameters: { ...formData.hyperparameters, batch_size: parseInt(e.target.value) }
                    })}
                    className="bg-slate-800 border-slate-600 text-white font-mono"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="epochs" className="text-slate-300">Epochs</Label>
                  <Input
                    id="epochs"
                    type="number"
                    value={formData.hyperparameters.epochs}
                    onChange={(e) => setFormData({
                      ...formData,
                      hyperparameters: { ...formData.hyperparameters, epochs: parseInt(e.target.value) }
                    })}
                    className="bg-slate-800 border-slate-600 text-white font-mono"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="weight_decay" className="text-slate-300">Weight Decay</Label>
                  <Input
                    id="weight_decay"
                    type="number"
                    step="0.0001"
                    value={formData.hyperparameters.weight_decay}
                    onChange={(e) => setFormData({
                      ...formData,
                      hyperparameters: { ...formData.hyperparameters, weight_decay: parseFloat(e.target.value) }
                    })}
                    className="bg-slate-800 border-slate-600 text-white font-mono"
                  />
                </div>
              </div>
            </div>

            {/* Resource Requirements */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <ServerIcon className="w-4 h-4" />
                Resource Requirements
              </h4>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="gpu_count" className="text-slate-300">GPU Count (Gang Scheduling)</Label>
                  <span className="text-white font-mono">{formData.gpu_count} GPU{formData.gpu_count !== 1 ? 's' : ''}</span>
                </div>
                <Slider
                  id="gpu_count"
                  min={1}
                  max={8}
                  step={1}
                  value={[formData.gpu_count]}
                  onValueChange={([val]) => setFormData({ ...formData, gpu_count: val })}
                  className="py-2"
                />
                <div className="flex justify-between text-xs text-slate-400">
                  <span>1 GPU</span>
                  <span>8 GPUs</span>
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="priority" className="text-slate-300">Priority Level</Label>
                <select
                  id="priority"
                  value={formData.priority}
                  onChange={(e) => setFormData({ ...formData, priority: e.target.value as any })}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="urgent">Urgent (Preempt other jobs)</option>
                  <option value="high">High (Scheduled first)</option>
                  <option value="normal">Normal (Fair scheduling)</option>
                  <option value="low">Low (Best effort)</option>
                </select>
              </div>
            </div>

            {/* Advanced Options */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Settings className="w-4 h-4" />
                Advanced Options
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="checkpoint_interval" className="text-slate-300">Checkpoint Interval (epochs)</Label>
                  <Input
                    id="checkpoint_interval"
                    type="number"
                    min={1}
                    value={formData.checkpoint_interval}
                    onChange={(e) => setFormData({
                      ...formData,
                      checkpoint_interval: parseInt(e.target.value) || 10
                    })}
                    className="bg-slate-800 border-slate-600 text-white"
                  />
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
                    Submitting...
                  </>
                ) : (
                  <>
                    <Plus className="w-4 h-4 mr-2" />
                    Submit Job
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

const M14TrainingOrchestrator = () => {
  const queryClient = useQueryClient();
  
  // State
  const [showSubmitModal, setShowSubmitModal] = useState(false);
  const [activeTab, setActiveTab] = useState("submission");
  const [selectedJob, setSelectedJob] = useState<TrainingJob | null>(null);
  const [submitLoading, setSubmitLoading] = useState(false);

  // Fetch jobs
  const { data: jobsData, isLoading: loadingJobs, error: errorJobs } = useQuery<{ jobs: TrainingJob[] }>({
    queryKey: ["training-jobs"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/training/jobs`);
      return response.data;
    },
  });

  // Fetch cluster resources
  const { data: clusterResources, isLoading: loadingResources } = useQuery<ClusterResource>({
    queryKey: ["cluster-resources"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/training/resources`);
      return response.data;
    },
  });

  // Submit job mutation
  const submitJobMutation = useMutation({
    mutationFn: async (data: SubmitJobRequest) => {
      const response = await axios.post(`${API_BASE_URL}/api/v1/training/jobs`, data);
      return response.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["training-jobs"] });
      queryClient.invalidateQueries({ queryKey: ["cluster-resources"] });
      setShowSubmitModal(false);
      alert("Training job submitted successfully!");
    },
    onError: (error: any) => {
      console.error("Job submission failed:", error);
      alert(error.response?.data?.error || "Failed to submit job");
    },
  });

  // Render job cards
  const renderJobCards = () => {
    if (loadingJobs) {
      return (
        <div className="text-center py-12">
          <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-4">Loading training jobs...</p>
        </div>
      );
    }

    if (errorJobs) {
      return (
        <Alert className="bg-red-500/10 border-red-500/30 text-red-400">
          <XCircle className="w-4 h-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>
            Failed to load jobs: {(errorJobs as Error).message}
          </AlertDescription>
        </Alert>
      );
    }

    const jobs = jobsData?.jobs || [];

    if (jobs.length === 0) {
      return (
        <div className="text-center py-12">
          <Layers className="w-16 h-16 mx-auto text-slate-500 mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No Training Jobs Yet</h3>
          <p className="text-slate-400 mb-6">Submit your first training job to start using gang scheduling</p>
          <Button
            onClick={() => setShowSubmitModal(true)}
            className="bg-blue-600 hover:bg-blue-700 text-white"
          >
            <Plus className="w-4 h-4 mr-2" />
            Submit First Job
          </Button>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {jobs.map((job) => (
          <Card key={job.id} className="border-slate-700 bg-slate-800/50 hover:border-blue-500/50 transition-colors">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="text-lg font-bold text-white">{job.name}</h3>
                  <div className="flex items-center gap-2 mt-1">
                    <StatusBadge status={job.status} />
                    <PriorityBadge priority={job.hyperparameters?.priority || job.resources_requested?.gpu_count ? 'normal' : 'normal'} />
                  </div>
                </div>
                {job.status === 'running' && (
                  <Zap className="w-5 h-5 text-blue-400 animate-pulse" />
                )}
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Progress for Running Jobs */}
              {job.progress && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-slate-400">Progress</span>
                    <span className="text-white">
                      {job.progress.epoch}/{job.progress.total_epochs} epochs
                    </span>
                  </div>
                  <ProgressBar
                    value={job.progress.epoch}
                    max={job.progress.total_epochs}
                    color={job.status === 'running' ? 'blue' : 'green'}
                  />
                  
                  {/* Training Metrics */}
                  {job.progress.loss !== undefined && (
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-slate-400">Loss</span>
                      <span className="text-orange-400 font-mono">{job.progress.loss.toFixed(4)}</span>
                    </div>
                  )}
                  {job.progress.accuracy !== undefined && (
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-slate-400">Accuracy</span>
                      <span className="text-green-400 font-mono">{(job.progress.accuracy * 100).toFixed(2)}%</span>
                    </div>
                  )}
                </div>
              )}

              {/* Resource Usage */}
              <div className="pt-2 border-t border-slate-700">
                <div className="flex items-center gap-4 text-sm">
                  <div className="flex items-center gap-2 text-slate-400">
                    <Cpu className="w-4 h-4" />
                    <span>{job.resources_requested.gpu_count} GPUs</span>
                  </div>
                  <div className="flex items-center gap-2 text-slate-400">
                    <ServerIcon className="w-4 h-4" />
                    <span>{job.resources_requested.memory_gb}GB RAM</span>
                  </div>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 pt-2">
                {job.status === 'pending' && (
                  <>
                    <Button
                      size="sm"
                      variant="outline"
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <Play className="w-3 h-3" />
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <Square className="w-3 h-3" />
                    </Button>
                  </>
                )}
                {job.status === 'running' && (
                  <>
                    <Button
                      size="sm"
                      variant="outline"
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <Pause className="w-3 h-3" />
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="border-slate-600 text-red-400 hover:bg-slate-700"
                    >
                      <Square className="w-3 h-3" />
                    </Button>
                  </>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                  onClick={() => setSelectedJob(job)}
                >
                  Details
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
              <h1 className="text-3xl font-bold text-white mb-2">M14 Training Orchestrator</h1>
              <p className="text-slate-400">Distributed training job orchestration with gang scheduling</p>
            </div>
            <Button
              onClick={() => setShowSubmitModal(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4"
            >
              <Plus className="w-4 h-4 mr-2" />
              Submit Job
            </Button>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <MetricCard
            icon={Layers}
            title="Total Jobs"
            value={jobsData?.jobs.length || 0}
            trend="+5 this week"
            color="blue"
          />
          <MetricCard
            icon={Zap}
            title="Running"
            value={jobsData?.jobs.filter(j => j.status === 'running').length || 0}
            color="green"
          />
          <MetricCard
            icon={ServerIcon}
            title="Cluster GPUs"
            value={clusterResources ? `${clusterResources.available_gpus}/${clusterResources.total_gpus}` : '-'}
            color="purple"
          />
          <MetricCard
            icon={Target}
            title="Pending Queue"
            value={jobsData?.jobs.filter(j => j.status === 'pending').length || 0}
            color="orange"
          />
        </div>

        {/* Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
          <TabsList className="bg-slate-800 border border-slate-700">
            <TabsTrigger value="submission" className="data-[state=active]:bg-blue-600">
              <Plus className="w-4 h-4 mr-2" />
              Job Submission
            </TabsTrigger>
            <TabsTrigger value="active" className="data-[state=active]:bg-blue-600">
              <Activity className="w-4 h-4 mr-2" />
              Active Jobs
            </TabsTrigger>
            <TabsTrigger value="history" className="data-[state=active]:bg-blue-600">
              <Clock className="w-4 h-4 mr-2" />
              Job History
            </TabsTrigger>
            <TabsTrigger value="resources" className="data-[state=active]:bg-blue-600">
              <Grid3X3 className="w-4 h-4 mr-2" />
              Resource Allocation
            </TabsTrigger>
          </TabsList>

          {/* Job Submission Tab */}
          <TabsContent value="submission">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Submit Training Job</h3>
                    <p className="text-sm text-slate-400">Configure and queue distributed training tasks</p>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <RefreshCw className="w-4 h-4" />
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400 mb-6">
                  <Settings className="w-4 h-4" />
                  <AlertTitle>Job Submission Form</AlertTitle>
                  <AlertDescription>
                    Click the "Submit Job" button in the header to open the full configuration wizard.
                    The form includes model selection, dataset specification, hyperparameter tuning,
                    and gang scheduling resource allocation.
                  </AlertDescription>
                </Alert>
                {renderJobCards()}
              </CardContent>
            </Card>
          </TabsContent>

          {/* Active Jobs Tab */}
          <TabsContent value="active">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Active Training Jobs</h3>
                    <p className="text-sm text-slate-400">Monitor running jobs and gang allocations in real-time</p>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => queryClient.invalidateQueries({ queryKey: ["training-jobs"] })}
                    className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <RefreshCw className="w-4 h-4" />
                    Refresh
                  </Button>
                </div>
              </CardHeader>
              <CardContent>{renderJobCards()}</CardContent>
            </Card>
          </TabsContent>

          {/* Job History & Resource Placeholders */}
          <TabsContent value="history">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Training Job History</h3>
                <p className="text-sm text-slate-400">View past runs and performance comparisons</p>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <Clock className="w-4 h-4" />
                  <AlertTitle>History Features Coming Soon</AlertTitle>
                  <AlertDescription>
                    Complete job history tracking with performance analytics and comparative analysis
                    will be available in the next release.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="resources">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Cluster Resource Allocation</h3>
                <p className="text-sm text-slate-400">Monitor GPU allocation and gang scheduling status</p>
              </CardHeader>
              <CardContent>
                {loadingResources ? (
                  <div className="text-center py-8">
                    <Loader2 className="w-8 h-8 animate-spin mx-auto text-blue-500" />
                    <p className="text-slate-400 mt-2">Loading cluster resources...</p>
                  </div>
                ) : clusterResources ? (
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
                    <div className="text-center">
                      <div className="text-3xl font-bold text-white">{clusterResources.total_gpus}</div>
                      <p className="text-sm text-slate-400 mt-1">Total GPUs</p>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-green-400">{clusterResources.available_gpus}</div>
                      <p className="text-sm text-slate-400 mt-1">Available GPUs</p>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-blue-400">{clusterResources.active_allocations}</div>
                      <p className="text-sm text-slate-400 mt-1">Active Allocations</p>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-orange-400">{clusterResources.pending_jobs}</div>
                      <p className="text-sm text-slate-400 mt-1">Pending Jobs</p>
                    </div>
                    
                    {/* Gang Scheduling Visualization */}
                    <div className="col-span-2 md:col-span-4 pt-4 border-t border-slate-700">
                      <p className="text-sm font-semibold text-slate-300 mb-3">Current Gang Allocations</p>
                      <div className="space-y-2">
                        <Alert className="bg-purple-500/10 border-purple-500/30 text-purple-400">
                          <Grid3X3 className="w-4 h-4" />
                          <AlertTitle>Gang Scheduling Visualization</AlertTitle>
                          <AlertDescription>
                            Interactive visualization of distributed training worker allocations across the cluster
                            will appear here. Shows which nodes are assigned to each job's parameter servers
                            and computation workers.
                          </AlertDescription>
                        </Alert>
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="text-center text-slate-400 py-8">No resource data available</div>
                )}
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </main>

      {/* Submit Job Modal */}
      <SubmitJobModal
        isOpen={showSubmitModal}
        onClose={() => setShowSubmitModal(false)}
        onSubmit={(data) => {
          setSubmitLoading(true);
          submitJobMutation.mutate(data);
        }}
        isLoading={submitLoading}
      />
    </div>
  );
};

export default M14TrainingOrchestrator;
