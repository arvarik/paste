import { computeDiff } from './diff-core.js';

export function handleDiffWorkerMessage(message, emit) {
    if (!message || message.type !== 'compare' || !message.requestId) return false;
    try {
        const result = computeDiff(message.payload);
        emit({ type: 'result', requestId: message.requestId, result });
    } catch (error) {
        emit({
            type: 'error',
            requestId: message.requestId,
            error: error instanceof Error ? error.message : 'Diff calculation failed'
        });
    }
    return true;
}
