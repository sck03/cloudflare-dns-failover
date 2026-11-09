import { apiRequest } from '../api.js';
import { LatestRequest } from '../requests.js';
import { escapeHTML } from '../format.js';
import { showNotification } from './notifications.js';
import { openAccountModal } from './modals.js';
import { fetchZones, resetDomains } from './domains.js';

const accountRequest = new LatestRequest();
let accountsCache = [];

// One response updates every account view. Slow reads cannot restore old state
// after a mutation, and editing uses the same snapshot as the displayed list.
export async function loadAccounts() {
    try {
        await accountRequest.run('/api/cloudflare-accounts', data => {
            accountsCache = data.accounts || [];
            const activeIndex = data.active_index || 0;
            renderCloudflareAccounts(accountsCache, activeIndex);
            renderAccountSwitcher(accountsCache, activeIndex);
        });
    } catch (error) {
        showNotification(`加载账号失败：${error.message}`, 'error');
    }
}

export async function refreshAccountViews() {
    resetDomains();
    await Promise.all([loadAccounts(), fetchZones()]);
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
                        <h4 class="font-medium text-gray-800">${escapeHTML(account.name || '(未命名)')}</h4>
                        ${isActive ? '<span class="px-2 py-1 text-xs rounded-full bg-blue-500 text-white">当前使用</span>' : ''}
                    </div>
                    <div class="flex gap-2">
                        ${!isActive ? `<button data-account-action="activate" data-account-id="${escapeHTML(account.id)}" class="text-sm text-blue-600 hover:text-blue-800">激活</button>` : ''}
                        <button data-account-action="edit" data-account-id="${escapeHTML(account.id)}" class="text-sm text-gray-600 hover:text-gray-800">编辑</button>
                        <button data-account-action="delete" data-account-id="${escapeHTML(account.id)}" class="text-sm text-red-600 hover:text-red-800">删除</button>
                    </div>
                </div>
                <div class="text-xs text-gray-500">
                    Token: ${account.has_api_token ? '已设置' : '(未设置)'}
                </div>
                ${account.has_api_key ? `
                <div class="text-xs text-gray-500 mt-1">
                    旧版凭证: ${escapeHTML(account.email)} / ••••••
                </div>
                ` : ''}
            </div>
        `;
    }).join('');
    container.onclick = event => {
        const button = event.target.closest('[data-account-action]');
        if (!button) return;
        const actions = { activate: activateAccount, edit: editAccount, delete: deleteAccount };
        actions[button.dataset.accountAction]?.(button.dataset.accountId);
    };
}

function renderAccountSwitcher(accounts, activeIndex) {
    const name = document.getElementById('current-account-name');
    if (name) name.textContent = accounts[activeIndex]?.name || '未配置凭证';
    const switcher = document.getElementById('account-switcher');
    if (switcher) {
        switcher.innerHTML = '<option value="">切换凭证...</option>' +
            accounts.map((account, index) =>
                `<option value="${escapeHTML(account.id)}" ${index === activeIndex ? 'disabled' : ''}>${escapeHTML(account.name)}${index === activeIndex ? ' (当前)' : ''}</option>`
            ).join('');
    }
}

export function editAccount(accountId) {
    const account = accountsCache.find(account => account.id === accountId);
    if (account) openAccountModal(account);
    else showNotification('未找到该凭证，请刷新账号列表', 'error');
}

export async function deleteAccount(accountId) {
    if (!confirm('确定要删除这个凭证吗？')) return;
    try {
        await apiRequest(`/api/cloudflare-accounts/${encodeURIComponent(accountId)}`, { method: 'DELETE' });
        showNotification('删除成功', 'success');
        await refreshAccountViews();
    } catch (error) {
        showNotification(`删除失败：${error.message}`, 'error');
    }
}

export async function activateAccount(accountId) {
    if (!accountId) return;
    try {
        await apiRequest(`/api/cloudflare-accounts/${encodeURIComponent(accountId)}/activate`, { method: 'POST' });
        showNotification('凭证已激活', 'success');
        await refreshAccountViews();
    } catch (error) {
        showNotification(`激活失败：${error.message}`, 'error');
    }
}
