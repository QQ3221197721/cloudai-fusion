/**
 * CloudAI Fusion Red Team Console - Type Definitions
 * 
 * Philosophy: 知己知彼，百战不殆 (Know yourself, know your enemy, win every battle)
 */

// ============================================
// Target Intelligence Models
// ============================================

export interface TargetIntelligence {
    target_id: string;
    target_name: string;
    analysis_timestamp: string;
    environment: EnvironmentProfile;
    vulnerability_surface: VulnerabilitySurface;
    active_defense_detection: ActiveDefenseInfo;
    network_topology?: NetworkTopology;
}

export interface EnvironmentProfile {
    os_info: OperatingSystemInfo;
    network_topology: NetworkTopology;
    defense_stack: DefenseStack;
    asset_inventory: AssetInventory[];
    service_exposure: ServiceExposure[];
}

export interface OperatingSystemInfo {
    os_type: 'linux' | 'windows' | 'macos' | 'bsd';
    os_version: string;
    kernel_version: string;
    architecture: 'x86_64' | 'arm64' | 'aarch64';
    hardening_level: HardeningLevel;
    security_patches: SecurityPatchStatus;
}

export type HardeningLevel = 'minimal' | 'moderate' | 'hardened' | 'military_grade';

export interface SecurityPatchStatus {
    last_updated: string;
    critical_patches_pending: number;
    recommended_updates: string[];
}

export interface NetworkTopology {
    segments: NetworkSegment[];
    firewalls: FirewallRule[];
    intrusion_detection: IDSConfiguration;
    load_balancers: LoadBalancerConfig[];
    exposed_ports: ExposedPort[];
}

export interface NetworkSegment {
    segment_id: string;
    name: string;
    cidr: string;
    vlan_id?: number;
    trust_level: TrustLevel;
    critical_assets: string[];
}

export type TrustLevel = 'public' | 'semi_private' | 'internal' | 'critical';

export interface FirewallRule {
    rule_id: string;
    direction: 'inbound' | 'outbound';
    action: 'allow' | 'deny';
    protocol: string;
    source_cidr: string;
    destination_cidr: string;
    ports: number[];
    description: string;
}

export interface IDSConfiguration {
    enabled: boolean;
    signature_database_version: string;
    detection_rules: DetectionRule[];
    alert_threshold: AlertThreshold;
}

export interface DetectionRule {
    rule_id: string;
    rule_name: string;
    severity: SeverityLevel;
    pattern: string;
}

export type AlertThreshold = 'low' | 'medium' | 'high' | 'critical';

export interface LoadBalancerConfig {
    lb_id: string;
    type: 'nginx' | 'haproxy' | 'aws_alb' | 'azure_app_gateway';
    health_check_interval: number;
    ssl_termination: boolean;
    waf_enabled: boolean;
}

export interface ExposedPort {
    port: number;
    protocol: 'tcp' | 'udp';
    service: string;
    version?: string;
    banner?: string;
}

export interface DefenseStack {
    antimalware: AntimalwareConfig;
    endpoint_protection: EndpointProtection;
    siem_integration: SIEMConfig;
    patch_management: PatchManagement;
    access_control: AccessControlConfig;
    encryption: EncryptionConfig;
    monitoring: MonitoringConfig;
}

export interface AntimalwareConfig {
    vendor: string;
    product_name: string;
    version: string;
    real_time_protection: boolean;
    signature_updated: string;
}

export interface EndpointProtection {
    edr_enabled: boolean;
    vendor: string;
    detection_capabilities: EdrCapability[];
    response_automatic: boolean;
}

export type EdrCapability = 'behavior_analysis' | 'memory_scanning' | 'fileless_detection' | 'lateral_movement_prevention';

export interface SIEMConfig {
    enabled: boolean;
    vendor: string;
    correlation_rules: number;
    retention_days: number;
    log_sources: string[];
}

