// ============================================================
// Doc Gateway - Register Page
// ============================================================

document.getElementById('regForm').addEventListener('submit', async function(e) {
    e.preventDefault();
    const userName = document.getElementById('regUserName').value.trim();
    const userPwd = document.getElementById('regUserPwd').value;
    const confirmPwd = document.getElementById('regConfirmPwd').value;
    const errDiv = document.getElementById('regError');
    const errMsg = document.getElementById('regErrorMsg');

    if (!userName || !userPwd || !confirmPwd) {
        errMsg.textContent = '所有字段不能为空';
        errDiv.style.display = 'flex';
        return;
    }
    if (userPwd !== confirmPwd) {
        errMsg.textContent = '两次输入的密码不一致';
        errDiv.style.display = 'flex';
        return;
    }
    if (userPwd.length < 6) {
        errMsg.textContent = '密码长度至少6个字符';
        errDiv.style.display = 'flex';
        return;
    }

    try {
        const resp = await fetch('/api/register', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ user_name: userName, user_pwd: userPwd })
        });
        const data = await resp.json();
        if (data.status === 'ok') {
            window.location.href = '/login?registered=1';
        } else {
            errMsg.textContent = data.error || '注册失败';
            errDiv.style.display = 'flex';
        }
    } catch (err) {
        errMsg.textContent = '网络错误: ' + err.message;
        errDiv.style.display = 'flex';
    }
});