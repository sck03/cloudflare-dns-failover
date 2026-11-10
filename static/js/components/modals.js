import { state } from '../state.js';
import { apiRequest } from '../api.js';
import { showNotification } from './notifications.js';
import { fetchRecords } from './domains.js';
import { fetchMonitors } from './monitors.js';
import { refreshAccountViews } from './accounts.js';
import { renderDashboard } from './dashboard.js';
import { LatestRequest } from '../requests.js';
import { showModal, hideModal, captureModal } from '../modal.js';

const monitorRequest = new LatestRequest();

// --- Record Modal --- //
export function openRecordModal() {
    if (!state.currentZoneId) {
        showNotification('请先选择一个域名再添加记录', 'warning');
        return;
    }
    state.editingRecordId = null;
    showRecordModal({
        type: 'A',
        name: '',
        content: '',
        ttl: 60,
        proxied: false
    });
}

export function editRecord(recordId) {
    const record = state.recordsCache.find(r => r.id === recordId);
    if (!record) {
        showNotification('未找到该记录', 'error');
        return;
    }
    state.editingRecordId = recordId;
    showRecordModal(record);
}

function showRecordModal(record) {
    const modal = document.getElementById('record-modal');
    if (!modal) return;

    document.getElementById('record-modal-title').textContent = state.editingRecordId ? '编辑DNS记录' : '添加DNS记录';
    document.getElementById('record-type').value = record.type || 'A';
    document.getElementById('record-name').value = record.name || '';
    document.getElementById('record-content').value = record.content || '';
    document.getElementById('record-ttl').value = record.ttl || 1;
    document.getElementById('record-proxied').checked = !!record.proxied;

    showModal('record-modal');
}

export function hideRecordModal() {
    hideModal('record-modal');
    state.editingRecordId = null;
}