export interface PatchManagement {
    automated: boolean;
    schedule: string;
    approval_required: boolean;
    critical_patch_timeline: number; // days
}

export interface AccessControlConfig {
    mfa_enforced: boolean;
    password_policy: PasswordPolicy;
    privilege_escalation_controls: boolean;
    session_timeout: number; // minutes
}

export interface PasswordPolicy {
    min_length: number;
    require_special_chars: boolean;
    require_numbers: boolean;
    max_age_days: number;
    history_count: number;
}

export interface EncryptionConfig {
    data_at_rest: EncryptionStatus;
    data_in_transit: EncryptionStatus;
    key_management: KeyManagementSystem;
}

export interface EncryptionStatus {
    enabled: boolean;
    algorithm: string;
    key_length: number;
}

export type KeyManagementSystem = 'aws_kms' | 'azure_key_vault' | 'hashicorp_vault' | 'cloud_native' | 'on_premises_hsm';

export interface MonitoringConfig {
    logging_enabled: boolean;
    log_aggregation: boolean;
    anomaly_detection: boolean;
    metrics_collection: MetricCollection[];
}

export interface MetricCollection {
    metric_name: string;
    collection_interval: number; // seconds
    retention_period: number; // days
}

export interface AssetInventory {
    asset_id: string;
    asset_type: 'server' | 'database' | 'container' | 'endpoint' | 'network_device';
    hostname: string;
    ip_addresses: string[];
    owner: string;
    criticality: CriticalityLevel;
    data_classification: DataClassification[];
}

export type CriticalityLevel = 'critical' | 'high' | 'medium' | 'low';

export type DataClassification = 'public' | 'internal' | 'confidential' | 'restricted';

export interface ServiceExposure {
    service_name: string;
    external_url: string;
    authentication_required: boolean;
    api_endpoints: ApiEndpoint[];
    exposure_risk: RiskLevel;
}

export interface ApiEndpoint {
    path: string;
    method: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
    authentication: AuthType;
    rate_limiting?: RateLimitConfig;
}

export type AuthType = 'none' | 'basic' | 'bearer' | 'oauth2' | 'api_key' | 'mutual_tls';

export interface RateLimitConfig {
    requests_per_minute: number;
    burst_limit: number;
}

export interface VulnerabilitySurface {
    known_cves: KnownCVE[];
    misconfigurations: Misconfiguration[];
    weak_credentials: WeakCredential[];
    hardening_score: number; // 0-100
    risk_rankings: RiskRanking[];
}

export interface KnownCVE {
    cve_id: string;
    cvss_score: number;
    cvss_vector: string;
    publish_date: string;
    description: string;
    affected_component: string;
    exploit_available: boolean;
    public_exploit_urls: string[];
    mitigation_available: boolean;
    remediation_steps: string[];
    confidence_level: ConfidenceLevel;
}

export type ConfidenceLevel = 'low' | 'medium' | 'high' | 'verified';

export interface Misconfiguration {
    config_id: string;
    category: 'network' | 'security' | 'application' | 'identity';
    severity: SeverityLevel;
    description: string;
    current_setting: string;
    recommended_setting: string;
    compliance_framework?: ComplianceStandard;
}

export type SeverityLevel = 'info' | 'low' | 'medium' | 'high' | 'critical';

export interface WeakCredential {
    credential_id: string;
    location: string;
    credential_type: 'password' | 'api_key' | 'ssh_key' | 'certificate';
    weakness_category: 'default_password' | 'weak_password' | 'plaintext_storage' | 'expired_cert';
    risk_impact: string;
    remediation: string;
}

export interface RiskRanking {
    rank: number;
    threat_vector: string;
    exploitation_difficulty: DifficultyLevel;
    potential_impact: ImpactLevel;
    priority_score: number;
}

export type DifficultyLevel = 'trivial' | 'easy' | 'moderate' | 'difficult' | 'very_difficult';

