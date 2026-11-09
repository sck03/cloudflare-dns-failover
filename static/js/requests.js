import { apiRequest } from './api.js';

// Only the newest request may update a view, even if an older response arrives
// after navigation or an account switch. Each view owns its request lifetime.
export class LatestRequest {
    controller = null;

    cancel() {
        this.controller?.abort();
        this.controller = null;
    }

    async run(path, apply) {
        this.cancel();
        const controller = new AbortController();
        this.controller = controller;
        try {
            const result = await apiRequest(path, { signal: controller.signal });
            if (!controller.signal.aborted) apply(result);
        } catch (error) {
            if (!controller.signal.aborted) throw error;
        } finally {
            if (this.controller === controller) this.controller = null;
        }
    }
}
