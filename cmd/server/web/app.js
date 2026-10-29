// QUIC Direct Relay — High-Performance Client Interface

const API = ''; // Same origin relative paths

// ── State ──────────────────────────────────────────
let selectedFile = null;
let currentUploadXhr = null;
let incomingFilename = null;

// ── Elements ───────────────────────────────────────
const dropZone = document.getElementById('drop-zone');
const fileInput = document.getElementById('file-input');
const browseBtn = document.getElementById('browse-btn');
const tokenToggle = document.getElementById('token-toggle');
const tokenContainer = document.getElementById('token-container');
const tokenInput = document.getElementById('auth-token');
const saveTokenBtn = document.getElementById('save-token-btn');

// Upload States
const uploadIdleState = document.getElementById('upload-idle-state');
const uploadSelectedState = document.getElementById('upload-selected-state');
const uploadActiveState = document.getElementById('upload-active-state');
const selectedName = document.getElementById('selected-name');
const selectedSize = document.getElementById('selected-size');
const cancelSelectionBtn = document.getElementById('cancel-selection-btn');
const startUploadBtn = document.getElementById('start-upload-btn');

// Active Progress & Share
const activeStatusTitle = document.getElementById('active-status-title');
const activeStatusDesc = document.getElementById('active-status-desc');
const progressFill = document.getElementById('progress-fill');
const progressPercent = document.getElementById('progress-percent');
const progressSpeed = document.getElementById('progress-speed');
const shareUrlInput = document.getElementById('share-url-input');
const copyShareBtn = document.getElementById('copy-share-btn');
const copyBtnIcon = document.getElementById('copy-btn-icon');
const copyBtnText = document.getElementById('copy-btn-text');
const qrShareBtn = document.getElementById('qr-share-btn');

// Incoming File Hero
const incomingCard = document.getElementById('incoming-card');
const incomingFilenameEl = document.getElementById('incoming-filename');
const incomingSizeEl = document.getElementById('incoming-size');
const incomingDownloadBtn = document.getElementById('incoming-download-btn');

// Files List
const filesList = document.getElementById('files-list');
const filesCount = document.getElementById('files-count');

// QR Modal
const qrModal = document.getElementById('qr-modal');
const qrModalBackdrop = document.getElementById('qr-modal-backdrop');
const qrCloseBtn = document.getElementById('qr-close-btn');
const qrImage = document.getElementById('qr-image');
const qrFileLabel = document.getElementById('qr-file-label');

// Toast
const toast = document.getElementById('toast');
const toastMessage = document.getElementById('toast-message');

// ── Initialization ─────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    initUrlParams();
    initDragAndDrop();
    initEventListeners();
    loadFiles();
    setInterval(loadFiles, 2000);
});

function initUrlParams() {
    const params = new URLSearchParams(window.location.search);
    if (params.has('token')) {
        tokenInput.value = params.get('token');
        tokenContainer.classList.remove('hidden');
    }
    if (params.has('file')) {
        incomingFilename = params.get('file');
        showIncomingCard(incomingFilename);
    }
}

function showIncomingCard(filename) {
    incomingCard.classList.remove('hidden');
    incomingFilenameEl.textContent = filename;
    incomingSizeEl.textContent = 'Host is broadcasting this file live in RAM';
}

// ── Event Listeners ────────────────────────────────

function initEventListeners() {
    // Token Toggle
    tokenToggle.addEventListener('click', () => {
        tokenContainer.classList.toggle('hidden');
        if (!tokenContainer.classList.contains('hidden')) {
            tokenInput.focus();
        }
    });

    saveTokenBtn.addEventListener('click', () => {
        showToast('Token applied for requests');
        tokenContainer.classList.add('hidden');
        loadFiles();
    });

    tokenInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            saveTokenBtn.click();
        }
    });

    // File selection
    browseBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        fileInput.click();
    });

    uploadIdleState.addEventListener('click', () => {
        fileInput.click();
    });

    fileInput.addEventListener('change', () => {
        if (fileInput.files.length > 0) {
            handleFileChosen(fileInput.files[0]);
        }
    });

    cancelSelectionBtn.addEventListener('click', resetUploadState);

    startUploadBtn.addEventListener('click', () => {
        if (selectedFile) {
            startHosting(selectedFile);
        }
    });

    // Share buttons in active card
    copyShareBtn.addEventListener('click', () => {
        copyToClipboard(shareUrlInput.value, copyBtnIcon, copyBtnText);
    });

    qrShareBtn.addEventListener('click', () => {
        if (selectedFile) {
            openQrModal(selectedFile.name, shareUrlInput.value);
        }
    });

    // Incoming download
    incomingDownloadBtn.addEventListener('click', () => {
        if (incomingFilename) {
            downloadFile(incomingFilename);
        }
    });

    // QR Modal close
    qrCloseBtn.addEventListener('click', closeQrModal);
    qrModalBackdrop.addEventListener('click', closeQrModal);
}