export async function submitRecordForm() {
    const isCurrent = captureModal('record-modal');
    const zoneId = state.currentZoneId;
    const payload = {
        type: document.getElementById('record-type').value,
        name: document.getElementById('record-name').value.trim(),
        content: document.getElementById('record-content').value.trim(),
        ttl: Number(document.getElementById('record-ttl').value) || 1,
        proxied: document.getElementById('record-proxied').checked,
    };

    if (!payload.name || !payload.content) {
        throw new Error('名称和内容不能为空');
    }

    if (state.editingRecordId) {
        await apiRequest(`/api/zones/${state.currentZoneId}/records/${state.editingRecordId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
    } else {
        await apiRequest(`/api/zones/${state.currentZoneId}/records`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
    }

    if (isCurrent()) hideRecordModal();
    showNotification('保存成功', 'success');
    await fetchRecords(zoneId);
}

// --- Monitor Modal --- //
export async function openMonitorModal(monitor) {
    hideMonitorModal();
    const draft = monitor || {};
    try {
        await monitorRequest.run('/api/cloudflare-accounts', data => {
            state.editingMonitorId = monitor?.id ?? null;
            showMonitorModal(draft, data.accounts || []);
        });
    } catch (error) {
        showNotification(`加载账号列表失败：${error.message}`, 'error');
    }
}

function showMonitorModal(monitor, accounts) {
    const modal = document.getElementById('monitor-modal');
    if (!modal) {
        showNotification('缺少监控策略弹窗HTML', 'error');
        return;
    }

    document.getElementById('monitor-modal-title').textContent =
        state.editingMonitorId ? '编辑监控策略' : '创建监控策略';

    const accountSelect = document.getElementById('monitor-account');
    if (accountSelect) {
        accountSelect.innerHTML = '<option value="">选择 Cloudflare 账号...</option>';
        for (const account of accounts) {
            const option = document.createElement('option');
            option.value = option.textContent = account.name;
            option.selected = account.name === monitor.account_name;
            accountSelect.appendChild(option);
        }
    }

    document.getElementById('monitor-name').value = monitor.name || '';
    document.getElementById('monitor-zone-id').value = monitor.cf_zone_id || '';
    document.getElementById('monitor-domain').value = monitor.cf_domain || '';
    document.getElementById('monitor-dns-type').value = monitor.dns_type || 'A';
    document.getElementById('monitor-check-type').value = monitor.type || 'https';
    document.getElementById('monitor-check-target').value = monitor.target || '';
    document.getElementById('monitor-original-ip').value = monitor.original_ip || '';
    document.getElementById('monitor-backup-ip').value = monitor.backup_ip || '';

    document.getElementById('monitor-retries').value = monitor.retries ?? 3;
    document.getElementById('monitor-success-threshold').value = monitor.success_threshold ?? 2;
    document.getElementById('monitor-interval').value = monitor.interval ?? 60;
    document.getElementById('monitor-timeout-seconds').value = monitor.timeout ?? 5;
    document.getElementById('monitor-dns-ttl').value = monitor.dns_ttl ?? 60;
    document.getElementById('monitor-expected-status').value = monitor.expected_status ?? 0;
    document.getElementById('monitor-insecure-tls').checked = !!monitor.insecure_skip_verify;
    document.getElementById('monitor-ping-count').value = monitor.ping_count ?? 3;
    document.getElementById('monitor-original-cdn').checked = !!monitor.original_ip_cdn_enabled;
    document.getElementById('monitor-backup-cdn').checked = !!monitor.backup_ip_cdn_enabled;

    showModal('monitor-modal');
}

export function hideMonitorModal() {
    monitorRequest.cancel();
    state.editingMonitorId = null;
    hideModal('monitor-modal');
}

export async function submitMonitorForm() {
    const isCurrent = captureModal('monitor-modal');
    const checkType = document.getElementById('monitor-check-type').value;
    let checkTarget = document.getElementById('monitor-check-target').value.trim();
    if ((checkType === 'http' || checkType === 'https') && checkTarget && !/^https?:\/\//i.test(checkTarget)) {
        checkTarget = `${checkType}://${checkTarget}`;
    }

    const domain = document.getElementById('monitor-domain').value.trim();
    if (!domain) throw new Error('请填写子域名');

    const accountName = document.getElementById('monitor-account').value;
    if (!accountName) throw new Error('请选择所属账号');

    const payload = {
        name: document.getElementById('monitor-name').value.trim(),
        account_name: accountName,
        cf_zone_id: document.getElementById('monitor-zone-id').value.trim(),
        cf_domain: domain,
        dns_type: document.getElementById('monitor-dns-type').value,
        type: checkType,
        target: checkTarget,
        original_ip: document.getElementById('monitor-original-ip').value.trim(),
        backup_ip: document.getElementById('monitor-backup-ip').value.trim(),
        retries: Number(document.getElementById('monitor-retries').value) || 3,
        success_threshold: Number(document.getElementById('monitor-success-threshold').value) || 2,
        interval: Number(document.getElementById('monitor-interval').value) || 60,
        timeout: Number(document.getElementById('monitor-timeout-seconds').value) || 5,
        dns_ttl: Number(document.getElementById('monitor-dns-ttl').value),
        expected_status: Number(document.getElementById('monitor-expected-status').value),
        insecure_skip_verify: document.getElementById('monitor-insecure-tls').checked,
        ping_count: Number(document.getElementById('monitor-ping-count').value) || 3,
        original_ip_cdn_enabled: !!document.getElementById('monitor-original-cdn').checked,
        backup_ip_cdn_enabled: !!document.getElementById('monitor-backup-cdn').checked
    };

    if (!payload.name) throw new Error('请填写策略名称');
    if (!payload.cf_zone_id) throw new Error('请填写 Zone ID');
    if (!payload.original_ip) throw new Error('请填写主IP');
    if (!payload.backup_ip) throw new Error('请填写备IP');
    if ((payload.type === 'http' || payload.type === 'https') && !payload.target) {
        throw new Error('HTTP/HTTPS 检测需要填写检测目标(URL)');
    }

    if (state.editingMonitorId) {
        await apiRequest(`/api/monitors/${state.editingMonitorId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
    } else {
        await apiRequest('/api/monitors', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
    }

    if (isCurrent()) hideMonitorModal();
    showNotification('保存成功', 'success');
    await fetchMonitors(undefined, renderDashboard);
}

// --- Restore Modal --- //
export function openRestoreModal(monitorId) {
    const monitor = state.monitorsCache.find(m => m.id == monitorId);
    if (!monitor) {
        showNotification('未找到该策略', 'error');
        return;
    }

    state.restoreMonitorId = monitorId;
    const modal = document.getElementById('restore-modal');
    if (!modal) return;

    document.getElementById('restore-monitor-name').textContent = monitor.name;
    document.getElementById('restore-proxied').checked = !!monitor.original_ip_cdn_enabled;

    showModal('restore-modal');
}

export function hideRestoreModal() {
    hideModal('restore-modal');
    state.restoreMonitorId = null;
}

export async function confirmRestore() {
    if (!state.restoreMonitorId) return;
    const isCurrent = captureModal('restore-modal');
    const proxied = document.getElementById('restore-proxied').checked;
    await apiRequest(`/api/monitors/${state.restoreMonitorId}/restore`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ proxied })
    });
    if (isCurrent()) hideRestoreModal();
    showNotification('Cloudflare 已确认恢复到主地址', 'success');
    await fetchMonitors(undefined, renderDashboard);
}

// --- Schedule Modal --- //
export async function openScheduleSwitchModal(monitorId) {
    const monitor = state.monitorsCache.find(m => m.id == monitorId);
    if (!monitor) {
        showNotification('未找到该策略', 'error');
        return;
    }

    state.scheduleMonitorId = monitorId;
    const modal = document.getElementById('schedule-modal');
    if (!modal) return;

    document.getElementById('schedule-monitor-name').textContent = monitor.name;
    const custom = monitor.schedules?.length > 0 && !monitor.schedule_hours;
    document.getElementById('schedule-custom-warning').classList.toggle('hidden', !custom);
    document.getElementById('schedule-enabled').checked = monitor.schedule_enabled || false;
    document.getElementById('schedule-hours').value = monitor.schedule_hours || '';
    document.getElementById('schedule-ip').value = monitor.schedules && monitor.schedules[0] ? monitor.schedules[0].target_ip : '';

    showModal('schedule-modal');
}

export function hideScheduleSwitchModal() {
    hideModal('schedule-modal');
    state.scheduleMonitorId = null;
}

export async function saveScheduleSwitch() {
    if (!state.scheduleMonitorId) return;
    const isCurrent = captureModal('schedule-modal');
    const current = state.monitorsCache.find(m => m.id == state.scheduleMonitorId);
    if (current?.schedules?.length && !current.schedule_hours &&
        !confirm('保存将替换该策略的全部自定义计划，确定继续吗？')) return;

    const enabled = document.getElementById('schedule-enabled').checked;
    const hours = document.getElementById('schedule-hours').value;
    const ip = document.getElementById('schedule-ip').value.trim();

    let payload = { enabled: false };
    if (enabled) {
        if (!Number.isInteger(Number(hours)) || Number(hours) < 1 || Number(hours) > 23) {
            throw new Error('请填写 1 到 23 的整数小时数');
        }
        const monitor = state.monitorsCache.find(m => m.id == state.scheduleMonitorId);
        if (!monitor || ![monitor.original_ip, monitor.backup_ip].includes(ip)) {
            throw new Error('切换地址必须填写该策略的主地址或备用地址');
        }
        payload = {
            enabled: true,
            cron: `0 */${hours} * * *`,
            target_ip: ip
        };
    }

    await apiRequest(`/api/monitors/${state.scheduleMonitorId}/schedule`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    });
    if (isCurrent()) hideScheduleSwitchModal();
    showNotification('定时切换保存成功', 'success');
    await fetchMonitors();
}

// --- Account Modal --- //
export function openAccountModal(account = null) {
    const modal = document.getElementById('account-modal');
    if (!modal) return;

    state.editingAccountId = account ? account.id : null;
    document.getElementById('account-modal-title').textContent = account ? '编辑凭证' : '添加凭证';
    document.getElementById('account-name').value = account ? account.name : '';
    const token = document.getElementById('account-token');
    token.value = '';
    token.placeholder = account?.has_api_token ? '已设置，留空保留' : '填写 API Token';
    document.getElementById('account-email').value = account?.email || '';
    const apiKey = document.getElementById('account-api-key');
    apiKey.value = '';
    apiKey.placeholder = account?.has_api_key ? '已设置，留空保留' : '填写 API Key';
    
    showModal('account-modal');
}

export function hideAccountModal() {
    hideModal('account-modal');
    state.editingAccountId = null;
}

export async function submitAccountForm() {
    const isCurrent = captureModal('account-modal');
    const name = document.getElementById('account-name').value.trim();
    const token = document.getElementById('account-token').value.trim();
    const email = document.getElementById('account-email').value.trim();
    const apiKey = document.getElementById('account-api-key').value.trim();

    if (!name) throw new Error('请填写凭证名称');
    if (!state.editingAccountId && !token && !apiKey) throw new Error('API Token 和 API Key 至少需要填写一个');

    const payload = { 
        name, 
        api_token: token,
        email: email,
        api_key: apiKey
    };

    if (state.editingAccountId) {
        await apiRequest(`/api/cloudflare-accounts/${encodeURIComponent(state.editingAccountId)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
    } else {
        await apiRequest('/api/cloudflare-accounts', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
    }

    if (isCurrent()) hideAccountModal();
    showNotification('保存成功', 'success');
    await refreshAccountViews();
}
