// ============================================================
// Doc Gateway - Admin Panel JavaScript
// ============================================================

let currentIfaceId = 0;

function logout() {
    fetch('/api/logout', { method: 'POST' }).finally(() => { window.location.href = '/login'; });
}

function toast(msg, type) {
    var t = document.getElementById('toast');
    t.textContent = msg; t.className = 'cfg-toast ' + type; t.style.display = 'block';
    setTimeout(function() { t.style.display = 'none'; }, 3000);
}

// ============================================================
// Tab switching
// ============================================================
document.querySelectorAll('.cfg-tab').forEach(tab => {
    tab.addEventListener('click', function() {
        document.querySelectorAll('.cfg-tab').forEach(t => t.classList.remove('active'));
        document.querySelectorAll('.cfg-section').forEach(s => s.classList.remove('active'));
        this.classList.add('active');
        var section = document.getElementById('section-' + this.dataset.tab);
        if (section) section.classList.add('active');
        // Load data on tab switch
        if (this.dataset.tab === 'interfaces') loadInterfaces();
        else if (this.dataset.tab === 'llm') loadLLMConfigs();
        else if (this.dataset.tab === 'files') loadFiles();
        else if (this.dataset.tab === 'logs') loadLogs();
        else if (this.dataset.tab === 'oa-sim') { loadInterfacesForSelect(); }
        else if (this.dataset.tab === 'proofread-sim') { loadProofreadConfig(); }
        else if (this.dataset.tab === 'sys') loadSysConfig();
        else if (this.dataset.tab === 'users') loadUsers();
    });
});

// ============================================================
// Interfaces (接口管理) CRUD
// ============================================================
async function loadInterfaces() {
    try {
        var resp = await fetch('/api/inner/interfaces');
        var json = await resp.json();
        var data = json.data || [];
        var tbody = document.getElementById('ifacesTbody');
        if (data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;color:#999;">暂无接口</td></tr>';
            return;
        }
        tbody.innerHTML = data.map(function(iface) {
            return '<tr>' +
                '<td>' + iface.id + '</td>' +
                '<td><strong>' + esc(iface.name) + '</strong></td>' +
                '<td><code>/api/v1/' + esc(iface.path) + '</code></td>' +
                '<td>' + (iface.enabled ? '<span class="badge badge-success">启用</span>' : '<span class="badge badge-gray">禁用</span>') + '</td>' +
                '<td>v' + iface.version + '</td>' +
                '<td>' +
                    '<button class="cfg-btn cfg-btn-reset" onclick="editIface(' + iface.id + ')" style="padding:4px 10px;font-size:0.8rem;">编辑</button>' +
                    '<button class="cfg-btn cfg-btn-reset" onclick="testIface(' + iface.id + ')" style="padding:4px 10px;font-size:0.8rem;margin-left:4px;">测试</button>' +
                    '<button class="cfg-btn cfg-btn-reset" onclick="deleteIface(' + iface.id + ')" style="padding:4px 10px;font-size:0.8rem;margin-left:4px;color:#c62828;">删除</button>' +
                '</td>' +
            '</tr>';
        }).join('');
    } catch (err) {
        toast('加载接口列表失败: ' + err.message, 'error');
    }
}

function showIfaceModal() {
    currentIfaceId = 0;
    document.getElementById('ifaceModalTitle').innerHTML = '<i class="fas fa-code"></i> 新建接口';
    document.getElementById('ifaceId').value = '0';
    document.getElementById('ifaceName').value = '';
    document.getElementById('ifacePath').value = '';
    document.getElementById('ifaceDesc').value = '';
    document.getElementById('ifacePrompt').value = '';
    document.getElementById('ifaceSchema').value = '';
    document.getElementById('ifaceTemp').value = '';
    document.getElementById('ifaceMaxTok').value = '';
    document.getElementById('ifaceEnabled').checked = true;
    document.getElementById('inputParamsContainer').innerHTML = '';
    document.getElementById('ifaceModal').classList.add('show');
    loadLLMForSelect();
}

