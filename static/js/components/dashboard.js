import { apiRequest } from '../api.js';
import { showNotification } from './notifications.js';

export async function loadDashboardData() {
    try {
        const [monitors, status] = await Promise.all([
            apiRequest('/api/monitors'),
            apiRequest('/api/status')
        ]);

        let zones = [];
        try {
            zones = await apiRequest('/api/zones');
        } catch {
            zones = [];
        }
        
        const totalMonitors = Array.isArray(monitors) ? monitors.length : 0;
        const runtimeMonitors = status?.monitors || [];
        const healthyMonitors = runtimeMonitors.filter(m => m.status === 'Normal').length;
        const downMonitors = runtimeMonitors.filter(m => m.status === 'Down').length;
        
        document.getElementById('stat-total').textContent = totalMonitors;
        document.getElementById('stat-healthy').textContent = healthyMonitors;
        document.getElementById('stat-down').textContent = downMonitors;
        document.getElementById('stat-zones').textContent = Array.isArray(zones) ? zones.length : 0;
        
        if (status?.system) {
            const memAlloc = status.system.mem_alloc || 0;

            const gorEl = document.getElementById('stat-goroutines');
            if (gorEl) gorEl.textContent = status.system.goroutines || 0;

            const memEl = document.getElementById('stat-memory');
            if (memEl) memEl.textContent = `${Math.round(memAlloc / 1024 / 1024)} MB`;
        }
        
        updateMonitorList(monitors, runtimeMonitors);
        updateSystemLogs(status?.history || []);
        
        updateGlobalStatus(healthyMonitors, downMonitors);
        
    } catch (error) {
        console.error('加载仪表板数据失败:', error);
        showNotification('加载数据失败，请检查后端服务', 'error');
    }
}

function updateMonitorList(monitors, runtimeMonitors) {
    const container = document.getElementById('dashboard-monitor-list');
    if (!container) return;

    const runtimeById = new Map((runtimeMonitors || []).map(m => [m.id, m]));
    
    if (!Array.isArray(monitors) || monitors.length === 0) {
        container.innerHTML = `
            <div class="text-center py-8 text-gray-500">
                <svg class="w-12 h-12 mx-auto mb-3 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                </svg>
                <p>暂无监控任务</p>
            </div>
        `;
        return;
    }
    
    container.innerHTML = monitors.slice(0, 5).map(monitor => {
        const runtime = runtimeById.get(monitor.id);
        const isDown = runtime?.status === 'Down';
        const statusClass = isDown ? 'status-error' : 'status-normal';
        const statusText = isDown ? '故障' : '正常';
        const target = monitor.target || monitor.original_ip || '';
        
        return `
            <div class="flex items-center justify-between p-4 bg-white rounded-lg border border-gray-200">
                <div class="flex items-center gap-3">
                    <div class="w-3 h-3 rounded-full ${isDown ? 'bg-red-500' : 'bg-green-500'}"></div>
                    <div>
                        <h5 class="font-medium text-gray-800">${monitor.name || '(未命名)'}</h5>
                        <p class="text-xs text-gray-500">${target}</p>
                    </div>
                </div>
                <span class="status-badge ${statusClass}">${statusText}</span>
            </div>
        `;
    }).join('');
}

function updateGlobalStatus(healthy, down) {
    const statusEl = document.getElementById('global-status');
    if (!statusEl) return;
    
    if (down > 0) {
        statusEl.className = 'status-badge status-error';
        statusEl.textContent = `${down}个故障`;
    } else if (healthy > 0) {
        statusEl.className = 'status-badge status-normal';
        statusEl.textContent = '系统运行正常';
    } else {
        statusEl.className = 'status-badge status-warning';
        statusEl.textContent = '无监控任务';
    }
}

function updateSystemLogs(history) {
    const container = document.getElementById('system-logs');
    if (!container) return;

    if (!Array.isArray(history) || history.length === 0) {
        container.innerHTML = `
            <div class="flex items-center gap-3 p-3 bg-gray-50 rounded-lg">
                <div class="w-2 h-2 rounded-full bg-blue-500"></div>
                <span class="text-sm text-gray-600">暂无切换事件</span>
            </div>
        `;
        return;
    }

    container.innerHTML = history.slice(0, 50).map(evt => {
        const timeStr = new Date(evt.timestamp).toLocaleString('zh-CN');
        const badge = evt.to_backup
            ? '<span class="text-xs px-2 py-1 rounded-full bg-orange-100 text-orange-800">切到备IP</span>'
            : '<span class="text-xs px-2 py-1 rounded-full bg-green-100 text-green-800">切回主IP</span>';

        return `
            <div class="p-3 bg-gray-50 rounded-lg space-y-1">
                <div class="flex items-center justify-between gap-3">
                    <div class="text-sm font-medium text-gray-800">${evt.name || evt.monitor_id}</div>
                    ${badge}
                </div>
                <div class="text-xs text-gray-600">${evt.from_ip} → ${evt.to_ip}</div>
                <div class="text-xs text-gray-500">${timeStr} · ${evt.type || ''}</div>
            </div>
        `;
    }).join('');
}