import { state } from '../state.js';
import { apiRequest } from '../api.js';
import { showNotification } from './notifications.js';
import { openAccountModal } from './modals.js';
import { fetchZones } from './domains.js';

export async function loadSettings() {
    try {
        const config = await apiRequest('/api/config');
        
        if (config.cloudflare) {
            document.getElementById('set-cf-token').value = config.cloudflare.api_token || '';
        }
        
        if (config.dingtalk) {
            document.getElementById('set-ding-enabled').checked = config.dingtalk.enabled || false;
            document.getElementById('set-ding-token').value = config.dingtalk.access_token || '';
            document.getElementById('set-ding-secret').value = config.dingtalk.secret || '';
        }

        if (config.email) {
            document.getElementById('set-email-enabled').checked = config.email.enabled || false;
            document.getElementById('set-email-host').value = config.email.host || '';
            document.getElementById('set-email-port').value = config.email.port || '';
            document.getElementById('set-email-username').value = config.email.username || '';
            document.getElementById('set-email-password').value = config.email.password || '';
            document.getElementById('set-email-to').value = config.email.to || '';
        }

        if (config.telegram) {
            document.getElementById('set-tg-enabled').checked = config.telegram.enabled || false;
            document.getElementById('set-tg-bot-token').value = config.telegram.bot_token || '';
            document.getElementById('set-tg-chat-id').value = config.telegram.chat_id || '';
        }
        
        await loadCloudflareAccounts();
        
    } catch (error) {
        console.error('加载设置失败:', error);
    }
}

export async function loadCloudflareAccounts() {
    try {
        const data = await apiRequest('/api/cloudflare-accounts');
        renderCloudflareAccounts(data.accounts || [], data.active_index || 0);
    } catch (error) {
        console.error('加载Cloudflare凭证失败:', error);
    }
}

function renderCloudflareAccounts(accounts, activeIndex) {
    const container = document.getElementById('cf-accounts-list');
    if (!container) return;

    if (!Array.isArray(accounts) || accounts.length === 0) {
        container.innerHTML = `
            <div class="text-center py-8 text-gray-500">
                <p>暂无凭证，点击下方按钮添加</p>
            </div>
        `;
        return;
    }

    container.innerHTML = accounts.map((account, index) => {
        const isActive = index === activeIndex;
        return `
            <div class="p-4 rounded-lg border ${isActive ? 'border-blue-500 bg-blue-50' : 'border-gray-200 bg-white'}">
                <div class="flex items-center justify-between mb-2">
                    <div class="flex items-center gap-2">
                        <h4 class="font-medium text-gray-800">${account.name || '(未命名)'}</h4>
                        ${isActive ? '<span class="px-2 py-1 text-xs rounded-full bg-blue-500 text-white">当前使用</span>' : ''}
                    </div>
                    <div class="flex gap-2">
                        ${!isActive ? `<button onclick="dnsManager.activateAccount('${account.id}')" class="text-sm text-blue-600 hover:text-blue-800">激活</button>` : ''}
                        <button onclick="dnsManager.editAccount('${account.id}')" class="text-sm text-gray-600 hover:text-gray-800">编辑</button>
                        <button onclick="dnsManager.deleteAccount('${account.id}')" class="text-sm text-red-600 hover:text-red-800">删除</button>
                    </div>
                </div>
                <div class="text-xs text-gray-500">
                    Token: ${account.api_token ? '••••••' : '(未设置)'}
                </div>
                ${account.email ? `
                <div class="text-xs text-gray-500 mt-1">
                    旧版凭证: ${account.email} / ••••••
                </div>
                ` : ''}
            </div>
        `;
    }).join('');
}

export async function editAccount(accountId) {
    try {
        const data = await apiRequest('/api/cloudflare-accounts');
        const account = (data.accounts || []).find(a => a.id === accountId);
        if (!account) {
            showNotification('未找到该凭证', 'error');
            return;
        }
        openAccountModal(account);
    } catch (error) {
        console.error('加载凭证失败:', error);
        showNotification('加载凭证失败', 'error');
    }
}

export async function deleteAccount(accountId) {
    if (!confirm('确定要删除这个凭证吗？')) return;
    try {
        await apiRequest(`/api/cloudflare-accounts/${accountId}`, { method: 'DELETE' });
        showNotification('删除成功', 'success');
        await loadCloudflareAccounts();
    } catch (error) {
        console.error('删除凭证失败:', error);
        showNotification('删除失败', 'error');
    }
}

export async function activateAccount(accountId) {
    try {
        await apiRequest(`/api/cloudflare-accounts/${accountId}/activate`, { method: 'POST' });
        showNotification('凭证已激活', 'success');
        await loadCloudflareAccounts();
        await fetchZones();
    } catch (error) {
        console.error('激活凭证失败:', error);
        showNotification('激活失败', 'error');
    }
}

export async function switchAccount(accountId) {
    if (!accountId) return;
    try {
        await apiRequest(`/api/cloudflare-accounts/${accountId}/activate`, { method: 'POST' });
        showNotification('凭证已切换', 'success');
        await updateAccountSwitcher();
        await fetchZones();
    } catch (error) {
        console.error('切换凭证失败:', error);
        showNotification('切换失败', 'error');
    }
}

export async function updateAccountSwitcher() {
    try {
        const data = await apiRequest('/api/cloudflare-accounts');
        const accounts = data.accounts || [];
        const activeIndex = data.active_index || 0;
        
        const currentNameEl = document.getElementById('current-account-name');
        if (currentNameEl) {
            const currentName = accounts[activeIndex]?.name || '默认凭证';
            currentNameEl.textContent = currentName;
        }
        
        const switcher = document.getElementById('account-switcher');
        if (switcher) {
            switcher.innerHTML = '<option value="">切换凭证...</option>' +
                accounts.map((acc, idx) => 
                    `<option value="${acc.id}" ${idx === activeIndex ? 'disabled' : ''}>${acc.name}${idx === activeIndex ? ' (当前)' : ''}</option>`
                ).join('');
        }
    } catch (error) {
        console.error('更新凭证切换器失败:', error);
    }
}

export async function saveSettings() {
    const config = {
        cloudflare: {
            api_token: document.getElementById('set-cf-token').value
        },
        dingtalk: {
            enabled: document.getElementById('set-ding-enabled').checked,
            access_token: document.getElementById('set-ding-token').value,
            secret: document.getElementById('set-ding-secret').value
        },
        email: {
            enabled: document.getElementById('set-email-enabled').checked,
            host: document.getElementById('set-email-host').value,
            port: Number(document.getElementById('set-email-port').value) || 0,
            username: document.getElementById('set-email-username').value,
            password: document.getElementById('set-email-password').value,
            to: document.getElementById('set-email-to').value
        },
        telegram: {
            enabled: document.getElementById('set-tg-enabled').checked,
            bot_token: document.getElementById('set-tg-bot-token').value,
            chat_id: document.getElementById('set-tg-chat-id').value
        }
    };
    
    try {
        const response = await fetch(`${state.baseURL}/api/config`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(config)
        });
        
        if (response.ok) {
            showNotification('设置保存成功', 'success');
        } else {
            let msg = `保存失败: ${response.status}`;
            try {
                const payload = await response.json();
                msg = payload?.msg || payload?.message || msg;
            } catch {
                // ignore
            }
            showNotification(msg, 'error');
        }
    } catch (error) {
        console.error('保存设置失败:', error);
        showNotification(`保存失败: ${error.message || error}`, 'error');
    }
}