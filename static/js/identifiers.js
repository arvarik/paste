export function isPasteReference(value) {
    return /^(?:[a-zA-Z0-9]{6}|[a-zA-Z0-9]{32})$/.test(value);
}
