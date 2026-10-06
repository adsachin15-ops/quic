// QUIC File Transfer — Web UI JavaScript

const API = '';  // Same origin

// ── State ──────────────────────────────────────────

let selectedFile = null;

// ── Elements ───────────────────────────────────────

const tokenInput = document.getElementById('auth-token');
const fileInput = document.getElementById('file-input');
const fileLabel = document.getElementById('file-label');
const fileNameDisplay = document.getElementById('file-name-display');
const uploadBtn = document.getElementById('upload-btn');
const progressArea = document.getElementById('progress-area');
const progressFilename = document.getElementById('progress-filename');
const progressPercent = document.getElementById('progress-percent');
const progressFill = document.getElementById('progress-fill');
const progressTransferred = document.getElementById('progress-transferred');
const progressSpeed = document.getElementById('progress-speed');
const filesList = document.getElementById('files-list');
const confirmModal = document.getElementById('confirm-modal');
const confirmMessage = document.getElementById('confirm-message');
const confirmYes = document.getElementById('confirm-yes');
const confirmNo = document.getElementById('confirm-no');

// ── File Selection ─────────────────────────────────

fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
        selectedFile = fileInput.files[0];
        fileNameDisplay.textContent = `${selectedFile.name} (${formatSize(selectedFile.size)})`;
        fileLabel.classList.add('has-file');
        uploadBtn.disabled = false;
    }
});

// ── Upload ─────────────────────────────────────────

uploadBtn.addEventListener('click', () => {
    if (!selectedFile) return;
    uploadFile(selectedFile);
});

function uploadFile(file) {
    const xhr = new XMLHttpRequest();
    const formData = new FormData();
    formData.append('file', file);

    // Show progress
    progressArea.classList.remove('hidden');
    progressFilename.textContent = file.name;
    uploadBtn.disabled = true;

    const startTime = Date.now();

    xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
            const pct = (e.loaded / e.total) * 100;
            const elapsed = (Date.now() - startTime) / 1000;
            const speed = elapsed > 0 ? e.loaded / elapsed : 0;

            progressFill.style.width = pct + '%';
            progressPercent.textContent = pct.toFixed(1) + '%';
            progressTransferred.textContent = `${formatSize(e.loaded)} / ${formatSize(e.total)}`;
            progressSpeed.textContent = `${formatSize(speed)}/s`;
        }
    });

    xhr.addEventListener('load', () => {
        if (xhr.status === 200) {
            const elapsed = (Date.now() - startTime) / 1000;
            progressPercent.textContent = '✓ Done';
            progressFill.style.width = '100%';
            progressSpeed.textContent = `${elapsed.toFixed(1)}s total`;

            // Reset after a moment
            setTimeout(() => {
                resetUpload();
            }, 3000);

            // Refresh file list
            loadFiles();
        } else {
            progressPercent.textContent = '✗ Error';
            progressSpeed.textContent = xhr.responseText || 'Upload failed';
            setTimeout(resetUpload, 5000);
        }
    });

    xhr.addEventListener('error', () => {
        progressPercent.textContent = '✗ Error';
        progressSpeed.textContent = 'Connection failed';
        setTimeout(resetUpload, 5000);
    });

    xhr.open('POST', API + '/api/upload');
    const token = tokenInput.value;
    if (token) {
        xhr.setRequestHeader('Authorization', 'Bearer ' + token);
    }
    xhr.send(formData);
}

function resetUpload() {
    progressArea.classList.add('hidden');
    progressFill.style.width = '0%';
    progressPercent.textContent = '0%';
    progressTransferred.textContent = '';
    progressSpeed.textContent = '';
    fileNameDisplay.textContent = 'Select a file';
    fileLabel.classList.remove('has-file');
    fileInput.value = '';
    selectedFile = null;
    uploadBtn.disabled = true;
}

// ── File List ──────────────────────────────────────

async function loadFiles() {
    try {
        const headers = {};
        const token = tokenInput.value;
        if (token) headers['Authorization'] = 'Bearer ' + token;

        const resp = await fetch(API + '/api/files', { headers });
        if (!resp.ok) {
            if (resp.status === 401) throw new Error('Unauthorized');
            throw new Error('Failed to load files');
        }

        const files = await resp.json();
        renderFiles(files);
    } catch (err) {
        filesList.innerHTML = `<p class="empty-state" style="color: var(--danger)">${err.message === 'Unauthorized' ? 'Unauthorized — Please enter the correct QUIC_TOKEN.' : 'Failed to load files'}</p>`;
    }
}

function renderFiles(files) {
    if (!files || files.length === 0) {
        filesList.innerHTML = '<p class="empty-state">No files uploaded yet</p>';
        return;
    }

    filesList.innerHTML = files.map(f => `
        <div class="file-row">
            <span class="file-name" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span>
            <span class="file-size">${formatSize(f.size)}</span>
            <div class="file-actions">
                <button class="btn-download" onclick="downloadFile('${escapeJs(f.name)}')">↓ Download</button>
                <button class="btn-delete" onclick="confirmDelete('${escapeJs(f.name)}')">Delete</button>
            </div>
        </div>
    `).join('');
}

// ── Download ───────────────────────────────────────

function downloadFile(filename) {
    // Trigger browser download via a hidden link
    let url = API + '/api/download/' + encodeURIComponent(filename);
    const token = tokenInput.value;
    if (token) {
        url += '?token=' + encodeURIComponent(token);
    }
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
}

// ── Delete ─────────────────────────────────────────

let pendingDelete = null;

function confirmDelete(filename) {
    pendingDelete = filename;
    confirmMessage.textContent = `Delete "${filename}" from the server?`;
    confirmModal.classList.remove('hidden');
}

confirmYes.addEventListener('click', async () => {
    if (!pendingDelete) return;

    try {
        const headers = {};
        const token = tokenInput.value;
        if (token) headers['Authorization'] = 'Bearer ' + token;

        const resp = await fetch(API + '/api/files/' + encodeURIComponent(pendingDelete), {
            method: 'DELETE',
            headers
        });

        if (resp.ok) {
            loadFiles();
        }
    } catch (err) {
        // Silently fail — file list will show actual state
    }

    confirmModal.classList.add('hidden');
    pendingDelete = null;
});

confirmNo.addEventListener('click', () => {
    confirmModal.classList.add('hidden');
    pendingDelete = null;
});

// Close modal on backdrop click
confirmModal.addEventListener('click', (e) => {
    if (e.target === confirmModal) {
        confirmModal.classList.add('hidden');
        pendingDelete = null;
    }
});

// ── Utilities ──────────────────────────────────────

function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    const size = bytes / Math.pow(1024, i);
    return size.toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function escapeJs(str) {
    return str.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}

// ── Init ───────────────────────────────────────────

const urlParams = new URLSearchParams(window.location.search);
if (urlParams.has('token')) {
    tokenInput.value = urlParams.get('token');
}

tokenInput.addEventListener('input', () => {
    loadFiles();
});

loadFiles();
