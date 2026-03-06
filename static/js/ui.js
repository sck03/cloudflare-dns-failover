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