async function editIface(id) {
    try {
        var resp = await fetch('/api/inner/interfaces/' + id);
        var json = await resp.json();
        var iface = json.data;
        currentIfaceId = iface.id;
        document.getElementById('ifaceModalTitle').innerHTML = '<i class="fas fa-code"></i> 编辑接口';
        document.getElementById('ifaceId').value = iface.id;
        document.getElementById('ifaceName').value = iface.name;
        document.getElementById('ifacePath').value = iface.path;
        document.getElementById('ifaceDesc').value = iface.description || '';
        document.getElementById('ifacePrompt').value = iface.prompt_template;
        document.getElementById('ifaceSchema').value = iface.output_schema || '';
        document.getElementById('ifaceTemp').value = iface.temperature || '';
        document.getElementById('ifaceMaxTok').value = iface.max_tokens || '';
        document.getElementById('ifaceEnabled').checked = iface.enabled;
        document.getElementById('ifaceModal').classList.add('show');
        loadLLMForSelect(iface.llm_config_id);
        // Render input params
        var container = document.getElementById('inputParamsContainer');
        container.innerHTML = '';
        (iface.input_params || []).forEach(function(p) {
            addInputParamRow(p.name, p.source, p.required, p['default'] || '');
        });
    } catch (err) {
        toast('加载接口失败: ' + err.message, 'error');
    }
}

function hideIfaceModal() {
    document.getElementById('ifaceModal').classList.remove('show');
}

function addInputParam(name, source, required, defVal) {
    addInputParamRow(name || '', source || '', required || false, defVal || '');
}

function addInputParamRow(name, source, required, defVal) {
    var container = document.getElementById('inputParamsContainer');
    var row = document.createElement('div');
    row.className = 'param-row';
    row.style.cssText = 'display:flex;gap:8px;margin-bottom:8px;align-items:center;';
    row.innerHTML =
        '<input type="text" class="param-name" placeholder="名称" value="' + esc(name) + '" style="flex:1;padding:6px 10px;border:1px solid #e0e3e8;border-radius:6px;font-size:0.82rem;">' +
        '<input type="text" class="param-source" placeholder="来源" value="' + esc(source) + '" style="flex:2;padding:6px 10px;border:1px solid #e0e3e8;border-radius:6px;font-size:0.82rem;">' +
        '<label style="display:flex;align-items:center;gap:4px;font-size:0.8rem;white-space:nowrap;"><input type="checkbox" class="param-required" ' + (required ? 'checked' : '') + '> 必填</label>' +
        '<input type="text" class="param-default" placeholder="默认值" value="' + esc(defVal) + '" style="flex:1;padding:6px 10px;border:1px solid #e0e3e8;border-radius:6px;font-size:0.82rem;">' +
        '<button type="button" class="cfg-btn cfg-btn-reset" onclick="this.parentElement.remove()" style="padding:4px 8px;">×</button>';
    container.appendChild(row);
}

function collectInputParams() {
    var params = [];
    document.querySelectorAll('#inputParamsContainer .param-row').forEach(function(row) {
        var name = row.querySelector('.param-name').value.trim();
        var source = row.querySelector('.param-source').value.trim();
        if (name && source) {
            params.push({
                name: name,
                source: source,
                required: row.querySelector('.param-required').checked,
                'default': row.querySelector('.param-default').value || ''
            });
        }
    });
    return params;
}

