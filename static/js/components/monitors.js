import { state } from '../state.js';
import { apiRequest } from '../api.js';
import { showNotification } from './notifications.js';

export async function fetchMonitors() {
    try {
        const [monitors, status] = await Promise.all([
            apiRequest('/api/monitors'),
            apiRequest('/api/status')
        ]);
        const runtimeById = new Map((status?.monitors || []).map(m => [m.id, m]));
        const viewModels = (Array.isArray(monitors) ? monitors : []).map(m => ({
            ...m,
            runtime: runtimeById.get(m.id) || null
        }));
        state.monitorsCache = viewModels;
        renderMonitors(viewModels);
        renderOfflineHot(status?.offline_hot || []);
    } catch (error) {
        console.error('获取监控策略失败:', error);
        showNotification('获取监控策略失败', 'error');
    }
}

export function editMonitor(monitorId) {
    const monitor = state.monitorsCache.find(m => m.id == monitorId);
    if (monitor) {
        openMonitorModal(monitor);
    }
}

function renderOfflineHot(items) {
    const container = document.getElementById('strategy-offline-hot');
    if (!container) return;

    if (!Array.isArray(items) || items.length === 0) {
        container.innerHTML = `
            <div class="p-4 bg-gray-50 rounded-lg border border-gray-200 text-sm text-gray-600">
                暂无需要关注的策略
            </div>
        `;
        return;
    }

    container.innerHTML = items.slice(0, 10).map(it => {
        const name = it.name || it.monitor_id || '-';
        const ip = it.ip || '-';
        const role = it.role === 'backup' ? '备' : '主';
        const count = Number(it.count) || 0;
        const lastAt = it.last_at ? new Date(it.last_at).toLocaleString('zh-CN') : '';

        return `
            <div class="p-4 rounded-lg border border-red-200 bg-red-50 space-y-1">
                <div class="flex items-center justify-between gap-3">
                    <div class="font-medium text-gray-800 truncate">${name}</div>
                    <span class="text-xs px-2 py-1 rounded-full bg-red-100 text-red-800">掉线 ${count} 次</span>
                </div>
                <div class="text-xs text-gray-700">${role}IP：<span class="font-mono">${ip}</span></div>
                ${lastAt ? `<div class="text-xs text-gray-500">最近：${lastAt}</div>` : ''}
            </div>
        `;
    }).join('');
}

export async function deleteMonitor(monitorId) {
    const monitor = state.monitorsCache.find(m => m.id == monitorId);
    const monitorName = monitor ? monitor.name : 'this monitor';

    if (!confirm(`确定要删除监控策略 [${monitorName}] 吗？`)) {
        return;
    }

    try {
        await apiRequest(`/api/monitors/${monitorId}`, { method: 'DELETE' });
        showNotification('删除成功', 'success');
        await fetchMonitors();
    } catch (error) {
        console.error('删除监控策略失败:', error);
        showNotification('删除失败', 'error');
    }
}

function enhanceMonitorActionBar(container) {
    if (!container) return;

    const editButtons = container.querySelectorAll('button[onclick^="dnsManager.editMonitor("]');
    editButtons.forEach(editBtn => {
        const parent = editBtn.parentElement;
        if (!parent) return;

        const deleteBtn = parent.querySelector('button[onclick^="dnsManager.deleteMonitor("]');
        if (!deleteBtn) return;

        parent.className = 'flex items-center justify-between gap-3 flex-wrap';

        editBtn.classList.remove('flex-1');
        editBtn.classList.add('py-2', 'px-3', 'text-sm');

        const restoreBtn = parent.querySelector('button[onclick^="dnsManager.openRestoreModal("]');
        if (restoreBtn) {
            restoreBtn.classList.remove('flex-1');
            restoreBtn.classList.add('py-2', 'px-3', 'text-sm');
        }

        deleteBtn.classList.add('text-sm', 'px-3', 'py-2');

        const match = (editBtn.getAttribute('onclick') || '').match(/'([^']+)'/);
        const monitorId = match ? match[1] : null;
        if (!monitorId) return;

        if (!parent.querySelector('button[onclick^="dnsManager.openScheduleSwitchModal("]')) {
            const group = document.createElement('div');
            group.className = 'flex items-center gap-2';

            group.appendChild(editBtn);
            if (restoreBtn) group.appendChild(restoreBtn);

            const scheduleBtn = document.createElement('button');
            scheduleBtn.className = 'btn-secondary py-2 px-3 text-sm';
            scheduleBtn.textContent = '定时切换';
            scheduleBtn.setAttribute('onclick', `dnsManager.openScheduleSwitchModal('${monitorId}')`);
            group.insertBefore(scheduleBtn, restoreBtn || null);

            parent.insertBefore(group, deleteBtn);
        }
    });
}

