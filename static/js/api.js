import { state } from './state.js';

export async function apiRequest(path, options = {}) {
    const url = `${state.baseURL}${path}`;
    const timeout = AbortSignal.timeout(150000);
    const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout;
    const response = await fetch(url, { ...options, signal });

    let payload = null;
    try {
        payload = await response.json();
    } catch (error) {
        signal.throwIfAborted();
        if (response.ok && response.status !== 204) throw new Error('服务器返回了无效的 JSON 响应', { cause: error });
    }
    signal.throwIfAborted();

    if (response.status === 401 && path !== '/api/auth/login') {
        window.location.href = '/login';
        throw new Error('未登录或登录已过期');
    }

    if (!response.ok) {
        const message = payload?.error || payload?.msg || payload?.message || `请求失败: ${response.status}`;
        throw new Error(message);
    }

    return payload;
}
