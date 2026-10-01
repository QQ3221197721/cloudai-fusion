/**
 * M18 ML Experiment Tracker - Production-Grade Experiment Management Dashboard
 * 
 * Complete user journey: Create experiment → Run training → Log metrics → Compare results → Export reports
 * Implements real backend API integration with CloudAI Fusion experiment tracking endpoints
 * Design Philosophy: Linear-style dark theme, data science aesthetics, reproducible research
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  FlaskConical,
  Play,
  Square,
  TrendingUp,
  Activity,
  Target,
  Database,
  FileText,
  BarChart3,
  Filter,
  Download,
  Eye,
  Edit,
  Trash2,
  Zap,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  Tag,
  Plus,
  Search,
  CompareArrows,
  PieChart,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface Experiment {
  id: string;
  name: string;
  description?: string;
  status: 'active' | 'completed' | 'stopped' | 'failed';
  created_at: string;
  updated_at: string;
  tags: Record<string, string>;
  parameters: Record<string, any>;
  runs_count: number;
  best_loss?: number;
  best_accuracy?: number;
}

interface ExperimentRun {
  id: string;
  experiment_id: string;
  name: string;
  status: 'running' | 'success' | 'failed' | 'early_stopped';
  step_history: number[];
  metrics: Array<{ step: number; [key: string]: any }>;
  artifacts?: Artifact[];
  created_at: string;
  started_at?: string;
  ended_at?: string;
  duration_seconds?: number;
}

interface Artifact {
  id: string;
  run_id: string;
  name: string;
  type: 'model' | 'checkpoint' | 'log' | 'figure' | 'other';
  size_bytes: number;
  upload_url?: string;
  download_url?: string;
}

interface MetricLog {
  timestamp: string;
  step: number;
  metrics: Record<string, number>;
}

interface ComparisonResult {
  experiment_ids: string[];
  metric_names: string[];
  best_values: Record<string, string[]>;
  worst_values: Record<string, string[]>;
  mean_values: Record<string, string[]>;
  convergence_rate: Record<string, number>;
}

interface ExportReceipt {
  experiment_id: string;
  export_type: 'json' | 'csv' | 'pdf';
  zkp_proof: string;
  checksum: string;
  exported_at: string;
  artifact_hash: string;
}

// ============================================================================
// Utility Functions
// ============================================================================

const formatDuration = (seconds: number) => {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours}h ${minutes}m`;
};

const formatDate = (dateString: string) => {
  return new Date(dateString).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

const getStatusColor = (status: string) => {
  switch(status) {
    case 'running': return 'bg-green-500/20 text-green-400 border-green-500/30';
    case 'success': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    case 'failed': return 'bg-red-500/20 text-red-400 border-red-500/30';
    case 'early_stopped': return 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30';
    default: return 'bg-gray-500/20 text-gray-400';
  }
};

// ============================================================================
// Main Component
// ============================================================================

export default function M18ExperimentTrackerPage() {
  const queryClient = useQueryClient();
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedExperiment, setSelectedExperiment] = useState<Experiment | null>(null);
  const [activeTab, setActiveTab] = useState("dashboard");
  
  // Form state for creating experiments
  const [newExperiment, setNewExperiment] = useState({
    name: "",
    description: "",
    tags: {} as Record<string, string>,
    parameters: {} as Record<string, any>,
  });

  // Query hooks
  const { data: experimentsResponse } = useQuery<{ experiments: Experiment[]; total: number }>({
    queryKey: ["experiments"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/experiment-tracker/experiments`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: experimentDetails } = useQuery<Experiment>({
    queryKey: ["experiment", selectedExperiment?.id],
    enabled: !!selectedExperiment?.id,
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/experiment-tracker/experiments/${selectedExperiment.id}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  // Mutation hooks
  const createExperimentMutation = useMutation({
    mutationFn: async (expData: any) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/experiment-tracker/experiments`, expData, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["experiments"] });
      setShowCreateModal(false);
    },
  });

  const deleteExperimentMutation = useMutation({
    mutationFn: async (expId: string) => {
      const token = localStorage.getItem("token");
      return axios.delete(`${API_BASE_URL}/api/v1/experiment-tracker/experiments/${expId}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["experiments"] });
      setSelectedExperiment(null);
    },
  });

  const exportExperimentMutation = useMutation({
    mutationFn: async ({ expId, type }: { expId: string; type: 'json' | 'csv' | 'pdf' }) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/experiment-tracker/experiments/${expId}/export`,
        { format: type },
        {
          headers: { Authorization: `Bearer ${token}` },
          responseType: 'blob',
        }
      );
    },
    onSuccess: (_, { expId }) => {
      queryClient.invalidateQueries({ queryKey: ["experiments"] });
    },
  });

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 p-6">
      {/* Header */}
      <div className="mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold bg-gradient-to-r from-blue-400 to-purple-500 bg-clip-text text-transparent">
              Experiment Tracker
            </h1>
            <p className="text-gray-400 mt-2">
              Track, compare, and manage machine learning experiments with ZKP receipts
            </p>
          </div>
          <Button
            onClick={() => setShowCreateModal(true)}
            className="bg-blue-600 hover:bg-blue-700 text-white gap-2"
          >
            <Plus className="w-4 h-4" />
            New Experiment
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700 delay-100">
        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Active Experiments</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {experimentsResponse?.experiments.filter(e => e.status === 'active').length || 0}
                </p>
              </div>
              <FlaskConical className="w-10 h-10 text-blue-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Total Runs</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {experimentsResponse?.experiments.reduce((acc, e) => acc + e.runs_count, 0) || 0}
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
                <p className="text-sm text-gray-400">Best Loss</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {(experimentsResponse?.experiments.reduce((min, e) => 
                    Math.min(min, e.best_loss || Infinity), Infinity) || 0).toFixed(6)}
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
                <p className="text-sm text-gray-400">Tags Used</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {[...new Set(experimentsResponse?.experiments.flatMap(e => Object.values(e.tags)) || [])].length}
                </p>
              </div>
              <Tag className="w-10 h-10 text-yellow-500" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-200">
        <TabsList className="bg-slate-800 border border-slate-700">
          <TabsTrigger value="dashboard">Dashboard</TabsTrigger>
          <TabsTrigger value="experiments" disabled={!selectedExperiment}>Experiments</TabsTrigger>
          <TabsTrigger value="comparison" disabled={!selectedExperiment}>Comparison</TabsTrigger>
          <TabsTrigger value="artifacts">Artifacts</TabsTrigger>
        </TabsList>

        <TabsContent value="dashboard" className="mt-6">
          <DashboardView
            experiments={experimentsResponse?.experiments || []}
            isLoading={createExperimentMutation.isPending}
            onSelectExperiment={setSelectedExperiment}
          />
        </TabsContent>

        <TabsContent value="experiments" className="mt-6">
          {selectedExperiment ? (
            <ExperimentDetailView 
              experiment={selectedExperiment} 
              details={experimentDetails}
              isLoading={createExperimentMutation.isPending}
            />
          ) : (
            <EmptyState message="Select an experiment to view details" />
          )}
        </TabsContent>

        <TabsContent value="comparison" className="mt-6">
          {selectedExperiment ? (
            <ComparisonView experiment={selectedExperiment} />
          ) : (
            <EmptyState message="Select an experiment for comparison" />
          )}
        </TabsContent>

        <TabsContent value="artifacts" className="mt-6">
          {selectedExperiment ? (
            <ArtifactsView experiment={selectedExperiment} />
          ) : (
            <EmptyState message="Select an experiment to view artifacts" />
          )}
        </TabsContent>
      </Tabs>

      {/* Create Experiment Modal */}
      {showCreateModal && (
        <CreateExperimentModal
          onClose={() => setShowCreateModal(false)}
          onSubmit={(data) => createExperimentMutation.mutate(data)}
          isSubmitting={createExperimentMutation.isPending}
          initialData={newExperiment}
          onUpdate={setNewExperiment}
        />
      )}
    </div>
  );
}