// ── Drag & Drop ────────────────────────────────────

function initDragAndDrop() {
    ['dragenter', 'dragover'].forEach(eventName => {
        dropZone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
            dropZone.classList.add('drag-over');
        });
    });

    ['dragleave', 'dragend', 'drop'].forEach(eventName => {
        dropZone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
            dropZone.classList.remove('drag-over');
        });
    });

    dropZone.addEventListener('drop', (e) => {
        const dt = e.dataTransfer;
        if (dt && dt.files && dt.files.length > 0) {
            handleFileChosen(dt.files[0]);
        }
    });
}

// ── File Selection & Upload ────────────────────────

function handleFileChosen(file) {
    selectedFile = file;
    selectedName.textContent = file.name;
    selectedSize.textContent = formatSize(file.size);

    uploadIdleState.classList.add('hidden');
    uploadSelectedState.classList.remove('hidden');
    uploadActiveState.classList.add('hidden');
}

function startHosting(file) {
    uploadSelectedState.classList.add('hidden');
    uploadActiveState.classList.remove('hidden');

    activeStatusTitle.textContent = 'Buffering stream into memory...';
    activeStatusDesc.textContent = 'Establishing direct QUIC relay session...';
    progressFill.style.width = '0%';
    progressPercent.textContent = '0%';

    // Build share URL
    const shareUrl = buildShareUrl(file.name);
    shareUrlInput.value = shareUrl;

    const formData = new FormData();
    formData.append('file', file);

    const xhr = new XMLHttpRequest();
    currentUploadXhr = xhr;

    const startTime = Date.now();

    xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
            const pct = (e.loaded / e.total) * 100;
            const elapsed = (Date.now() - startTime) / 1000;
            const speed = elapsed > 0 ? e.loaded / elapsed : 0;

            progressFill.style.width = pct + '%';
            progressPercent.textContent = pct.toFixed(0) + '%';
            progressSpeed.textContent = `${formatSize(e.loaded)} / ${formatSize(e.total)} • ${formatSize(speed)}/s`;

            if (pct >= 99) {
                activeStatusTitle.textContent = '📡 Broadcasting live in RAM';
                activeStatusDesc.textContent = 'Awaiting peer connection. Share the link or QR code below.';
            }
        }
    });

    xhr.addEventListener('load', () => {
        if (xhr.status === 200) {
            activeStatusTitle.textContent = '✓ Transfer Complete!';
            activeStatusDesc.textContent = 'The recipient has successfully received the entire file.';
            progressFill.style.background = 'var(--brand-emerald)';
            progressPercent.textContent = '100%';
            progressSpeed.textContent = 'Delivered to peer';
            showToast('✓ File successfully transferred!');

            setTimeout(() => {
                resetUploadState();
                loadFiles();
            }, 6000);
        } else {
            activeStatusTitle.textContent = 'Transmission Error';
            activeStatusDesc.textContent = xhr.responseText || 'Relay was interrupted.';
            progressFill.style.background = 'var(--brand-rose)';
            setTimeout(resetUploadState, 6000);
        }
    });

    xhr.addEventListener('error', () => {
        activeStatusTitle.textContent = 'Connection Interrupted';
        activeStatusDesc.textContent = 'Lost connection to relay server.';
        progressFill.style.background = 'var(--brand-rose)';
        setTimeout(resetUploadState, 6000);
    });

    let uploadUrl = API + '/api/upload';
    const token = tokenInput.value.trim();
    if (token) {
        uploadUrl += '?token=' + encodeURIComponent(token);
    }

    xhr.open('POST', uploadUrl);
    if (token) {
        xhr.setRequestHeader('Authorization', 'Bearer ' + token);
    }
    xhr.send(formData);

    // Refresh active list so uploader sees it listed
    setTimeout(loadFiles, 600);
}

function resetUploadState() {
    if (currentUploadXhr && currentUploadXhr.readyState !== 4) {
        currentUploadXhr.abort();
    }
    currentUploadXhr = null;
    selectedFile = null;
    fileInput.value = '';

    uploadIdleState.classList.remove('hidden');
    uploadSelectedState.classList.add('hidden');
    uploadActiveState.classList.add('hidden');

    progressFill.style.width = '0%';
    progressFill.style.background = 'var(--gradient-brand)';
    progressPercent.textContent = '0%';
    progressSpeed.textContent = '';
}

// ── Active Files List ──────────────────────────────

async function loadFiles() {
    try {
        const headers = {};
        const token = tokenInput.value.trim();
        if (token) headers['Authorization'] = 'Bearer ' + token;

        const resp = await fetch(API + '/api/files', { headers });
        if (!resp.ok) {
            if (resp.status === 401) throw new Error('Unauthorized');
            throw new Error('Failed to fetch files');
        }

        const files = await resp.json();
        renderFiles(files);
    } catch (err) {
        if (err.message === 'Unauthorized') {
            filesList.innerHTML = '<div class="empty-state-modern"><p style="color: var(--brand-rose)">Unauthorized — Enter your access token in the navigation bar.</p></div>';
        }
    }
}