document.getElementById('ifaceForm').addEventListener('submit', async function(e) {
    e.preventDefault();
    var data = {
        name: document.getElementById('ifaceName').value.trim(),
        path: document.getElementById('ifacePath').value.trim(),
        description: document.getElementById('ifaceDesc').value.trim(),
        prompt_template: document.getElementById('ifacePrompt').value.trim(),
        output_schema: document.getElementById('ifaceSchema').value.trim(),
        llm_config_id: parseInt(document.getElementById('ifaceLLM').value) || null,
        temperature: parseFloat(document.getElementById('ifaceTemp').value) || null,
        max_tokens: parseInt(document.getElementById('ifaceMaxTok').value) || null,
        enabled: document.getElementById('ifaceEnabled').checked,
        input_params: collectInputParams()
    };

    if (!data.name || !data.path || !data.prompt_template) {
        toast('名称、路径和提示词模板不能为空', 'error'); return;
    }

    try {
        var url = '/api/inner/interfaces';
        var method = 'POST';
        if (currentIfaceId > 0) {
            url += '/' + currentIfaceId;
            method = 'PUT';
        }
        var resp = await fetch(url, { method: method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
        var json = await resp.json();
        if (json.status === 'ok') {
            toast('保存成功', 'success');
            hideIfaceModal();
            loadInterfaces();
        } else {
            toast(json.error || '保存失败', 'error');
        }
    } catch (err) {
        toast('保存失败: ' + err.message, 'error');
    }
});

async function testIface(id) {
    try {
        var resp = await fetch('/api/inner/interfaces/' + id);
        var json = await resp.json();
        var iface = json.data;
        // Show rendered prompt
        var testResp = await fetch('/api/inner/interfaces/' + id + '/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ test: true })
        });
        var testJson = await testResp.json();
        alert('接口: ' + iface.name + '\n路径: ' + iface.path + '\n\n渲染后的提示词:\n' + testJson.rendered_prompt.substring(0, 1000) + '...');
    } catch (err) {
        toast('测试失败: ' + err.message, 'error');
    }
}

async function deleteIface(id) {
    if (!confirm('确定删除此接口？')) return;
    try {
        var resp = await fetch('/api/inner/interfaces/' + id, { method: 'DELETE' });
        var json = await resp.json();
        if (json.status === 'ok') { toast('删除成功', 'success'); loadInterfaces(); }
        else { toast(json.error || '删除失败', 'error'); }
    } catch (err) { toast('删除失败: ' + err.message, 'error'); }
}

// ============================================================
// 模型配置 CRUD
// ============================================================
let currentLLMId = 0;

async function loadLLMConfigs() {
    try {
        var resp = await fetch('/api/inner/llm-configs');
        var json = await resp.json();
        var data = json.data || [];
        var tbody = document.getElementById('llmTbody');
        if (data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" style="text-align:center;color:#999;">暂无模型配置</td></tr>';
            return;
        }
        tbody.innerHTML = data.map(function(c) {
            return '<tr>' +
                '<td><strong>' + esc(c.name) + '</strong></td>' +
                '<td><code>' + esc(c.api_uri) + '</code></td>' +
                '<td>' + esc(c.model_name) + '</td>' +
                '<td>' + (c.is_default ? '<span class="badge badge-success">默认</span>' : '<button class="cfg-btn cfg-btn-reset" onclick="setDefaultLLM(' + c.id + ')" style="padding:2px 8px;font-size:0.75rem;">设为默认</button>') + '</td>' +
                '<td>' +
                    '<button class="cfg-btn cfg-btn-reset" onclick="editLLM(' + c.id + ')" style="padding:4px 10px;font-size:0.8rem;">编辑</button>' +
                    '<button class="cfg-btn cfg-btn-reset" onclick="deleteLLM(' + c.id + ')" style="padding:4px 10px;font-size:0.8rem;margin-left:4px;color:#c62828;">删除</button>' +
                '</td>' +
            '</tr>';
        }).join('');
    } catch (err) { toast('加载模型配置失败: ' + err.message, 'error'); }
}

function showLLMModal() {
    currentLLMId = 0;
    document.getElementById('llmModalTitle').innerHTML = '<i class="fas fa-robot"></i> 新建模型配置';
    document.getElementById('llmId').value = '0';
    document.getElementById('llmName').value = '';
    document.getElementById('llmURI').value = '';
    document.getElementById('llmKey').value = '';
    document.getElementById('llmModel').value = '';
    document.getElementById('llmTemp').value = '0.7';
    document.getElementById('llmMaxTok').value = '4096';
    document.getElementById('llmDefault').checked = false;
    document.getElementById('llmModal').classList.add('show');
}

async function editLLM(id) {
    try {
        var resp = await fetch('/api/inner/llm-configs'); var json = await resp.json();
        var cfg = (json.data || []).find(function(c) { return c.id === id; });
        if (!cfg) return;
        currentLLMId = id;
        document.getElementById('llmModalTitle').innerHTML = '<i class="fas fa-robot"></i> 编辑模型配置';
        document.getElementById('llmId').value = cfg.id;
        document.getElementById('llmName').value = cfg.name;
        document.getElementById('llmURI').value = cfg.api_uri;
        document.getElementById('llmKey').value = cfg.api_key;
        document.getElementById('llmModel').value = cfg.model_name;
        document.getElementById('llmTemp').value = cfg.temperature;
        document.getElementById('llmMaxTok').value = cfg.max_tokens;
        document.getElementById('llmDefault').checked = cfg.is_default;
        document.getElementById('llmModal').classList.add('show');
    } catch (err) { toast('加载失败: ' + err.message, 'error'); }
}

function hideLLMModal() { document.getElementById('llmModal').classList.remove('show'); }

document.getElementById('llmForm').addEventListener('submit', async function(e) {
    e.preventDefault();
    var data = {
        name: document.getElementById('llmName').value.trim(),
        api_uri: document.getElementById('llmURI').value.trim(),
        api_key: document.getElementById('llmKey').value.trim(),
        model_name: document.getElementById('llmModel').value.trim(),
        temperature: parseFloat(document.getElementById('llmTemp').value) || 0.7,
        max_tokens: parseInt(document.getElementById('llmMaxTok').value) || 4096,
        is_default: document.getElementById('llmDefault').checked
    };
    if (!data.name || !data.api_uri || !data.model_name) { toast('名称、API地址和模型名称不能为空', 'error'); return; }
    try {
        var url = '/api/inner/llm-configs'; var method = 'POST';
        if (currentLLMId > 0) { url += '/' + currentLLMId; method = 'PUT'; }
        var resp = await fetch(url, { method: method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
        var json = await resp.json();
        if (json.status === 'ok') { toast('保存成功', 'success'); hideLLMModal(); loadLLMConfigs(); }
        else { toast(json.error || '保存失败', 'error'); }
    } catch (err) { toast('保存失败: ' + err.message, 'error'); }
});

async function setDefaultLLM(id) {
    try {
        var resp = await fetch('/api/inner/llm-configs/' + id + '/default', { method: 'PUT' });
        var json = await resp.json();
        if (json.status === 'ok') { toast('已设为默认', 'success'); loadLLMConfigs(); }
        else { toast(json.error || '设置失败', 'error'); }
    } catch (err) { toast('设置失败: ' + err.message, 'error'); }
}

async function deleteLLM(id) {
    if (!confirm('确定删除此模型配置？')) return;
    try {
        var resp = await fetch('/api/inner/llm-configs/' + id, { method: 'DELETE' });
        var json = await resp.json();
        if (json.status === 'ok') { toast('删除成功', 'success'); loadLLMConfigs(); }
        else { toast(json.error || '删除失败', 'error'); }
    } catch (err) { toast('删除失败: ' + err.message, 'error'); }
}

async function loadLLMForSelect(selectedId) {
    try {
        var resp = await fetch('/api/inner/llm-configs'); var json = await resp.json();
        var sel = document.getElementById('ifaceLLM');
        sel.innerHTML = '<option value="">-- 使用默认模型 --</option>';
        (json.data || []).forEach(function(c) {
            sel.innerHTML += '<option value="' + c.id + '"' + (c.id === selectedId ? ' selected' : '') + '>' + esc(c.name) + ' (' + esc(c.model_name) + ')</option>';
        });
    } catch (_) {}
}

// ============================================================
// Files
// ============================================================
async function loadFiles() {
    try {
        var resp = await fetch('/api/inner/files'); var json = await resp.json();
        var data = json.data || [];
        var tbody = document.getElementById('filesTbody');
        if (data.length === 0) { tbody.innerHTML = '<tr><td colspan="7" style="text-align:center;color:#999;">暂无文件</td></tr>'; return; }
        tbody.innerHTML = data.map(function(f) {
            var badge = f.status === 'ready' ? 'badge-success' : f.status === 'failed' ? 'badge-danger' : 'badge-warning';
            return '<tr>' +
                '<td><code>' + f.file_id + '</code></td>' +
                '<td>' + esc(f.original_name) + '</td>' +
                '<td>' + esc(f.file_type) + '</td>' +
                '<td><span class="badge ' + badge + '">' + f.status + '</span></td>' +
                '<td>' + f.text_length + '</td>' +
                '<td>' + f.created_at + '</td>' +
                '<td><button class="cfg-btn cfg-btn-reset" onclick="deleteFile(\'' + f.file_id + '\')" style="padding:4px 10px;font-size:0.8rem;color:#c62828;">删除</button></td>' +
            '</tr>';
        }).join('');
    } catch (err) { toast('加载文件列表失败: ' + err.message, 'error'); }
}

async function deleteFile(fileId) {
    if (!confirm('确定删除此文件及预处理结果？')) return;
    try {
        var resp = await fetch('/api/inner/files/' + fileId, { method: 'DELETE' });
        var json = await resp.json();
        if (json.status === 'ok') { toast('删除成功', 'success'); loadFiles(); }
        else { toast(json.error || '删除失败', 'error'); }
    } catch (err) { toast('删除失败: ' + err.message, 'error'); }
}

// ============================================================
// 任务日志
// ============================================================
async function loadLogs() {
    try {
        var resp = await fetch('/api/inner/task-trace'); var json = await resp.json();
        var data = json.data || [];
        var tbody = document.getElementById('logsTbody');
        if (data.length === 0) { tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;color:#999;">暂无任务记录</td></tr>'; return; }
        tbody.innerHTML = data.map(function(t) {
            var badge = statusBadge(t.status);
            return '<tr>' +
                '<td><code>' + esc(t.task_id).substring(0, 8) + '...</code></td>' +
                '<td>' + esc(t.linkid || '-') + '</td>' +
                '<td><code>' + esc(t.interface_path) + '</code></td>' +
                '<td><span class="badge ' + badge + '">' + t.status + '</span></td>' +
                '<td>' + t.created_at + '</td>' +
                '<td><button class="cfg-btn cfg-btn-reset" onclick="showTaskDetail(\'' + t.task_id + '\')" style="padding:4px 10px;font-size:0.8rem;">详情</button></td>' +
            '</tr>';
        }).join('');
    } catch (err) { toast('加载任务日志失败: ' + err.message, 'error'); }
}

function statusBadge(status) {
    switch (status) {
        case 'success': case 'completed': return 'badge-success';
        case 'processing': return 'badge-warning';
        case 'pending': return 'badge-gray';
        default: return 'badge-danger';
    }
}

async function showTaskDetail(taskId) {
    try {
        var resp = await fetch('/api/inner/task-trace/' + encodeURIComponent(taskId));
        var json = await resp.json();
        var t = json.data;
        if (!t) { toast('任务不存在', 'error'); return; }

        var detailEl = document.getElementById('taskDetailContent');
        detailEl.innerHTML =
            '<table class="kv-table">' +
            '<tr><td class="kv-key">task_id</td><td><code>' + esc(t.task_id) + '</code></td></tr>' +
            '<tr><td class="kv-key">linkid</td><td>' + esc(t.linkid || '-') + '</td></tr>' +
            '<tr><td class="kv-key">接口路径</td><td><code>' + esc(t.interface_path) + '</code></td></tr>' +
            '<tr><td class="kv-key">状态</td><td><span class="badge ' + statusBadge(t.status) + '">' + t.status + '</span></td></tr>' +
            '<tr><td class="kv-key">重试次数</td><td>' + t.retry_count + '</td></tr>' +
            '<tr><td class="kv-key">回调地址</td><td><code style="word-break:break-all;">' + esc(t.callback_url) + '</code></td></tr>' +
            '<tr><td class="kv-key">错误信息</td><td style="color:#c62828;">' + esc(t.error_message || '-') + '</td></tr>' +
            '<tr><td class="kv-key">创建时间</td><td>' + t.created_at + '</td></tr>' +
            '<tr><td class="kv-key">完成时间</td><td>' + (t.finished_at || '-') + '</td></tr>' +
            '</table>' +
            codeSection('请求体 (request_body)', t.request_body) +
            codeSection('文件摘要 (files_summary)', t.files_summary) +
            codeSection('LLM Prompt', t.llm_request_prompt) +
            codeSection('LLM 原始输出 (llm_raw_output)', t.llm_raw_output) +
            codeSection('最终输出 (output_result)', t.output_result) +
            codeSection('回调请求体 (callback_body)', t.callback_body) +
            codeSection('时间线 (timeline)', t.timeline);

        document.getElementById('taskDetailModal').classList.add('show');
    } catch (err) { toast('查询失败: ' + err.message, 'error'); }
}

function codeSection(title, jsonStr) {
    if (!jsonStr || jsonStr === '{}' || jsonStr === '[]' || jsonStr === '') return '';
    var formatted = jsonStr;
    try { formatted = JSON.stringify(JSON.parse(jsonStr), null, 2); } catch (_) {}
    return '<h4 class="kv-section">' + esc(title) + '</h4>' +
        '<pre class="task-detail-json">' + esc(formatted) + '</pre>';
}

function hideTaskDetail() {
    document.getElementById('taskDetailModal').classList.remove('show');
}

// ============================================================
// 操作模拟
// ============================================================
async function loadInterfacesForSelect() {
    try {
        var resp = await fetch('/api/inner/interfaces'); var json = await resp.json();
        var sel = document.getElementById('oaInterface');
        sel.innerHTML = '<option value="">-- 选择接口 --</option>';
        (json.data || []).forEach(function(iface) {
            if (iface.enabled) sel.innerHTML += '<option value="' + esc(iface.path) + '">' + esc(iface.name) + ' (' + esc(iface.path) + ')</option>';
        });
    } catch (_) {}
}

async function oaCall() {
    var ifacePath = document.getElementById('oaInterface').value;
    var body = {};
    try { body = JSON.parse(document.getElementById('oaParams').value || '{}'); } catch(e) { toast('参数 JSON 格式错误', 'error'); return; }

    if (!ifacePath) { toast('请选择接口', 'error'); return; }

    document.getElementById('oaResult').textContent = '提交中...';
    try {
        var start = Date.now();
        var resp = await fetch('/api/v1/' + ifacePath, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        var elapsed = Date.now() - start;
        var json = await resp.json();
        var result = JSON.stringify(json, null, 2);
        document.getElementById('oaResult').textContent = result + '\n\n--- 受理耗时: ' + elapsed + 'ms ---';
    } catch (err) {
        document.getElementById('oaResult').textContent = '调用失败: ' + err.message;
    }
}

// ============================================================
// System Config
// ============================================================
async function loadSysConfig() {
    try {
        var resp = await fetch('/api/inner/config'); var json = await resp.json();
        document.getElementById('sysName').value = json.data.sys.name || '';
        var apiEnabled = json.data.api && json.data.api.auth_enabled;
        document.getElementById('apiAuthEnabled').checked = apiEnabled || false;
        document.getElementById('apiAuthToken').value = (json.data.api && json.data.api.auth_token) || '';
        document.getElementById('apiTokenRow').style.display = apiEnabled ? 'flex' : '';
    } catch (_) {}
}

function generateAPIToken() {
    var chars = '0123456789abcdef';
    var token = '';
    for (var i = 0; i < 32; i++) { token += chars[Math.floor(Math.random() * 16)]; }
    document.getElementById('apiAuthToken').value = token;
}

async function saveSysConfig() {
    try {
        var resp = await fetch('/api/inner/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                sys: { name: document.getElementById('sysName').value },
                api: {
                    auth_enabled: document.getElementById('apiAuthEnabled').checked,
                    auth_token: document.getElementById('apiAuthToken').value
                }
            })
        });
        var json = await resp.json();
        if (json.status === 'ok') { toast('保存成功', 'success'); }
        else { toast(json.error || '保存失败', 'error'); }
    } catch (err) { toast('保存失败: ' + err.message, 'error'); }
}

// ============================================================
// Users
// ============================================================
async function loadUsers() {
    try {
        var resp = await fetch('/api/inner/users'); var json = await resp.json();
        var data = json.data || [];
        var tbody = document.getElementById('usersTbody');
        tbody.innerHTML = data.map(function(u) {
            return '<tr><td>' + esc(u.user_name) + '</td><td>' + (u.role === 1 ? '管理员' : '普通用户') + '</td><td>' + esc(u.note||'') + '</td>' +
                '<td>' + (u.user_name !== 'admin' ? '<button class="cfg-btn cfg-btn-reset" onclick="deleteUser(\'' + u.user_name + '\')" style="padding:4px 10px;font-size:0.8rem;color:#c62828;">删除</button>' : '') + '</td></tr>';
        }).join('');
    } catch (err) { toast('加载用户失败: ' + err.message, 'error'); }
}

async function addUser() {
    var name = document.getElementById('newUserName').value.trim();
    var pwd = document.getElementById('newUserPwd').value.trim();
    var role = parseInt(document.getElementById('newUserRole').value);
    if (!name || !pwd) { toast('用户名和密码不能为空', 'error'); return; }
    try {
        var resp = await fetch('/api/inner/users', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ user_name: name, user_pwd: pwd, role: role })
        });
        var json = await resp.json();
        if (json.status === 'ok') { toast('添加成功', 'success'); loadUsers(); document.getElementById('newUserName').value = ''; document.getElementById('newUserPwd').value = ''; }
        else { toast(json.error || '添加失败', 'error'); }
    } catch (err) { toast('添加失败: ' + err.message, 'error'); }
}