export type ImpactLevel = 'minimal' | 'low' | 'moderate' | 'high' | 'catastrophic';

export interface ActiveDefenseInfo {
    honeypots_detected: HoneypotInfo[];
    deception_technology: DeceptionTechnology;
    traffic_monitoring: TrafficMonitoring;
    response_readiness: ResponseReadiness;
}

export interface HoneypotInfo {
    honeypot_type: 'low_interaction' | 'high_interaction';
    detected_indicators: string[];
    confidence: number; // 0-1
    likely_location: string;
}

export interface DeceptionTechnology {
    honeyfiles_detected: boolean;
    honeytokens_present: boolean;
    fake_credentials_deployed: boolean;
    decoy_systems: DecoySystem[];
}

export interface DecoySystem {
    decoy_id: string;
    decoy_type: 'server' | 'database' | 'credential_set' | 'document';
    mimics_real_asset: boolean;
    trigger_response: string;
}

export interface TrafficMonitoring {
    deep_packet_inspection: boolean;
    flow_capture_enabled: boolean;
    behavioral_analysis: boolean;
    ai_driven_detection: boolean;
}

export interface ResponseReadiness {
    incident_response_team: boolean;
    playbooks_defined: boolean;
    automated_response_enabled: boolean;
    escalation_matrix_available: boolean;
}

// ============================================
// Attack Planning Models
// ============================================

export interface WeaponArsenalKnowledge {
    weapon_categories: WeaponCategory[];
    total_weapons: number;
    last_updated: string;
    capabilities: WeaponCapabilities;
    limitations: WeaponLimitations;
}

export interface WeaponCategory {
    category_id: string;
    name: string;
    description: string;
    weapons: Weapon[];
    success_statistics: SuccessStatistics;
}

export interface Weapon {
    weapon_id: string;
    name: string;
    category: string;
    type: 'exploit' | 'payload' | 'tool' | 'technique';
    description: string;
    target_platforms: Platform[];
    required_privileges: PrivilegeLevel;
    detection_evasion: EvasionCapability;
    reliability: ReliabilityScore;
    documentation: WeaponDocumentation;
}

export type Platform = 'linux' | 'windows' | 'macos' | 'bsd' | 'container' | 'iot';

export type PrivilegeLevel = 'user' | 'service' | 'system' | 'root' | 'administrator';

export interface EvasionCapability {
    antivirus_bypass: boolean;
    edr_evasion: boolean;
    firewall_bypass: boolean;
    log_clearing: boolean;
    stealth_level: StealthLevel;
}

export type StealthLevel = 'visible' | 'covert' | 'stealthy' | 'ultra_stealth';

export interface ReliabilityScore {
    success_rate: number; // 0-100
    false_positive_rate: number; // 0-100
    consistency_score: number; // 0-100
    environmental_factors: string[];
}

export interface WeaponDocumentation {
    public_references: Reference[];
    technical_details: string;
    usage_examples: CodeSnippet[];
    countermeasures: Countermeasure[];
}

export interface Reference {
    source: 'cve' | 'metasploit' | 'exploit_db' | 'github' | 'research_paper';
    url: string;
    title: string;
    credibility: number; // 0-10
}

export interface CodeSnippet {
    language: string;
    code: string;
    context: string;
    warning: string;
}

export interface Countermeasure {
    detection_method: string;
    prevention_steps: string[];
    response_actions: string[];
}

export interface WeaponCapabilities {
    remote_execution: boolean;
    persistence: boolean;
    privilege_escalation: boolean;
    lateral_movement: boolean;
    data_exfiltration: boolean;
    defensive_evasion: boolean;
}

export interface WeaponLimitations {
    network_requirements: string[];
    environmental_constraints: string[];
    legal_restrictions: string[];
    detection_risks: string[];
}

export interface SuccessStatistics {
    total_attempts: number;
    successful_attacks: number;
    failed_attempts: number;
    average_time_to_exploit: number; // seconds
    platform_breakdown: PlatformSuccessStats[];
}

