import { state } from '../state.js';
import { apiRequest } from '../api.js';
import { escapeHTML, formatTime, healthLabel, monitorStatus } from '../format.js';
import { showNotification } from './notifications.js';
import { openMonitorModal } from './modals.js';

export async function fetchMonitors() {
    try {
        const status = await apiRequest('/api/status');
        state.monitorsCache = status.monitors || [];
        renderMonitors(state.monitorsCache);
        renderOfflineHot(status.offline_hot || []);
    } catch (error) {
        showNotification(`获取监控策略失败：${error.message}`, 'error');
    }
}

export function editMonitor(monitorId) {
    const monitor = state.monitorsCache.find(m => m.id == monitorId);
    if (monitor) openMonitorModal(monitor);
}

export async function deleteMonitor(monitorId) {
    const monitor = state.monitorsCache.find(m => m.id == monitorId);
    if (!confirm(`确定删除监控策略「${monitor?.name || monitorId}」吗？`)) return;
    try {
        await apiRequest(`/api/monitors/${monitorId}`, { method: 'DELETE' });
        showNotification('删除成功', 'success');
        await fetchMonitors();
    } catch (error) {
        showNotification(error.message, 'error');
    }
}

function renderOfflineHot(items) {
    const container = document.getElementById('strategy-offline-hot');
    if (!container) return;
    container.innerHTML = items.length ? items.map(item => `
        <div class="p-4 rounded-lg border border-red-200 bg-red-50 space-y-1">
            <div class="font-medium text-gray-800">${escapeHTML(item.name)}</div>
            <div class="text-xs text-gray-700">${item.role === 'backup' ? '备' : '主'}地址 ${escapeHTML(item.ip)}：故障切出 ${Number(item.count)} 次</div>
            <div class="text-xs text-gray-500">最近：${formatTime(item.last_at)}</div>
        </div>`).join('') : '<p class="p-4 text-sm text-gray-600">暂无需要关注的频繁故障</p>';
}

function renderMonitors(monitors) {
    const container = document.getElementById('strategy-list');
    if (!container) return;
    if (!monitors.length) {
        container.innerHTML = '<div class="text-center py-12"><p class="mb-4 text-gray-600">暂无监控策略</p><button onclick="dnsManager.openMonitorModal()" class="btn-primary">创建策略</button></div>';
        return;
    }
    container.innerHTML = monitors.map(monitor => {
        const status = monitorStatus(monitor);
        const id = Number(monitor.id);
        const fields = [
            ['主地址', `${monitor.original_ip} · ${healthLabel(monitor.primary_healthy)}`],
            ['备用地址', `${monitor.backup_ip} · ${healthLabel(monitor.backup_healthy)}`],
            ['检测方式', `${monitor.type.toUpperCase()} · 每 ${monitor.interval} 秒`],
            ['检测目标', monitor.target],
            ['DNS 名称', monitor.cf_domain],
            ['优先地址', monitor.preferred_ip || monitor.original_ip]
        ];
        const errors = [monitor.last_error, monitor.primary_error && `主地址：${monitor.primary_error}`, monitor.backup_error && `备用地址：${monitor.backup_error}`].filter(Boolean);
        return `<div class="glass-card p-6 space-y-4">
            <div class="flex items-center justify-between gap-3">
                <h3 class="font-bold text-gray-800 text-lg">${escapeHTML(monitor.name)}</h3>
                <span class="status-badge ${status.className}">${status.label}</span>
            </div>
            <div class="p-3 rounded-lg border border-blue-200 bg-blue-50 text-sm">
                当前 DNS 地址：<strong class="font-mono break-all">${escapeHTML(monitor.current_ip || '等待 Cloudflare 确认')}</strong>
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                ${fields.map(([label, value]) => `<div class="bg-gray-50 rounded-lg p-3"><p class="text-xs text-gray-500 mb-1">${label}</p><p class="text-sm text-gray-800 break-all">${escapeHTML(value)}</p></div>`).join('')}
            </div>
            ${errors.length ? `<div class="p-3 rounded-lg bg-red-50 text-red-700 text-xs break-all" role="status">${errors.map(escapeHTML).join('<br>')}</div>` : ''}
            <div class="text-xs text-gray-500 space-y-1">
                <p>当前地址连续失败 ${monitor.fail_count}/${monitor.retries} · 优先地址连续成功 ${monitor.succ_count}/${monitor.success_threshold}</p>
                <p>最后检测：${formatTime(monitor.last_check)} · 最近切换：${formatTime(monitor.last_switch)}</p>
                ${(monitor.schedules || []).map(s => `<p>计划：${escapeHTML(s.cron)} → ${escapeHTML(s.target_ip)}</p>`).join('')}
            </div>
            <div class="flex flex-wrap items-center gap-2 pt-3 border-t border-gray-200">
                <button class="btn-secondary text-sm" onclick="dnsManager.editMonitor(${id})">编辑</button>
                <button class="btn-secondary text-sm" onclick="dnsManager.openScheduleSwitchModal(${id})">定时切换</button>
                ${monitor.current_ip !== monitor.original_ip ? `<button class="btn-secondary text-sm" onclick="dnsManager.openRestoreModal(${id})">恢复主地址</button>` : ''}
                <button class="text-sm text-red-600 ml-auto" onclick="dnsManager.deleteMonitor(${id})">删除</button>
            </div>
        </div>`;
    }).join('');
}