async function deleteUser(name) {
    if (!confirm('确定删除用户 ' + name + '？')) return;
    try {
        var resp = await fetch('/api/inner/users/' + name, { method: 'DELETE' });
        var json = await resp.json();
        if (json.status === 'ok') { toast('删除成功', 'success'); loadUsers(); }
        else { toast(json.error || '删除失败', 'error'); }
    } catch (err) { toast('删除失败: ' + err.message, 'error'); }
}

// ============================================================
// 审稿模拟（rdCLI 回调模式 — 不轮询）
// ============================================================
var prAllowedExts = ['.docx', '.doc', '.pdf', '.txt', '.md', '.xlsx'];
var prCurrentTaskId = '';
var prCurrentStatus = '';

function prFileExt(fileName) {
    var i = fileName.lastIndexOf('.');
    if (i < 0) return '';
    return fileName.slice(i).toLowerCase();
}

async function loadProofreadConfig() {
    // doc_audit 配置已移至 cfg.yml，无需从 API 加载界面配置
    restoreProofreadState();
}

async function restoreProofreadState() {
    try {
        var resp = await fetch('/api/inner/proofread/state');
        var json = await resp.json();
        if (json.status !== 'ok') return;
        var data = json.data || {};
        if (data.task_id) {
            prSetTaskId(data.task_id);
            prCurrentStatus = data.status || '';
            if (prCurrentStatus === 'pending' || prCurrentStatus === 'processing') {
                document.getElementById('prResult').textContent = '（上一任务 ' + data.task_id + ' 状态: ' + prCurrentStatus + '，请手动点击查询）';
            } else {
                document.getElementById('prResult').textContent = '（上一任务 ' + data.task_id + ' 已完成，点击查询查看结果）';
            }
        }
    } catch (_) {}
}

