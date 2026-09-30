/**
 * M8 Global Configuration Manager - Production-Grade Dashboard
 * 
 * Complete user journey: Feature Flags Management → System Configs → Config Hierarchy
 * Implements real backend API integration with CloudAI Fusion configuration endpoints
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import {
  Settings,
  ToggleLeft,
  ToggleRight,
  Save,
  Trash2,
  RefreshCw,
  Plus,
  Download,
  Upload,
  Search,
  Filter,
  ChevronDown,
  GitBranch,
  Layers,
  Key,
  Code,
  CheckCircle2,
  XCircle,
  Loader2,
  Zap,
  FileJson,
  Eye,
  EyeOff,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface ConfigCategory {
  name: string;
  description: string;
  icon: React.ReactNode;
  count: number;
}

interface ConfigItem {
  key: string;
  value: any;
  type: 'boolean' | 'string' | 'int' | 'float' | 'array' | 'object';
  scope: 'global' | 'tenant' | 'user';
  category?: string;
  description?: string;
  last_modified: string;
  modified_by?: string;
  is_feature_flag?: boolean;
  enabled?: boolean;
}

interface ConfigHierarchyNode {
  path: string;
  value: any;
  scope: string;
  environment?: string;
  inherited_from?: string;
  override_source?: string;
}

interface ConfigExportFormat {
  version: string;
  exported_at: string;
  export_by?: string;
  global_configs: Record<string, any>;
  tenant_configs: Record<string, any>;
  feature_flags: Record<string, boolean>;
  metadata: {
    total_keys: number;
    categories: string[];
  };
}

// ============================================================================
// Component: M8_ConfigManager_Page
// ============================================================================

export default function M8ConfigManagerPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  
  // State Management
  const [configs, setConfigs] = useState<ConfigItem[]>([]);
  const [selectedConfig, setSelectedConfig] = useState<ConfigItem | null>(null);
  const [showEditModal, setShowEditModal] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [filterCategory, setFilterCategory] = useState<string>('all');
  const [scopeFilter, setScopeFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [viewMode, setViewMode] = useState<'list' | 'tree' | 'json'>('list');
  const [editFormData, setEditFormData] = useState<Partial<ConfigItem>>({});
  const [showExportDialog, setShowExportDialog] = useState(false);
  const [hierarchyData, setHierarchyData] = useState<ConfigHierarchyNode[]>([]);
  const [showHierarchy, setShowHierarchy] = useState(false);
  const [configTree, setConfigTree] = useState<any>(null);

  // Fetch all configs
  async function fetchConfigs(): Promise<ConfigItem[]> {
    const response = await axios.get('/api/v1/config');
    return response.data.configs || response.data;
  }

  const { data: configData = [], isLoading: configsLoading, refetch: refetchConfigs } = useQuery({
    queryKey: ['m8-configs'],
    queryFn: fetchConfigs,
    staleTime: 5000,
    refetchOnWindowFocus: true,
  });

  // Sync local state with query data
  useEffect(() => {
    if (configData && Array.isArray(configData)) {
      setConfigs(configData);
    }
  }, [configData]);

  // Mutation handlers
  const updateConfigMutation = useMutation({
    mutationFn: async (updates: Partial<ConfigItem>) => {
      if (!selectedConfig) throw new Error("No selected config");
      await axios.put(`/api/v1/config/${encodeURIComponent(selectedConfig.key)}`, updates);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['m8-configs'] });
      setShowEditModal(false);
    },
  });

  const deleteConfigMutation = useMutation({
    mutationFn: async (key: string) => {
      await axios.delete(`/api/v1/config/${encodeURIComponent(key)}`);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['m8-configs'] });
    },
  });

  const hierarchyQuery = useQuery({
    queryKey: ['m8-hierarchy'],
    queryFn: async () => {
      const response = await axios.get('/api/v1/config/hierarchy');
      return response.data.nodes || [];
    },
    enabled: false,
  });

  // Filtering logic
  const filteredConfigs = configs.filter(config => {
    const matchesCategory = filterCategory === 'all' || !config.category || config.category === filterCategory;
    const matchesScope = scopeFilter === 'all' || config.scope === scopeFilter;
    const matchesSearch = config.key.toLowerCase().includes(searchQuery.toLowerCase()) ||
                          config.description?.toLowerCase().includes(searchQuery.toLowerCase());
    return matchesCategory && matchesScope && matchesSearch;
  });

  // Extract unique categories
  const categories = Array.from(new Set(configs.map(c => c.category).filter(Boolean)));

  // Handlers
  const handleEditClick = (config: ConfigItem) => {
    setSelectedConfig(config);
    setEditFormData({
      key: config.key,
      value: config.value,
      type: config.type,
      scope: config.scope,
      category: config.category,
      description: config.description,
      is_feature_flag: config.is_feature_flag,
      enabled: config.enabled,
    });
    setShowEditModal(true);
  };

  const handleDeleteClick = async (key: string) => {
    if (confirm(`Are you sure you want to delete configuration "${key}"?`)) {
      deleteConfigMutation.mutate(key);
    }
  };

  const handleToggleFeatureFlag = async (config: ConfigItem) => {
    await updateConfigMutation.mutateAsync({
      ...config,
      enabled: !config.enabled,
    });
  };

  const handleExportConfig = async () => {
    const exportData: ConfigExportFormat = {
      version: '1.0',
      exported_at: new Date().toISOString(),
      global_configs: {},
      tenant_configs: {},
      feature_flags: {},
      metadata: {
        total_keys: configs.length,
        categories: categories,
      },
    };

    configs.forEach(config => {
      if (config.scope === 'global') {
        exportData.global_configs[config.key] = config.value;
      } else if (config.scope === 'tenant') {
        exportData.tenant_configs[config.key] = config.value;
      }
      if (config.is_feature_flag) {
        exportData.feature_flags[config.key] = config.enabled ?? false;
      }
    });

    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `config-export-${new Date().toISOString().split('T')[0]}.json`;
    link.click();
    window.URL.revokeObjectURL(url);
  };

  // Calculate statistics
  const stats = {
    total: configs.length,
    enabled: configs.filter(c => c.enabled === true).length,
    disabled: configs.filter(c => c.enabled === false).length,
    featureFlags: configs.filter(c => c.is_feature_flag).length,
    byScope: {
      global: configs.filter(c => c.scope === 'global').length,
      tenant: configs.filter(c => c.scope === 'tenant').length,
      user: configs.filter(c => c.scope === 'user').length,
    },
  };

  // Render config value display
  const renderValueDisplay = (value: any, type: string) => {
    if (type === 'boolean') {
      return (
        <Badge variant={value ? 'default' : 'secondary'} className="gap-1">
          {value ? <CheckCircle2 className="w-3 h-3" /> : <XCircle className="w-3 h-3" />}
          {String(value)}
        </Badge>
      );
    }
    if (['array', 'object'].includes(type)) {
      return (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <FileJson className="w-3 h-3" />
          {typeof value === 'object' ? JSON.stringify(value).slice(0, 50) + '...' : String(value)}
        </div>
      );
    }
    return <span className="font-mono text-sm">{String(value)}</span>;
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 p-6 space-y-6">
      {/* Header */}
      <div className="space-y-2 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <h1 className="text-4xl font-bold gradient-text flex items-center gap-3">
          <Settings className="w-10 h-10 text-primary" />
          Global Configuration Manager
        </h1>
        <p className="text-gray-400 text-lg">
          Manage feature flags, system configurations, and hierarchical settings
        </p>
      </div>

      {/* Statistics Cards */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Total</p>
                <p className="text-2xl font-bold text-primary">{stats.total}</p>
              </div>
              <Layers className="w-8 h-8 text-muted-foreground opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Enabled</p>
                <p className="text-2xl font-bold text-emerald-400">{stats.enabled}</p>
              </div>
              <ToggleRight className="w-8 h-8 text-emerald-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Disabled</p>
                <p className="text-2xl font-bold text-red-400">{stats.disabled}</p>
              </div>
              <ToggleLeft className="w-8 h-8 text-red-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Features</p>
                <p className="text-2xl font-bold text-purple-400">{stats.featureFlags}</p>
              </div>
              <Zap className="w-8 h-8 text-purple-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Global</p>
                <p className="text-2xl font-bold text-blue-400">{stats.byScope.global}</p>
              </div>
              <Key className="w-8 h-8 text-blue-400 opacity-50" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Toolbar */}
      <Card className="bg-card/50 backdrop-blur border-accent/20">
        <CardContent className="p-4">
          <div className="flex flex-wrap gap-4 items-center">
            <div className="flex-1 min-w-64">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                  placeholder="Search configurations..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-10"
                />
              </div>
            </div>

            <Select value={filterCategory} onValueChange={setFilterCategory}>
              <SelectTrigger className="w-48">
                <SelectValue placeholder="Filter by category" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Categories</SelectItem>
                {categories.map(cat => (
                  <SelectItem key={cat} value={cat}>{cat}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={scopeFilter} onValueChange={setScopeFilter}>
              <SelectTrigger className="w-40">
                <SelectValue placeholder="Scope" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Scopes</SelectItem>
                <SelectItem value="global">Global</SelectItem>
                <SelectItem value="tenant">Tenant</SelectItem>
                <SelectItem value="user">User</SelectItem>
              </SelectContent>
            </Select>

            <Separator orientation="vertical" className="h-8" />

            <Button variant="outline" size="icon" onClick={() => setViewMode('list')} title="List View">
              <Layers className="w-4 h-4" />
            </Button>
            <Button variant="outline" size="icon" onClick={() => setShowHierarchy(true)} title="Hierarchy Tree">
              <GitBranch className="w-4 h-4" />
            </Button>
            <Button variant="outline" size="icon" onClick={handleExportConfig} title="Export Config">
              <Download className="w-4 h-4" />
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Main Content Area */}
      <Tabs defaultValue="list" className="flex-1">
        <div className="grid grid-cols-[1fr] gap-4">
          <TabsContent value="list" className="m-0">
            <ScrollArea className="h-[calc(100vh-400px)]">
              <div className="space-y-3">
                {configsLoading ? (
                  <div className="flex items-center justify-center py-12">
                    <Loader2 className="w-8 h-8 animate-spin text-primary" />
                  </div>
                ) : filteredConfigs.length === 0 ? (
                  <div className="text-center py-12 text-muted-foreground">
                    No configurations found matching your filters
                  </div>
                ) : (
                  filteredConfigs.map((config) => (
                    <Card key={config.key} className="bg-card/50 backdrop-blur border-accent/20 hover:border-accent/40 transition-colors">
                      <CardContent className="p-4">
                        <div className="flex items-start justify-between gap-4">
                          <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-2 mb-2">
                              <h3 className="font-semibold text-primary break-words">{config.key}</h3>
                              {config.is_feature_flag && (
                                <Badge variant="outline" className="text-xs bg-purple-500/20 text-purple-400 border-purple-500/30">
                                  Feature Flag
                                </Badge>
                              )}
                              <Badge variant={config.scope === 'global' ? 'default' : 'secondary'} className="text-xs">
                                {config.scope.toUpperCase()}
                              </Badge>
                            </div>

                            {config.description && (
                              <p className="text-sm text-muted-foreground mb-2 line-clamp-2">
                                {config.description}
                              </p>
                            )}

                            <div className="flex items-center gap-3 text-xs text-muted-foreground">
                              <code className="px-2 py-1 rounded bg-muted">Type: {config.type}</code>
                              <span>Last modified: {new Date(config.last_modified).toLocaleString()}</span>
                              {config.modified_by && (
                                <span>by {config.modified_by}</span>
                              )}
                            </div>
                          </div>

                          <div className="flex items-center gap-2 shrink-0">
                            {config.is_feature_flag && (
                              <Switch
                                checked={config.enabled ?? false}
                                onCheckedChange={handleToggleFeatureFlag}
                                className="data-[state=checked]:bg-emerald-500"
                              />
                            )}
                            <Button
                              variant="outline"
                              size="icon"
                              onClick={() => handleEditClick(config)}
                            >
                              <Settings className="w-4 h-4" />
                            </Button>
                            <Button
                              variant="outline"
                              size="icon"
                              onClick={() => handleDeleteClick(config.key)}
                              disabled={deleteConfigMutation.isLoading}
                            >
                              <Trash2 className="w-4 h-4" />
                            </Button>
                          </div>
                        </div>

                        <Separator className="my-3" />

                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="text-sm text-muted-foreground">Current Value:</span>
                            {renderValueDisplay(config.value, config.type)}
                          </div>
                        </div>
                      </CardContent>
                    </Card>
                  ))
                )}
              </div>
            </ScrollArea>
          </TabsContent>

          {/* Hierarchy View */}
          <TabsContent value="tree">
            <Card className="bg-card/50 backdrop-blur border-accent/20">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h2 className="text-xl font-bold">Configuration Hierarchy</h2>
                    <p className="text-sm text-muted-foreground">
                      Visual tree showing configuration inheritance and overrides
                    </p>
                  </div>
                  <Button onClick={() => hierarchyQuery.refetch()}>
                    <RefreshCw className={`w-4 h-4 mr-2 ${hierarchyQuery.isFetching ? 'animate-spin' : ''}`} />
                    Refresh
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                {!hierarchyQuery.isSuccess ? (
                  <div className="flex items-center justify-center py-12">
                    <p className="text-muted-foreground">Click refresh to load hierarchy</p>
                  </div>
                ) : (
                  <pre className="bg-muted/50 p-4 rounded-lg overflow-auto max-h-[500px] text-xs">
                    {JSON.stringify(hierarchyQuery.data, null, 2)}
                  </pre>
                )}
              </CardContent>
            </Card>
          </TabsContent>
        </div>
      </Tabs>

      {/* Edit Modal */}
      <Dialog open={showEditModal} onOpenChange={setShowEditModal}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Edit Configuration</DialogTitle>
            <DialogDescription>
              Modify the configuration key, value, type, and scope settings
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="key">Configuration Key *</Label>
                <Input
                  id="key"
                  value={editFormData.key || ''}
                  onChange={(e) => setEditFormData({...editFormData, key: e.target.value})}
                  disabled={!selectedConfig}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="type">Type *</Label>
                <Select
                  value={editFormData.type}
                  onValueChange={(value: any) => setEditFormData({...editFormData, type: value})}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="boolean">Boolean</SelectItem>
                    <SelectItem value="string">String</SelectItem>
                    <SelectItem value="int">Integer</SelectItem>
                    <SelectItem value="float">Float</SelectItem>
                    <SelectItem value="array">Array</SelectItem>
                    <SelectItem value="object">Object</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="value">Value</Label>
              {editFormData.type === 'object' || editFormData.type === 'array' ? (
                <Textarea
                  id="value"
                  placeholder='{"key": "value"}'
                  value={typeof editFormData.value === 'object' ? JSON.stringify(editFormData.value, null, 2) : String(editFormData.value)}
                  onChange={(e) => {
                    try {
                      setEditFormData({
                        ...editFormData,
                        value: JSON.parse(e.target.value),
                      });
                    } catch {
                      // Keep previous value if invalid JSON
                    }
                  }}
                  className="font-mono text-sm min-h-[150px]"
                />
              ) : (
                <Input
                  id="value"
                  type={editFormData.type === 'int' || editFormData.type === 'float' ? 'number' : 'text'}
                  value={editFormData.value !== undefined ? String(editFormData.value) : ''}
                  onChange={(e) => {
                    const val = e.target.value;
                    if (editFormData.type === 'int') {
                      setEditFormData({ ...editFormData, value: parseInt(val) });
                    } else if (editFormData.type === 'float') {
                      setEditFormData({ ...editFormData, value: parseFloat(val) });
                    } else {
                      setEditFormData({ ...editFormData, value: val });
                    }
                  }}
                />
              )}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="scope">Scope *</Label>
                <Select
                  value={editFormData.scope}
                  onValueChange={(value: any) => setEditFormData({...editFormData, scope: value})}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="global">Global</SelectItem>
                    <SelectItem value="tenant">Tenant</SelectItem>
                    <SelectItem value="user">User</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label htmlFor="category">Category</Label>
                <Input
                  id="category"
                  value={editFormData.category || ''}
                  onChange={(e) => setEditFormData({...editFormData, category: e.target.value})}
                  list="category-list"
                />
                <datalist id="category-list">
                  {categories.map(cat => (
                    <option key={cat} value={cat} />
                  ))}
                </datalist>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Textarea
                id="description"
                placeholder="Describe this configuration..."
                value={editFormData.description || ''}
                onChange={(e) => setEditFormData({...editFormData, description: e.target.value})}
                rows={3}
              />
            </div>

            {editFormData.type === 'boolean' && (
              <div className="space-y-2">
                <Label className="flex items-center gap-2">
                  <ToggleRight className="w-4 h-4" />
                  Enable / Disable
                </Label>
                <Select
                  value={editFormData.enabled ? 'true' : 'false'}
                  onValueChange={(value) => setEditFormData({...editFormData, enabled: value === 'true'})}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="true">Enabled</SelectItem>
                    <SelectItem value="false">Disabled</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowEditModal(false)}>Cancel</Button>
            <Button onClick={() => updateConfigMutation.mutate(editFormData)} disabled={updateConfigMutation.isLoading}>
              {updateConfigMutation.isLoading ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="w-4 h-4 mr-2" />
                  Save Changes
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
