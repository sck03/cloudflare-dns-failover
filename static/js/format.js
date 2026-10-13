export function escapeHTML(value) {
    return String(value ?? '').replace(/[&<>"']/g, char => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    })[char]);
}

export function monitorStatus(monitor) {
    const states = {
        Normal: ['主备正常', 'status-normal'],
        Failover: ['已故障切换', 'status-warning'],
        Degraded: ['健康异常', 'status-warning'],
        Down: ['主备均不可用', 'status-error'],
        Error: ['处理异常', 'status-error'],
        Unknown: ['等待检测', 'status-warning']
    };
    const [label, className] = states[monitor.status] || states.Unknown;
    return { label, className };
}

export function healthLabel(value) {
    return value === true ? '可用' : value === false ? '不可用' : '待检测';
}

export function formatTime(value) {
    const date = new Date(value);
    return !value || Number.isNaN(date.getTime()) || date.getFullYear() < 2000
        ? '尚无记录' : date.toLocaleString('zh-CN');
}
