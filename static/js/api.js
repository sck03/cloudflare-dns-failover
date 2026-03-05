import { state } from './state.js';

export async function apiRequest(path, options = {}) {
    const url = `${state.baseURL}${path}`;
    const response = await fetch(url, options);

    let payload = null;
    try {
        payload = await response.json();
    } catch {
        // ignore
    }

    if (response.status === 401) {
        window.location.href = '/login';
        throw new Error('未登录或登录已过期');
    }

    if (!response.ok) {
        const message = payload?.msg || payload?.message || `请求失败: ${response.status}`;
        throw new Error(message);
    }

    if (payload && typeof payload === 'object' && 'code' in payload && 'data' in payload) {
        return payload.data;
    }
    return payload;
}