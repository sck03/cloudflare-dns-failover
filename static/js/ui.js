export * from './components/notifications.js';
export * from './components/dashboard.js';
export * from './components/domains.js';
export * from './components/monitors.js';
export * from './components/modals.js';
export * from './components/settings.js';

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

    switch(section) {
        case 'dashboard':
            loadDashboardData();
            break;
        case 'domains':
            updateAccountSwitcher();
            fetchZones();
            break;
        case 'strategies':
            fetchMonitors();
            break;
        case 'settings':
            loadSettings();
            break;
    }
}