function renderMonitors(monitors) {
    const container = document.getElementById('strategy-list');
    if (!container) return;
    
    if (!Array.isArray(monitors) || monitors.length === 0) {
        container.innerHTML = `
            <div class="text-center py-12">
                <svg class="w-16 h-16 mx-auto mb-4 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                </svg>
                <h3 class="text-lg font-medium text-gray-700 mb-2">暂无监控策略</h3>
                <p class="text-gray-500 mb-4">创建您的第一个监控策略</p>
                <button onclick="dnsManager.openMonitorModal()" class="btn-primary">
                    创建策略
                </button>
            </div>
        `;
        return;
    }
    
    container.innerHTML = monitors.map(monitor => {
        const runtime = monitor.runtime || {};
        const isDown = runtime.status === 'Down';
        const isRestoring = runtime.status === 'Restoring';

        let statusText, statusClass, statusDescription;
        if (isDown) {
            statusText = '服务故障';
            statusClass = 'status-error';
            statusDescription = `当前由备用 IP (${monitor.backup_ip}) 提供服务`;
        } else if (isRestoring) {
            statusText = '恢复中';
            statusClass = 'status-warning';
            statusDescription = `主 IP (${monitor.original_ip}) 连续成功 ${runtime.succ_count || 0}/${monitor.success_threshold} 次`;
        } else {
            statusText = '服务正常';
            statusClass = 'status-normal';
            statusDescription = `当前由主 IP (${monitor.original_ip}) 提供服务`;
        }

        const statusIcon = isDown 
            ? '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path></svg>'
            : '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path></svg>';

        const checkType = monitor.type || 'ping';
        const checkTypeText = {
            'ping': 'Ping检测',
            'http': 'HTTP检测',
            'https': 'HTTPS检测'
        }[checkType] || checkType;

        const checkTarget = monitor.target || (checkType === 'ping' ? (monitor.original_ip || '') : '');
        const subdomains = monitor.cf_domain || 'N/A';

        const lastCheckTime = monitor.runtime?.last_check ? new Date(monitor.runtime.last_check).toLocaleString('zh-CN') : '从未';
        const nextCheckTime = monitor.runtime?.last_check ? new Date(new Date(monitor.runtime.last_check).getTime() + (monitor.interval * 1000)).toLocaleString('zh-CN') : '待定';
        
        const scheduleInfo = monitor.schedule_enabled && monitor.schedule_hours > 0
            ? `<div class="flex items-center gap-2 text-xs text-blue-600">
                <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                </svg>
                <span>定时切换：每${monitor.schedule_hours}小时</span>
               </div>`
            : '';

        return `
            <div class="glass-card p-6 hover:shadow-xl transition-all duration-300">
                <div class="flex items-start justify-between mb-2">
                    <div class="flex-1">
                        <div class="flex items-center gap-3 mb-2">
                            <h3 class="font-bold text-gray-800 text-lg">${monitor.name || '(未命名)'}</h3>
                        </div>
                        <div class="p-3 rounded-lg border ${isDown ? 'border-red-200 bg-red-50' : 'border-green-200 bg-green-50'}">
                            <div class="flex items-center gap-2 mb-1">
                                <span class="status-badge ${statusClass} flex items-center gap-1">${statusIcon} ${statusText}</span>
                            </div>
                            <p class="text-sm text-gray-700 font-medium">${statusDescription}</p>
                        </div>
                    </div>
                </div>

                <div class="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
                    <div class="bg-gradient-to-br from-blue-50 to-blue-100 p-3 rounded-lg border border-blue-200">
                        <p class="text-xs text-blue-600 mb-1 font-medium">检测类型</p>
                        <p class="font-semibold text-gray-800 text-sm">${checkTypeText}</p>
                    </div>
                    <div class="bg-gradient-to-br from-purple-50 to-purple-100 p-3 rounded-lg border border-purple-200">
                        <p class="text-xs text-purple-600 mb-1 font-medium">检测间隔</p>
                        <p class="font-semibold text-gray-800 text-sm">${monitor.interval || 60}秒</p>
                    </div>
                    <div class="bg-gradient-to-br from-green-50 to-green-100 p-3 rounded-lg border border-green-200">
                        <p class="text-xs text-green-600 mb-1 font-medium">主 IP</p>
                        <p class="font-semibold text-gray-800 text-sm font-mono">${monitor.original_ip || 'N/A'}</p>
                    </div>
                    <div class="bg-gradient-to-br from-orange-50 to-orange-100 p-3 rounded-lg border border-orange-200">
                        <p class="text-xs text-orange-600 mb-1 font-medium">备 IP</p>
                        <p class="font-semibold text-gray-800 text-sm font-mono">${monitor.backup_ip || 'N/A'}</p>
                    </div>
                </div>

                <div class="grid grid-cols-1 md:grid-cols-2 gap-3 mb-4">
                    <div class="bg-gray-50 p-3 rounded-lg border border-gray-200">
                        <p class="text-xs text-gray-600 mb-1 font-medium">检测目标</p>
                        <p class="font-medium text-gray-800 text-sm break-all">${checkTarget || 'N/A'}</p>
                    </div>
                    <div class="bg-gray-50 p-3 rounded-lg border border-gray-200">
                        <p class="text-xs text-gray-600 mb-1 font-medium">子域名</p>
                        <p class="font-medium text-gray-800 text-sm break-all">${subdomains}</p>
                    </div>
                </div>

                <div class="mt-4 pt-3 border-t border-gray-200 text-xs text-gray-500 flex justify-between">
                    <span>最后检测: ${lastCheckTime}</span>
                    <span>下次检测: ${nextCheckTime}</span>
                </div>

                <div class="flex items-center gap-2 pt-3 border-t border-gray-200">
                    <button onclick="dnsManager.editMonitor('${monitor.id}')" 
                            class="flex items-center gap-1 px-3 py-2 text-sm font-medium text-blue-600 hover:text-blue-800 hover:bg-blue-50 rounded-lg transition-colors">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"></path>
                        </svg>
                        编辑
                    </button>
                    <button onclick="dnsManager.openScheduleSwitchModal('${monitor.id}')" 
                            class="flex items-center gap-1 px-3 py-2 text-sm font-medium text-purple-600 hover:text-purple-800 hover:bg-purple-50 rounded-lg transition-colors">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                        </svg>
                        定时
                    </button>
                    ${isDown ? `
                    <button onclick="dnsManager.openRestoreModal('${monitor.id}')" 
                            class="flex items-center gap-1 px-3 py-2 text-sm font-medium text-white bg-green-600 hover:bg-green-700 rounded-lg transition-colors">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path>
                        </svg>
                        恢复
                    </button>
                    ` : ''}
                    <div class="flex-1"></div>
                    <button onclick="dnsManager.deleteMonitor('${monitor.id}')" 
                            class="flex items-center gap-1 px-3 py-2 text-sm font-medium text-red-600 hover:text-red-800 hover:bg-red-50 rounded-lg transition-colors">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path>
                        </svg>
                        删除
                    </button>
                </div>
            </div>
        `;
    }).join('');

    enhanceMonitorActionBar(container);
}