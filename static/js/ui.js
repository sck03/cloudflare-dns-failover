import { state } from './state.js';
import { showNotification } from './components/notifications.js';
import { loadDashboardData } from './components/dashboard.js';
import { fetchZones, viewRecords, hideRecords, fetchRecords, deleteRecord } from './components/domains.js';
import { fetchMonitors, editMonitor, deleteMonitor } from './components/monitors.js';
import { openRecordModal, editRecord, openMonitorModal, openAccountModal, openRestoreModal, openScheduleSwitchModal } from './components/modals.js';
import { loadSettings, activateAccount, switchAccount, updateAccountSwitcher, editAccount as editAccountFromSettings, deleteAccount } from './components/settings.js';

export {
    showNotification,
    loadDashboardData,
    fetchZones,
    viewRecords,
    hideRecords,
    fetchRecords,
    deleteRecord,
    fetchMonitors,
    editMonitor,
    deleteMonitor,
    openRecordModal,
    editRecord,
    openMonitorModal,
    openAccountModal,
    openRestoreModal,
    openScheduleSwitchModal,
    loadSettings,
    activateAccount,
    switchAccount,
    updateAccountSwitcher,
    editAccountFromSettings as editAccount,
    deleteAccount
};

export function updateTime() {
    const now = new Date();
    const timeStr = now.toLocaleTimeString('zh-CN');
    document.getElementById('current-time').textContent = timeStr;
    
    const uptime = Math.floor((Date.now() - state.startTime) / 1000);
    const hours = Math.floor(uptime / 3600);
    const minutes = Math.floor((uptime % 3600) / 60);
    const seconds = uptime % 60;
    document.getElementById('stat-uptime').textContent = 
        `${hours}h ${minutes}m ${seconds}s`;
}

export function switchSection(section) {
    console.log(`[UI] switchSection function called for: ${section}`);
    ['dashboard', 'domains', 'strategies', 'settings'].forEach(s => {
        const navItem = document.getElementById(`nav-${s}`);
        const sectionEl = document.getElementById(`section-${s}`);
        
        if (navItem) {
            if (s === section) {
                navItem.classList.add('active');
            } else {
                navItem.classList.remove('active');
            }
        }
        
        if (sectionEl) {
            if (s === section) {
                sectionEl.classList.remove('hidden');
                sectionEl.classList.add('fade-in');
            } else {
                sectionEl.classList.add('hidden');
            }
        }
    });

    const titles = {
        dashboard: '控制面板',
        domains: '域名管理',
        strategies: '监控策略',
        settings: '系统设置'
    };
    document.getElementById('section-title').textContent = titles[section] || '控制面板';

    // Dispatch a custom event to notify that the view has changed
    console.log(`[UI] Dispatching 'viewchanged' event for: ${section}`);
    document.dispatchEvent(new CustomEvent('viewchanged', { detail: { section } }));
}

