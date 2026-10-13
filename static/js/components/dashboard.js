import { state } from '../state.js';
import { apiRequest } from '../api.js';
import { escapeHTML, formatTime, monitorStatus } from '../format.js';
import { showNotification } from './notifications.js';

export async function loadDashboardData() {
    try {
        const status = await apiRequest('/api/status');
        const monitors = status.monitors || [];
        state.monitorsCache = monitors;
        const healthy = monitors.filter(m => m.status === 'Normal').length;
        const attention = monitors.length - healthy;
        document.getElementById('stat-total').textContent = monitors.length;
        document.getElementById('stat-healthy').textContent = healthy;
        document.getElementById('stat-down').textContent = attention;
        const goroutines = document.getElementById('stat-goroutines');
        const memory = document.getElementById('stat-memory');
        if (goroutines) goroutines.textContent = status.system?.goroutines || 0;
        if (memory) memory.textContent = `${Math.round((status.system?.mem_alloc || 0) / 1024 / 1024)} MiB`;

        const global = document.getElementById('global-status');
        global.className = `status-badge ${attention || !monitors.length ? 'status-warning' : 'status-normal'}`;
        global.textContent = attention ? `${attention} 个策略需关注` : monitors.length ? '主备服务正常' : '无监控策略';

        document.getElementById('dashboard-monitor-list').innerHTML = monitors.length ? monitors.slice(0, 5).map(m => {
            const view = monitorStatus(m);
            return `<div class="flex items-center justify-between gap-3 p-4 bg-white rounded-lg border border-gray-200">
                <div><h5 class="font-medium text-gray-800">${escapeHTML(m.name)}</h5>
                <p class="text-xs text-gray-500 break-all">DNS：${escapeHTML(m.current_ip || '等待确认')}</p></div>
                <span class="status-badge ${view.className}">${view.label}</span>
            </div>`;
        }).join('') : '<p class="text-center py-8 text-gray-500">暂无监控策略</p>';

        const history = status.history || [];
        document.getElementById('system-logs').innerHTML = history.length ? history.map(event => `
            <div class="p-3 bg-gray-50 rounded-lg space-y-1">
                <div class="font-medium text-sm text-gray-800">${escapeHTML(event.name)}</div>
                <div class="text-xs text-gray-600 break-all">${escapeHTML(event.from_ip)} → ${escapeHTML(event.to_ip)}</div>
                <div class="text-xs text-gray-500">${formatTime(event.timestamp)} · ${escapeHTML(event.type)}</div>
            </div>`).join('') : '<p class="p-3 text-sm text-gray-500">暂无切换事件</p>';
    } catch (error) {
        const global = document.getElementById('global-status');
        if (global) {
            global.className = 'status-badge status-error';
            global.textContent = '状态刷新失败';
        }
        showNotification(`加载监控状态失败：${error.message}`, 'error');
    }
}
