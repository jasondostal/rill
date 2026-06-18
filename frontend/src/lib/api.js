// api.js — REST client for rill frontend.
const TIMEOUT_MS = 30_000;

async function get(path) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
        const resp = await fetch(path, { credentials: 'same-origin', signal: controller.signal });
        if (!resp.ok) throw new Error(`Server error (${resp.status})`);
        return resp.json();
    } finally { clearTimeout(timeout); }
}

async function post(path, body) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
        const resp = await fetch(path, {
            method: 'POST', credentials: 'same-origin', signal: controller.signal,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!resp.ok) throw new Error(`Server error (${resp.status})`);
        return resp.json();
    } finally { clearTimeout(timeout); }
}

async function patch(path, body) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
        const resp = await fetch(path, {
            method: 'PATCH', credentials: 'same-origin', signal: controller.signal,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!resp.ok) throw new Error(`Server error (${resp.status})`);
        return resp.json();
    } finally { clearTimeout(timeout); }
}

async function del(path) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
        const resp = await fetch(path, {
            method: 'DELETE', credentials: 'same-origin', signal: controller.signal,
        });
        if (!resp.ok) throw new Error(`Server error (${resp.status})`);
        return resp.json();
    } finally { clearTimeout(timeout); }
}

export const api = {
    // ============================================================
    // Auth
    // ============================================================
    authStatus: () => get('/api/auth/status'),
    me: () => fetch('/api/auth/me', { credentials: 'same-origin' }).then(r => r.ok ? r.json() : null),
    login: (username, password) => fetch('/api/auth/login', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
    }).then(r => r.ok ? r.json() : Promise.reject(new Error('Invalid credentials'))),
    logout: () => fetch('/api/auth/logout', { method: 'POST', credentials: 'same-origin' }),
    setup: (username, password, confirm) => fetch('/api/auth/setup', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password, confirm }),
    }).then(r => r.ok ? r.json() : Promise.reject(new Error('Setup failed'))),

    // ============================================================
    // Tokens
    // ============================================================
    listTokens: () => get('/api/tokens').then(r => r.tokens || []),
    createToken: (name, scopes = ['read', 'write'], ttl = '') => post('/api/tokens', { name, scopes, ttl }),
    revokeToken: (id) => del(`/api/tokens?id=${encodeURIComponent(id)}`),

    // ============================================================
    // Memory: write
    // ============================================================
    remember: (payload) => post('/api/remember', payload),

    // ============================================================
    // Dashboard stats (aggregates)
    // ============================================================
    stats: (range = '90d') => get(`/api/stats?range=${encodeURIComponent(range)}`),

    // ============================================================
    // Settings (runtime config)
    // ============================================================
    getSettings: () => get('/api/settings').then(r => r.settings || []),
    updateSetting: (key, value) => patch('/api/settings', { key, value }),

    // ============================================================
    // Memory: ping / orient
    // ============================================================
    isReady: async () => {
        try {
            const resp = await fetch('/api/ping', { credentials: 'same-origin' });
            return resp.ok;
        } catch { return false; }
    },
    orient: (args = {}) => {
        const p = new URLSearchParams();
        if (args.project) p.set('project', args.project);
        if (args.force) p.set('force', '1');
        return get(`/api/orient?${p}`);
    },
    orientRegen: (project = '') => {
        const p = new URLSearchParams();
        if (project) p.set('project', project);
        return post(`/api/orient/regen?${p}`, {});
    },

    // ============================================================
    // Memory: entities
    // ============================================================
    listEntities: (args = {}) => {
        const p = new URLSearchParams();
        if (args.type) p.set('type', args.type);
        if (args.promoted !== undefined && args.promoted !== null) p.set('promoted', args.promoted ? 'true' : 'false');
        if (args.sort) p.set('sort', args.sort);
        if (args.limit) p.set('limit', String(args.limit));
        return get(`/api/entities?${p}`).then(r => r.entities || []);
    },
    getEntity: (type, slug) =>
        get(`/api/entity/${encodeURIComponent(type)}/${encodeURIComponent(slug)}`),
    editHandNotes: (type, slug, text, mode = 'append') =>
        post(`/api/entity/${encodeURIComponent(type)}/${encodeURIComponent(slug)}/hand_notes`, { text, mode }),
    promote: (type, slug) =>
        post(`/api/entity/${encodeURIComponent(type)}/${encodeURIComponent(slug)}/promote`, {}),
    demote: (type, slug) =>
        post(`/api/entity/${encodeURIComponent(type)}/${encodeURIComponent(slug)}/demote`, {}),
    // Merge this entity (source) into `target` (same type — bare name or full record id).
    // Returns { source, target, edges_moved, mentions_moved, self_loops_dropped }. Admin only.
    mergeEntity: (type, slug, target) =>
        post(`/api/entity/${encodeURIComponent(type)}/${encodeURIComponent(slug)}/merge`, { target }),
    // Set the entity's current version (bi-temporal, superseding). Returns the updated entity detail.
    setVersion: (type, slug, version) =>
        post(`/api/entity/${encodeURIComponent(type)}/${encodeURIComponent(slug)}/version`, { version }),

    // ============================================================
    // Memory: edges
    // ============================================================
    addEdge: (edge) => post('/api/edge', edge),
    closeEdge: (edgeID) => post(`/api/edge/${encodeURIComponent(edgeID)}/close`, {}),

    // ============================================================
    // Memory: memories
    // ============================================================
    listMemories: (args = {}) => {
        const p = new URLSearchParams();
        if (args.kind) p.set('kind', args.kind);
        if (args.project) p.set('project', args.project);
        if (args.author) p.set('author', args.author);
        if (args.limit) p.set('limit', String(args.limit));
        if (args.before) p.set('before', args.before);
        return get(`/api/memories?${p}`);
    },
    getMemory: (memoryID) => {
        const id = memoryID.startsWith('memory:') ? memoryID.slice('memory:'.length) : memoryID;
        return get(`/api/memory/${encodeURIComponent(id)}`);
    },
    editMemory: (memoryID, patchBody) => {
        const id = memoryID.startsWith('memory:') ? memoryID.slice('memory:'.length) : memoryID;
        return patch(`/api/memory/${encodeURIComponent(id)}`, patchBody);
    },
    forget: (memoryID) => {
        const id = memoryID.startsWith('memory:') ? memoryID.slice('memory:'.length) : memoryID;
        return del(`/api/memory/${encodeURIComponent(id)}`);
    },
    recall: (query, args = {}) => post('/api/recall', { query, ...args }),

    // ============================================================
    // Documents (standalone markdown docs)
    // ============================================================
    listDocs: (args = {}) => {
        const p = new URLSearchParams();
        if (args.project) p.set('project', args.project);
        if (args.doc_type) p.set('doc_type', args.doc_type);
        if (args.entity) p.set('entity', args.entity);
        if (args.limit) p.set('limit', String(args.limit));
        if (args.before) p.set('before', args.before);
        return get(`/api/docs?${p}`);
    },
    getDoc: (docID) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return get(`/api/docs/${encodeURIComponent(id)}`);
    },
    createDoc: (payload) => post('/api/docs', payload),
    updateDoc: (docID, payload) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return patch(`/api/docs/${encodeURIComponent(id)}`, payload);
    },
    deleteDoc: (docID) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return del(`/api/docs/${encodeURIComponent(id)}`);
    },
    restoreDoc: (docID) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return post(`/api/docs/${encodeURIComponent(id)}/restore`);
    },
    // Real file download (auth via same-origin cookie) — use as an <a href> target.
    exportDocMarkdownUrl: (docID) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return `/api/docs/${encodeURIComponent(id)}/export.md`;
    },
    associateDoc: (docID, name, type) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return post(`/api/docs/${encodeURIComponent(id)}/entities`, { name, type });
    },
    unassociateDoc: (docID, entityType, entitySlug) => {
        const id = docID.startsWith('document:') ? docID.slice('document:'.length) : docID;
        return del(`/api/docs/${encodeURIComponent(id)}/entities/${encodeURIComponent(entityType)}/${encodeURIComponent(entitySlug)}`);
    },
};