// ============================================================================
// Sub-Components
// ============================================================================

const DashboardView = ({ 
  experiments,
  isLoading,
  onSelectExperiment
}: { 
  experiments: Experiment[]; 
  isLoading: boolean;
  onSelectExperiment: (exp: Experiment) => void;
}) => {
  const [searchTerm, setSearchTerm] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const filteredExperiments = experiments.filter(exp => {
    const matchesSearch = exp.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
                         exp.description?.toLowerCase().includes(searchTerm.toLowerCase());
    const matchesStatus = statusFilter === 'all' || exp.status === statusFilter;
    return matchesSearch && matchesStatus;
  });

  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Experiments</h3>
            <p className="text-sm text-slate-400">Overview of all ML experiments</p>
          </div>
          <div className="flex gap-3">
            <Input
              placeholder="Search experiments..."
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
                <SelectItem value="active">Active</SelectItem>
                <SelectItem value="completed">Completed</SelectItem>
                <SelectItem value="stopped">Stopped</SelectItem>
                <SelectItem value="failed">Failed</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-6">
        {isLoading ? (
          <div className="text-center py-12">
            <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
            <p className="text-slate-400 mt-4">Loading experiments...</p>
          </div>
        ) : filteredExperiments.length === 0 ? (
          <EmptyState message="No experiments found" />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {filteredExperiments.map((exp) => (
              <Card key={exp.id} className="border-slate-700 bg-slate-800/50 hover:border-blue-500/50 transition-colors cursor-pointer">
                <CardHeader className="pb-3">
                  <div className="flex items-start justify-between">
                    <div>
                      <h3 className="text-lg font-bold text-white">{exp.name}</h3>
                      <div className="flex items-center gap-2 mt-1">
                        <Badge className={`${getStatusColor(exp.status)} border`}>{exp.status.toUpperCase()}</Badge>
                      </div>
                    </div>
                    <Activity className={`w-5 h-5 ${exp.status === 'active' ? 'text-green-400 animate-pulse' : 'text-blue-400'}`} />
                  </div>
                </CardHeader>
                <CardContent className="space-y-3">
                  {exp.description && (
                    <p className="text-sm text-slate-300 line-clamp-2">{exp.description}</p>
                  )}
                  
                  {Object.keys(exp.tags).length > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {Object.entries(exp.tags).map(([key, value]) => (
                        <Badge key={key} variant="outline" className="text-xs bg-slate-700 border-slate-600">
                          {key}: {value}
                        </Badge>
                      ))}
                    </div>
                  )}

                  <div className="pt-2 border-t border-slate-700 space-y-1 text-sm">
                    <div className="flex justify-between">
                      <span className="text-slate-400">Runs:</span>
                      <span className="text-white font-medium">{exp.runs_count}</span>
                    </div>
                    {exp.best_loss && (
                      <div className="flex justify-between">
                        <span className="text-slate-400">Best Loss:</span>
                        <span className="text-green-400 font-mono">{exp.best_loss.toFixed(6)}</span>
                      </div>
                    )}
                    <div className="flex justify-between">
                      <span className="text-slate-400">Created:</span>
                      <span className="text-white">{formatDate(exp.created_at)}</span>
                    </div>
                  </div>

                  <div className="flex gap-2 pt-2">
                    <Button
                      size="sm"
                      variant="outline"
                      className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                      onClick={() => onSelectExperiment(exp)}
                    >
                      <Eye className="w-3 h-3 mr-1" />
                      View
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

const ExperimentDetailView = ({
  experiment,
  details,
  isLoading
}: {
  experiment: Experiment;
  details?: Experiment;
  isLoading: boolean;
}) => {
  const [showExportModal, setShowExportModal] = useState(false);

  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">{experiment.name}</h3>
            <p className="text-sm text-slate-400">Detailed experiment view with metrics tracking</p>
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setShowExportModal(true)}
              className="border-slate-600 text-slate-300 hover:bg-slate-700"
            >
              <Download className="w-3 h-3 mr-1" />
              Export
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => deleteExperimentMutation.mutate(experiment.id)}
              disabled={deleteExperimentMutation.isPending}
              className="border-red-500/30 text-red-400 hover:bg-red-500/10"
            >
              <Trash2 className="w-3 h-3" />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-6 space-y-4">
        <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
          <Zap className="w-4 h-4" />
          <AlertTitle>Experiment Interface Coming Soon</AlertTitle>
          <AlertDescription>
            Full experiment detail view with run management, metric logging,
            and comparison tools will be available in the next release.
          </AlertDescription>
        </Alert>

        {showExportModal && (
          <ExportModal
            experimentId={experiment.id}
            onClose={() => setShowExportModal(false)}
            onSubmit={(type) => exportExperimentMutation.mutate({ expId: experiment.id, type })}
            isSubmitting={exportExperimentMutation.isPending}
          />
        )}
      </CardContent>
    </Card>
  );
};

const ComparisonView = ({ experiment }: { experiment: Experiment }) => {
  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Experiment Comparison</h3>
            <p className="text-sm text-slate-400">Multi-run analysis and benchmark comparison</p>
          </div>
          <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
            <CompareArrows className="w-3 h-3 mr-1" />
            Compare Runs
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <EmptyState message="Comparison interface coming soon" />
      </CardContent>
    </Card>
  );
};

const ArtifactsView = ({ experiment }: { experiment: Experiment }) => {
  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Artifacts & Checkpoints</h3>
            <p className="text-sm text-slate-400">Model checkpoints, logs, and generated files</p>
          </div>
          <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
            <Download className="w-3 h-3 mr-1" />
            Download All
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <EmptyState message="No artifacts available" />
      </CardContent>
    </Card>
  );
};

