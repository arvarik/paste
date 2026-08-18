import { handleDiffWorkerMessage } from './diff-worker-runtime.js';

self.addEventListener('message', (event) => {
    handleDiffWorkerMessage(event.data, (message) => self.postMessage(message));
});