function prSetTaskId(taskId) {
    prCurrentTaskId = taskId;
    var el = document.getElementById('prTaskId');
    el.textContent = taskId || '无';
}

async function proofreadSubmit() {
    var fileInput = document.getElementById('prFile');
    if (!fileInput.files || !fileInput.files[0]) { toast('请选择文档文件', 'error'); return; }

    var file = fileInput.files[0];
    var ext = prFileExt(file.name);
    if (prAllowedExts.indexOf(ext) < 0) {
        toast('不支持的文件格式: ' + (ext || '无扩展名') + '，仅支持 docx/doc/pdf/txt/md/xlsx', 'error');
        return;
    }

    // 已有未完成的审稿任务时提醒
    if (prCurrentTaskId && (prCurrentStatus === 'pending' || prCurrentStatus === 'processing')) {
        var ok = confirm('当前任务 ' + prCurrentTaskId + ' 尚未完成审稿，重新提交将覆盖它。是否继续？');
        if (!ok) return;
    }

    document.getElementById('prResult').textContent = '提交中...';

    var formData = new FormData();
    formData.append('file', file);
    var instructions = document.getElementById('prInstructions').value.trim();
    if (instructions) formData.append('additional_instructions', instructions);

    try {
        var start = Date.now();
        var resp = await fetch('/api/inner/proofread/submit', {
            method: 'POST',
            body: formData
        });
        var elapsed = Date.now() - start;
        var json = await resp.json();
        var result = JSON.stringify(json, null, 2);
        document.getElementById('prResult').textContent = result + '\n\n--- 耗时: ' + elapsed + 'ms ---';

        if (json.success && json.data && json.data.task_id) {
            prSetTaskId(json.data.task_id);
            prCurrentStatus = 'pending';
            fileInput.value = '';
            // 不轮询，等对方回调完成即可
            document.getElementById('prResult').textContent = result + '\n\n--- 耗时: ' + elapsed + 'ms ---\n\n>>> 任务已提交，等待 rdCLI 核稿完成后主动推送结果。请稍后点击「查询」按钮查看结果。<<<';
        }
    } catch (err) {
        document.getElementById('prResult').textContent = '提交失败: ' + err.message;
    }
}

async function proofreadQuery() {
    var taskId = prCurrentTaskId;
    if (!taskId) { toast('请先提交文档', 'error'); return; }

    document.getElementById('prResult').textContent = '查询中...';

    try {
        var start = Date.now();
        var resp = await fetch('/api/inner/proofread/query/' + encodeURIComponent(taskId));
        var elapsed = Date.now() - start;
        var json = await resp.json();
        var result = JSON.stringify(json, null, 2);
        document.getElementById('prResult').textContent = result + '\n\n--- 耗时: ' + elapsed + 'ms ---';

        if (json.success && json.data) {
            prCurrentStatus = json.data.status || '';
        }
    } catch (err) {
        document.getElementById('prResult').textContent = '查询失败: ' + err.message;
    }
}

// ============================================================
// Helpers
// ============================================================
function esc(s) { return (s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); }
function sleep(ms) { return new Promise(function(r) { setTimeout(r, ms); }); }

// Page init — admin 加载首 tab，只读用户加载任务日志
if (document.getElementById('llmTbody')) {
    loadLLMConfigs();
} else {
    loadLogs();
}