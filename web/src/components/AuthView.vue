<script setup>
import { ref } from 'vue'
import { apiPost, session } from '../api'
import { toast } from '../toast'

const emit = defineEmits(['authed'])

const tab = ref('login')
const user = ref('')
const pwd = ref('')
const pwd2 = ref('')
const loading = ref(false)
const msg = ref('')
const msgType = ref('error')
const showPwd = ref(false)

function clearMsg() { msg.value = '' }

async function doLogin() {
  if (!user.value.trim() || !pwd.value) { msg.value = '请输入用户名和密码'; msgType.value = 'error'; return }
  loading.value = true
  msg.value = ''
  try {
    const d = await apiPost('/user/signin', { username: user.value.trim(), password: pwd.value })
    session.username = d.Username || user.value.trim()
    toast('欢迎回来，' + session.username, 'ok')
    emit('authed')
  } catch (e) { msg.value = e.message; msgType.value = 'error' } finally { loading.value = false }
}

async function doRegister() {
  const name = user.value.trim()
  if (name.length < 3) { msg.value = '用户名至少 3 位'; msgType.value = 'error'; return }
  if (pwd.value.length < 5) { msg.value = '密码至少 5 位'; msgType.value = 'error'; return }
  if (pwd.value !== pwd2.value) { msg.value = '两次输入的密码不一致'; msgType.value = 'error'; return }
  loading.value = true
  msg.value = ''
  try {
    await apiPost('/user/signup', { username: name, password: pwd.value })
    toast('注册成功，请登录', 'ok')
    tab.value = 'login'
    pwd.value = ''
    pwd2.value = ''
  } catch (e) { msg.value = e.message; msgType.value = 'error' } finally { loading.value = false }
}
</script>

<template>
  <div class="auth-wrap">
    <div class="auth-card">
      <div class="auth-logo">
        <div class="logo-mark">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
          </svg>
        </div>
        <h1>GoFile</h1>
        <p>懂语义的智能网盘</p>
      </div>

      <div class="auth-tabs">
        <button :class="{ active: tab === 'login' }" @click="tab = 'login'; clearMsg()">登录</button>
        <button :class="{ active: tab === 'register' }" @click="tab = 'register'; clearMsg()">注册</button>
      </div>

      <form @submit.prevent="tab === 'login' ? doLogin() : doRegister()">
        <div class="field">
          <label>用户名</label>
          <input v-model="user" placeholder="请输入用户名" autocomplete="username" @input="clearMsg">
        </div>
        <div class="field">
          <label>密码</label>
          <input v-model="pwd" :type="showPwd ? 'text' : 'password'" placeholder="请输入密码" autocomplete="current-password" @input="clearMsg">
        </div>
        <div v-if="tab === 'register'" class="field">
          <label>确认密码</label>
          <input v-model="pwd2" :type="showPwd ? 'text' : 'password'" placeholder="请再次输入密码" autocomplete="new-password" @input="clearMsg">
        </div>
        <div class="auth-row">
          <label class="pwd-toggle">
            <input v-model="showPwd" type="checkbox"> 显示密码
          </label>
        </div>
        <button class="btn btn-primary" type="submit" :disabled="loading">
          {{ loading ? '请稍候…' : (tab === 'login' ? '登 录' : '注 册') }}
        </button>
        <div class="auth-msg" :class="msgType">{{ msg }}</div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.auth-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 24px; }
.auth-card {
  width: 100%; max-width: 380px; background: var(--surface);
  border: 1px solid var(--border); border-radius: var(--radius-lg);
  padding: 36px 34px 28px; box-shadow: var(--shadow-lg);
}
.auth-logo { text-align: center; margin-bottom: 26px; }
.logo-mark {
  width: 48px; height: 48px; margin: 0 auto 10px; border-radius: 14px;
  background: var(--primary-soft); color: var(--primary);
  display: flex; align-items: center; justify-content: center;
}
.logo-mark svg { width: 26px; height: 26px; }
.auth-logo h1 { font-size: 24px; font-weight: 700; letter-spacing: -0.5px; }
.auth-logo p { font-size: 12px; color: var(--text-dim); margin-top: 2px; }
.auth-tabs {
  display: flex; gap: 4px; margin-bottom: 22px;
  background: var(--surface-2); border-radius: var(--radius-sm); padding: 3px;
}
.auth-tabs button {
  flex: 1; padding: 8px; border: none; border-radius: 6px; cursor: pointer;
  font-size: 13px; font-weight: 500; transition: all .15s;
  background: transparent; color: var(--text-dim); font-family: inherit;
}
.auth-tabs button.active { background: var(--surface); color: var(--text); box-shadow: var(--shadow-sm); }
.auth-row { display: flex; justify-content: flex-end; margin: -6px 0 14px; }
.pwd-toggle { display: flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-dim); cursor: pointer; }
.auth-msg { text-align: center; font-size: 12px; margin-top: 14px; min-height: 18px; }
.auth-msg.error { color: var(--red); }
.auth-msg.success { color: var(--green); }
</style>
