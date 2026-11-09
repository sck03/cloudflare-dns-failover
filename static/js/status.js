import { LatestRequest } from './requests.js';

// Dashboard, monitor lists and mutation refreshes share one latest response.
// A slow poll cannot replace the state fetched after an edit or deletion.
export const statusRequest = new LatestRequest();