export interface PlatformSuccessStats {
    platform: Platform;
    attempts: number;
    successes: number;
    success_rate: number;
}

// ============================================
// Attack Graph & Path Models
// ============================================

export interface AttackGraph {
    graph_id: string;
    nodes: AttackGraphNode[];
    edges: AttackGraphEdge[];
    metadata: GraphMetadata;
    q_learning_optimized: QLearningOptimization;
}

export interface AttackGraphNode {
    node_id: string;
    node_type: 'vulnerability' | 'host' | 'service' | 'credential' | 'objective';
    properties: NodeProperties;
    initial_state: boolean;
    goal_state: boolean;
    exploitation_cost: ExploitationCost;
    privilege_level: number; // 0-10
}

export interface NodeProperties {
    host_info: HostInfo;
    service_info: ServiceInfo;
    vulnerability_info: VulnerabilityInfo;
    credential_info: CredentialInfo;
}

export interface HostInfo {
    hostname: string;
    ip_address: string;
    os: string;
    role: string;
}

export interface ServiceInfo {
    service_name: string;
    port: number;
    protocol: string;
    version: string;
}

export interface VulnerabilityInfo {
    cve_ids: string[];
    cvss_score: number;
    exploit_complexity: string;
}

export interface CredentialInfo {
    credential_type: string;
    stored_locally: boolean;
    hash_algorithm?: string;
}

export interface ExploitationCost {
    time_cost: number; // estimated seconds
    resource_cost: number; // 0-100
    skill_requirement: SkillLevel;
    detection_probability: number; // 0-1
}

export type SkillLevel = 'beginner' | 'intermediate' | 'expert' | 'specialist';

export interface AttackGraphEdge {
    edge_id: string;
    source_node_id: string;
    target_node_id: string;
    edge_type: 'exploitation' | 'privilege_escalation' | 'lateral_movement' | 'credential_access';
    prerequisites: Prerequisite[];
    effects: Effect[];
    success_probability: number; // 0-1
    expected_cost: number;
    detection_risk: DetectionRisk;
}

export interface Prerequisite {
    condition_type: 'reachability' | 'privilege' | 'credential' | 'service_running';
    required_value: string;
}

export interface Effect {
    effect_type: 'gain_access' | 'elevate_privilege' | 'collect_credential' | 'install_backdoor';
    new_state: StateChange;
}

export interface StateChange {
    property: string;
    old_value: string;
    new_value: string;
}

export interface DetectionRisk {
    baseline_detection_rate: number; // 0-1
    correlates_with_logs: boolean;
    triggers_alerts: boolean;
    evasion_possibility: number; // 0-1
}

export interface QLearningOptimization {
    converged: boolean;
    optimal_q_values: Map<string, number>;
    learning_iterations: number;
    reward_function: RewardFunction;
    discount_factor: number;
    exploration_rate: number;
}

export interface RewardFunction {
    immediate_reward: number;
    future_reward_weight: number;
    penalty_factors: PenaltyFactor[];
}

export interface PenaltyFactor {
    factor_type: 'detection' | 'resource_consumption' | 'time_delay' | 'failure_risk';
    penalty_weight: number;
}

export interface AttackPath {
    path_id: string;
    path_name: string;
    steps: AttackPathStep[];
    overall_success_probability: number;
    estimated_execution_time: number; // seconds
    overall_risk_score: number; // 0-100
    alternative_paths_available: boolean;
}

export interface AttackPathStep {
    step_number: number;
    action: string;
    tool_or_exploit: string;
    target_node: AttackGraphNode;
    prerequisites_met: boolean;
    success_probability: number;
    estimated_duration: number; // seconds
    risk_level: RiskLevel;
    expected_outcome: Outcome;
}

export interface Outcome {
    achieved_privilege_level: number;
    gained_access: boolean;
    collected_data: string[];
    modified_state: StateChange[];
}

