/**
 * CloudAI Fusion Red Team Console - API Client
 * 
 * Professional API client for Red Team operations with comprehensive error handling
 */

import axios, { AxiosInstance, AxiosError } from 'axios';
import type {
    TargetIntelligence,
    WeaponArsenalKnowledge,
    MultipleAttackPaths,
    KnownCVE,
    NetworkTopology
} from '../types/redteam-types';

const API_BASE_URL = import.meta.env.VITE_REDTEAM_API_URL || '/api/v1/redteam';

class RedTeamAPI {
    private client: AxiosInstance;
    
    constructor() {
        this.client = axios.create({
            baseURL: API_BASE_URL,
            timeout: 30000,
            headers: { 'Content-Type': 'application/json' },
        });
        
        this.client.interceptors.request.use(
            (config) => {
                console.log('[RedTeam API] Request:', config.method?.toUpperCase(), config.url);
                return config;
            },
            (error) => {
                console.error('[RedTeam API] Request error:', error);
                return Promise.reject(error);
            }
        );
        
        this.client.interceptors.response.use(
            (response) => response,
            (error: AxiosError) => {
                console.error('[RedTeam API] Response error:', error.response?.status, error.message);
                if (error.response?.status === 401) {
                    window.location.href = '/login';
                } else if (error.response?.status >= 500) {
                    alert('Server error. Please try again later.');
                }
                return Promise.reject(error);
            }
        );
    }
    
    async listTargets(): Promise<any[]> {
        const response = await this.client.get('/targets');
        return response.data;
    }
    
    async getTarget(targetId: string): Promise<TargetIntelligence> {
        const response = await this.client.get(`/targets/${targetId}`);
        return response.data;
    }
    
    async performReconnaissance(targetId: string, depth: 'quick' | 'deep' = 'deep'): Promise<TargetIntelligence> {
        const response = await this.client.post(`/reconnaissance/${targetId}`, { depth });
        return response.data.intelligence;
    }
    
    async getVulnerabilityScan(targetId: string): Promise<{ cves: KnownCVE[] }> {
        const response = await this.client.get(`/scans/vulnerabilities/${targetId}`);
        return response.data;
    }
    
    async getNetworkTopology(targetId: string): Promise<NetworkTopology> {
        const response = await this.client.get(`/topology/network/${targetId}`);
        return response.data;
    }
    
    async listWeapons(category?: string): Promise<any[]> {
        const params = category ? { category } : {};
        const response = await this.client.get('/weapons', { params });
        return response.data;
    }
    
    async getWeapon(weaponId: string): Promise<any> {
        const response = await this.client.get(`/weapons/${weaponId}`);
        return response.data;
    }
    
    async getWeaponArsenal(): Promise<WeaponArsenalKnowledge> {
        const response = await this.client.get('/arsenal/knowledge');
        return response.data;
    }
    
    async generateOptimalAttackPath(
        targetIntel: Partial<TargetIntelligence>,
        weaponArsenal: Partial<WeaponArsenalKnowledge>,
        numPaths: number = 5,
        mode: 'success-probability' | 'risk-minimization' | 'time-optimization' = 'success-probability'
    ): Promise<MultipleAttackPaths> {
        const response = await this.client.post('/optimization/generate-optimal', {
            target_intel: targetIntel,
            weapon_arsenal: weaponArsenal,
            optimization_mode: mode,
            num_alternatives: numPaths,
        });
        return response.data.attack_paths;
    }
    
    async getAttackGraph(targetId: string): Promise<any> {
        const response = await this.client.get(`/graphs/attack/${targetId}`);
        return response.data;
    }
    
    async startExecution(planId: string): Promise<{ execution_id: string; status: string }> {
        const response = await this.client.post(`/execution/start`, { plan_id: planId });
        return response.data;
    }
    
    async pauseExecution(executionId: string): Promise<void> {
        await this.client.post(`/execution/pause/${executionId}`);
    }
    
    async resumeExecution(executionId: string): Promise<void> {
        await this.client.post(`/execution/resume/${executionId}`);
    }
    
    async stopExecution(executionId: string): Promise<void> {
        await this.client.post(`/execution/stop/${executionId}`);
    }
    
    async getExecutionStatus(executionId: string): Promise<any> {
        const response = await this.client.get(`/execution/status/${executionId}`);
        return response.data;
    }
    
    connectExecutionWebSocket(
        executionId: string,
        onMessage: (data: any) => void,
        onError?: (error: Event) => void,
        onClose?: () => void
    ): WebSocket {
        const wsUrl = `ws://localhost:8080/ws/execution/${executionId}`;
        const ws = new WebSocket(wsUrl);
        
        ws.onopen = () => console.log('[RedTeam WS] Connected');
        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                onMessage(data);
            } catch (e) {
                console.error('[RedTeam WS] Failed to parse message:', e);
            }
        };
        ws.onerror = (error) => {
            console.error('[RedTeam WS] Error:', error);
            onError?.(error);
        };
        ws.onclose = () => {
            console.log('[RedTeam WS] Closed');
            onClose?.();
        };
        
        return ws;
    }
    
    async healthCheck(): Promise<{ status: string; version: string }> {
        const response = await this.client.get('/health');
        return response.data;
    }
}

export const redTeamAPI = new RedTeamAPI();
export default redTeamAPI;
