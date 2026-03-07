import { state } from '../state.js';
import { apiRequest } from '../api.js';
import { showNotification } from './notifications.js';

export async function fetchZones() {
    try {
        const zones = await apiRequest('/api/zones');
        renderZones(zones);
    } catch (error) {
        console.error('获取域名列表失败:', error);
        showNotification('获取域名列表失败', 'error');
    }
}

function renderZones(zones) {
    const container = document.getElementById('zone-list');
    if (!container) return;
    
    if (!zones || zones.length === 0) {
        container.innerHTML = `
            <div class="col-span-full text-center py-12">
                <svg class="w-16 h-16 mx-auto mb-4 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"></path>
                </svg>
                <h3 class="text-lg font-medium text-gray-700 mb-2">暂无域名</h3>
                <p class="text-gray-500 mb-4">请先配置Cloudflare凭证</p>
                <button onclick="dnsManager.switchSection('settings')" class="btn-primary">
                    前往设置
                </button>
            </div>
        `;
        return;
    }
    
    container.innerHTML = zones.map(zone => `
        <div class="glass-card p-6 hover:shadow-xl transition-all duration-300 border-l-4 ${zone.status === 'active' ? 'border-green-500' : 'border-gray-300'}">
            <div class="flex items-start justify-between mb-4">
                <div class="flex-1">
                    <div class="flex items-center gap-2 mb-2">
                        <div class="p-2 rounded-lg bg-gradient-to-br from-orange-50 to-orange-100">
                            <svg class="w-5 h-5 text-orange-600" viewBox="0 0 24 24" fill="currentColor">
                                <path d="M12.5 2L2 7.5v9L12.5 22l10.5-5.5v-9L12.5 2zm0 2.311L20.689 8.5 12.5 12.689 4.311 8.5 12.5 4.311zM4 10.311l7.5 3.939v7.439L4 17.75v-7.439zm9.5 11.378v-7.439l7.5-3.939v7.439l-7.5 3.939z"/>
                            </svg>
                        </div>
                        <h3 class="font-bold text-gray-800 text-lg">${zone.name}</h3>
                    </div>
                    <div class="flex items-center gap-2 flex-wrap">
                        <span class="flex items-center gap-1 px-3 py-1 text-xs rounded-full ${zone.status === 'active' ? 'bg-green-100 text-green-800 border border-green-200' : 'bg-gray-100 text-gray-800 border border-gray-200'}">
                            <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 20 20">
                                <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd"/>
                            </svg>
                            ${zone.status === 'active' ? '已激活' : '未激活'}
                        </span>
                        <span class="px-3 py-1 text-xs rounded-full bg-blue-100 text-blue-800 border border-blue-200" title="Cloudflare Zone 类型：full=全量接入；partial=仅DNS">
                            ${zone.type === 'full' ? '🌐 全量接入' : '📡 仅DNS'}
                        </span>
                    </div>
                </div>
            </div>
            
            <div class="space-y-3 mb-4">
                <div class="p-3 bg-gradient-to-r from-gray-50 to-gray-100 rounded-lg">
                    <div class="text-xs text-gray-500 mb-1">Zone ID</div>
                    <div class="font-mono text-xs text-gray-700 break-all">${zone.id}</div>
                </div>
                <div class="flex items-center justify-between text-sm">
                    <span class="flex items-center gap-1 text-gray-500">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"></path>
                        </svg>
                        创建时间
                    </span>
                    <span class="font-medium text-gray-700">${new Date(zone.created_on).toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })}</span>
                </div>
            </div>
            
            <div class="mt-6">
                <button onclick="dnsManager.viewRecords('${zone.id}', '${zone.name}')" 
                        class="w-full btn-primary flex items-center justify-center gap-2 py-3">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"></path>
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"></path>
                    </svg>
                    管理DNS记录
                </button>
            </div>
        </div>
    `).join('');
}

export async function viewRecords(zoneId, zoneName) {
    state.currentZoneId = zoneId;
    state.currentZoneName = zoneName;
    
    document.getElementById('zone-list').classList.add('hidden');
    document.getElementById('records-container').classList.remove('hidden');
    document.getElementById('current-zone-name').textContent = `${zoneName} - 解析记录`;
    
    await fetchRecords(zoneId);
}

export function hideRecords() {
    document.getElementById('zone-list').classList.remove('hidden');
    document.getElementById('records-container').classList.add('hidden');
    state.currentZoneId = null;
    state.currentZoneName = null;
}

export async function deleteRecord(recordId) {
    if (!confirm('确定要删除这条DNS记录吗？')) {
        return;
    }

    try {
        await apiRequest(`/api/zones/${state.currentZoneId}/records/${recordId}`, { method: 'DELETE' });
        showNotification('DNS记录删除成功', 'success');
        await fetchRecords(state.currentZoneId);
    } catch (error) {
        console.error('删除DNS记录失败:', error);
        showNotification('删除DNS记录失败', 'error');
    }
}

export async function fetchRecords(zoneId) {
    try {
        const records = await apiRequest(`/api/zones/${zoneId}/records`);
        state.recordsCache = Array.isArray(records) ? records : [];
        renderRecords(state.recordsCache);
    } catch (error) {
        console.error('获取DNS记录失败:', error);
        showNotification('获取DNS记录失败', 'error');
    }
}

function renderRecords(records) {
    const container = document.getElementById('records-list');
    if (!container) return;
    
    if (!records || records.length === 0) {
        container.innerHTML = `
            <tr>
                <td colspan="6" class="py-8 text-center text-gray-500">
                    <svg class="w-12 h-12 mx-auto mb-3 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                    </svg>
                    <p>暂无DNS记录</p>
                </td>
            </tr>
        `;
        return;
    }
    
    container.innerHTML = records.map(record => `
        <tr class="table-row">
            <td class="py-4 px-6">
                <span class="px-3 py-1 text-xs rounded-full bg-blue-100 text-blue-800 font-medium">
                    ${record.type}
                </span>
            </td>
            <td class="py-4 px-6 font-medium text-gray-800">${record.name}</td>
            <td class="py-4 px-6">
                <div class="max-w-xs truncate" title="${record.content}">
                    ${record.content}
                </div>
            </td>
            <td class="py-4 px-6">
                <span class="px-3 py-1 text-xs rounded-full ${record.proxied ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800'}">
                    ${record.proxied ? '已代理' : '未代理'}
                </span>
            </td>
            <td class="py-4 px-6 text-gray-600">${record.ttl}</td>
            <td class="py-4 px-6">
                <div class="flex gap-2">
                    <button onclick="dnsManager.editRecord('${record.id}')" 
                            class="text-blue-600 hover:text-blue-800 text-sm font-medium">
                        编辑
                    </button>
                    <button onclick="dnsManager.deleteRecord('${record.id}')" 
                            class="text-red-600 hover:text-red-800 text-sm font-medium">
                        删除
                    </button>
                </div>
            </td>
        </tr>
    `).join('');
}