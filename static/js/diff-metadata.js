import { normalizeItemMetadata } from './metadata.js';

function dateTimeLocal(value) {
    if (!value) return '';
    const date = new Date(value);
    const offset = date.getTimezoneOffset() * 60_000;
    return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function metadataFromState(state) {
    return {
        tags: state.currentDiffTags,
        favorite: state.currentDiffFavorite,
        expiresAt: state.currentDiffExpiresAt,
        burnAfterRead: state.currentDiffBurnAfterRead
    };
}

export function readDiffMetadata(state, controls) {
    const source = controls ? {
        tags: controls.tags.value,
        favorite: controls.favorite.checked,
        expiresAt: controls.expiresAt.value || null,
        burnAfterRead: controls.burnAfterRead.checked
    } : metadataFromState(state);
    return applyDiffMetadata(state, source, controls);
}

export function applyDiffMetadata(state, value, controls) {
    const metadata = normalizeItemMetadata(value);
    state.currentDiffTags = metadata.tags;
    state.currentDiffFavorite = metadata.favorite;
    state.currentDiffExpiresAt = metadata.expiresAt;
    state.currentDiffBurnAfterRead = metadata.burnAfterRead;
    if (controls) {
        controls.tags.value = metadata.tags.join(', ');
        controls.favorite.checked = metadata.favorite;
        controls.expiresAt.value = dateTimeLocal(metadata.expiresAt);
        controls.burnAfterRead.checked = metadata.burnAfterRead;
    }
    return metadata;
}

export function resetDiffMetadata(state, controls) {
    return applyDiffMetadata(state, {}, controls);
}