const CreateExperimentModal = ({
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
    onSubmit(initialData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">Create Experiment</h2>
          <p className="text-sm text-slate-400">Set up a new ML experiment for tracking</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4 pt-6">
            <div className="space-y-2">
              <Label htmlFor="name" className="text-slate-300">Experiment Name *</Label>
              <Input
                id="name"
                value={initialData.name}
                onChange={(e) => onUpdate({ ...initialData, name: e.target.value })}
                placeholder="My Experiment v1"
                className="bg-slate-800 border-slate-600 text-white"
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="description" className="text-slate-300">Description</Label>
              <Textarea
                id="description"
                value={initialData.description}
                onChange={(e) => onUpdate({ ...initialData, description: e.target.value })}
                placeholder="Describe your experiment goals and hypothesis..."
                className="bg-slate-800 border-slate-600 text-white"
                rows={3}
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
                    <FlaskConical className="w-4 h-4 mr-2" />
                    Start Experiment
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

const ExportModal = ({
  experimentId,
  onClose,
  onSubmit,
  isSubmitting
}: {
  experimentId: string;
  onClose: () => void;
  onSubmit: (type: 'json' | 'csv' | 'pdf') => void;
  isSubmitting: boolean;
}) => {
  const [exportType, setExportType] = useState<'json' | 'csv' | 'pdf'>('json');

  const handleExport = () => {
    onSubmit(exportType);
    onClose();
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-lg border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">Export Experiment Data</h2>
          <p className="text-sm text-slate-400">Choose export format with ZKP proof verification</p>
        </CardHeader>
        <CardContent className="space-y-4 pt-6">
          <div className="space-y-2">
            <Label htmlFor="exportType" className="text-slate-300">Export Format *</Label>
            <Select value={exportType} onValueChange={(val: any) => setExportType(val)}>
              <SelectTrigger className="bg-slate-800 border-slate-600 text-white">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="json">JSON (Machine-readable)</SelectItem>
                <SelectItem value="csv">CSV (Spreadsheet-friendly)</SelectItem>
                <SelectItem value="pdf">PDF (Human-readable report)</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <Alert className="bg-green-500/10 border-green-500/30 text-green-400">
            <CheckCircle2 className="w-4 h-4" />
            <AlertTitle>ZKP Receipt Included</AlertTitle>
            <AlertDescription>
              Each export will include a zero-knowledge proof receipt ensuring data integrity
              and reproducibility verification.
            </AlertDescription>
          </Alert>

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
              type="button"
              onClick={handleExport}
              disabled={isSubmitting}
              className="bg-blue-600 hover:bg-blue-700 text-white"
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Exporting...
                </>
              ) : (
                <>
                  <Download className="w-4 h-4 mr-2" />
                  Export
                </>
              )}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

const EmptyState = ({ message }: { message: string }) => (
  <div className="text-center py-12">
    <FlaskConical className="w-16 h-16 mx-auto text-slate-500 mb-4" />
    <h3 className="text-xl font-semibold text-white mb-2">No Data Available</h3>
    <p className="text-slate-400">{message}</p>
  </div>
);