export interface MultipleAttackPaths {
    primary_path: AttackPath;
    alternative_plans: AlternativePath[];
    overall_success_probability: number;
    estimated_execution_time: number;
    overall_risk_score: number;
    q_learning_convergence_verified: boolean;
    bellman_equation_satisfied: boolean;
}

export interface AlternativePath {
    path_id: string;
    path_name: string;
    steps: AttackPathStep[];
    success_probability: number;
    risk_score: number;
    fallback_reason: string;
   切换条件: string[];
}

// ============================================
// Execution Monitoring Models
// ============================================

export interface ExecutionState {
    execution_id: string;
    plan_id: string;
    status: ExecutionStatus;
    current_step: number;
    total_steps: number;
    started_at?: string;
    completed_at?: string;
    progress_percentage: number;
}

export type ExecutionStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'switched_path';

export interface ExecutionLog {
    timestamp: string;
    level: LogLevel;
    message: string;
    step_number?: number;
    action?: string;
    result?: ExecutionResult;
}

export type LogLevel = 'debug' | 'info' | 'warning' | 'error' | 'success';

export interface ExecutionResult {
    success: boolean;
    output: string;
    error_message?: string;
    metrics: ExecutionMetrics;
}

export interface ExecutionMetrics {
    duration_seconds: number;
    memory_used_mb: number;
    cpu_usage_percent: number;
    network_bytes_sent: number;
    detection_events_triggered: number;
}

export interface StageProgress {
    stage_number: number;
    stage_name: string;
    status: StageStatus;
    progress: number; // 0-100
    started_at?: string;
    completed_at?: string;
    error_message?: string;
}

export type StageStatus = 'not_started' | 'in_progress' | 'completed' | 'failed' | 'skipped';

export interface FallbackPlanStatus {
    has_alternatives: boolean;
    current_path_id: string;
    remaining_alternatives: string[];
    switch_triggered: boolean;
    switch_reason?: string;
    switched_to_path?: string;
}

// ============================================
// Dashboard Components Props
// ============================================

export interface TargetAnalysisCardProps {
    targetId?: string;
    mode?: 'quick' | 'deep';
    onAnalysisComplete?: (intelligence: TargetIntelligence) => void;
}

export interface AttackPlanGeneratorProps {
    targetIntel: TargetIntelligence;
    weaponArsenal: WeaponArsenalKnowledge;
    numPaths?: number;
    mode?: 'quick-analysis' | 'full-report' | 'production-ready';
    onPlanGenerated?: (plans: MultipleAttackPaths) => void;
}

export interface ExecutionMonitorProps {
    planId: string;
    autoStart?: boolean;
    onStatusChange?: (status: ExecutionStatus) => void;
    onCompletion?: () => void;
}

export interface AttackPathViewerProps {
    paths: AttackPath;
    showDetails?: boolean;
    interactive?: boolean;
    onStepSelect?: (step: AttackPathStep) => void;
}

export interface AlternativePathsViewerProps {
    alternatives: AlternativePath[];
    onSelectAlternative?: (path: AlternativePath) => void;
}

export interface MetricsSummaryProps {
    successProbability: number;
    estimatedTime: number;
    riskScore: number;
}

// ============================================
// Utility Types
// ============================================

export type ColorScheme = 'light' | 'dark';

export interface ThemeColors {
    primaryRed: string;
    secondaryBlue: string;
    accentGreen: string;
    warningYellow: string;
    dangerRed: string;
    backgrounds: BackgroundColors;
    text: TextColors;
    borders: BorderColors;
}

export interface BackgroundColors {
    light: string;
    dark: string;
    sidebar: string;
    card: string;
    hover: string;
}

export interface TextColors {
    primary: string;
    secondary: string;
    inverse: string;
    success: string;
    warning: string;
    error: string;
}

export interface BorderColors {
    light: string;
    medium: string;
    dark: string;
}