function renderFiles(files) {
    if (!files || files.length === 0) {
        filesCount.textContent = '0 Live';
        filesList.innerHTML = `
            <div class="empty-state-modern">
                <div class="empty-icon-wrap">
                    <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                        <rect width="18" height="18" x="3" y="3" rx="2" ry="2"/>
                        <line x1="3" x2="21" y1="9" y2="9"/>
                        <line x1="9" x2="9" y1="21" y2="9"/>
                    </svg>
                </div>
                <h4>No active relays right now</h4>
                <p>Drop a file above to broadcast an instant transmission stream.</p>
            </div>
        `;
        return;
    }

    filesCount.textContent = `${files.length} Live`;

    filesList.innerHTML = files.map(f => {
        const icon = getFileIcon(f.name);
        const shareUrl = buildShareUrl(f.name);

        return `
            <div class="file-card-item">
                <div class="file-info-group">
                    <div class="file-symbol-badge">${icon}</div>
                    <div class="file-text-col">
                        <span class="file-name-text" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span>
                        <span class="file-subtext">Ephemeral Stream In RAM</span>
                    </div>
                </div>
                <div class="file-actions-group">
                    <span class="file-size-tag">${formatSize(f.size)}</span>
                    <button type="button" class="btn-stream-dl" onclick="downloadFile('${escapeJs(f.name)}')">
                        <span>↓ Download</span>
                    </button>
                    <button type="button" class="btn-sub-action" onclick="copyToClipboard('${escapeJs(shareUrl)}')">
                        <span>📋 Copy Link</span>
                    </button>
                    <button type="button" class="btn-sub-action" onclick="openQrModal('${escapeJs(f.name)}', '${escapeJs(shareUrl)}')">
                        <span>📱 QR</span>
                    </button>
                </div>
            </div>
        `;
    }).join('');
}

// ── Download ───────────────────────────────────────

function downloadFile(filename) {
    let url = API + '/api/download/' + encodeURIComponent(filename);
    const token = tokenInput.value.trim();
    if (token) {
        url += '?token=' + encodeURIComponent(token);
    }

    showToast(`Streaming ${filename}...`);

    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);

    setTimeout(loadFiles, 1200);
}

// ── Share & QR Helpers ─────────────────────────────

function buildShareUrl(filename) {
    const loc = window.location;
    return `${loc.protocol}//${loc.host}/?file=${encodeURIComponent(filename)}`;
}

function openQrModal(filename, url) {
    qrFileLabel.textContent = filename;
    qrImage.src = `https://api.qrserver.com/v1/create-qr-code/?size=240x240&data=${encodeURIComponent(url)}`;
    qrModal.classList.remove('hidden');
}

function closeQrModal() {
    qrModal.classList.add('hidden');
    qrImage.src = '';
}

function copyToClipboard(text, iconEl, textEl) {
    const doFeedback = () => {
        showToast('📋 Link copied to clipboard!');
        if (textEl) {
            const original = textEl.textContent;
            textEl.textContent = 'Copied!';
            if (iconEl) iconEl.textContent = '✓';
            setTimeout(() => {
                textEl.textContent = original;
                if (iconEl) iconEl.textContent = '📋';
            }, 2000);
        }
    };

    if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(doFeedback).catch(() => fallbackCopy(text, doFeedback));
    } else {
        fallbackCopy(text, doFeedback);
    }
}

function fallbackCopy(text, cb) {
    const temp = document.createElement('input');
    temp.value = text;
    document.body.appendChild(temp);
    temp.select();
    document.execCommand('copy');
    document.body.removeChild(temp);
    if (cb) cb();
}

function showToast(message) {
    toastMessage.textContent = message;
    toast.classList.remove('hidden');
    setTimeout(() => {
        toast.classList.add('hidden');
    }, 3200);
}

// ── Utilities ──────────────────────────────────────

function formatSize(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    const size = bytes / Math.pow(1024, i);
    return size.toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function getFileIcon(name) {
    const ext = name.split('.').pop().toLowerCase();
    const map = {
        png: '🖼️', jpg: '🖼️', jpeg: '🖼️', gif: '🖼️', webp: '🖼️', svg: '🖼️',
        mp4: '🎬', mov: '🎬', mkv: '🎬', avi: '🎬', webm: '🎬',
        mp3: '🎵', wav: '🎵', flac: '🎵', aac: '🎵',
        pdf: '📕', doc: '📘', docx: '📘', txt: '📄', md: '📝',
        zip: '🗜️', tar: '🗜️', gz: '🗜️', '7z': '🗜️', rar: '🗜️',
        iso: '💿', exe: '⚙️', dmg: '💿', deb: '📦', rpm: '📦',
        go: '💻', js: '💻', ts: '💻', py: '💻', html: '🌐', css: '🎨'
    };
    return map[ext] || '📄';
}

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function escapeJs(str) {
    return str.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}
