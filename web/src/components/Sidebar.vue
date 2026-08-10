<script setup>
import { computed } from 'vue'
import { apiPost, session } from '../api'

const props = defineProps({ view: { type: String, default: 'files' } })
const emit = defineEmits(['navigate', 'logout'])

const navs = [
  { key: 'files', label: '我的文件', icon: 'M3 7a2 2 0 0 1 2-2h4l2 3h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z' },
  { key: 'search', label: 'AI 搜索', icon: 'M21 21l-4.35-4.35M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16z' },
  { key: 'trash', label: '回收站', icon: 'M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6h14z' },
  { key: 'shares', label: '我的分享', icon: 'M10 14a5 5 0 0 0 7.07 0l3.54-3.54a5 5 0 0 0-7.07-7.07L11.8 4.91M14 10a5 5 0 0 0-7.07 0L3.39 13.54a5 5 0 0 0 7.07 7.07l1.74-1.74' },
]

const avatar = computed(() => (session.username || '?').charAt(0).toUpperCase())

async function doLogout() {
  try { await apiPost('/user/logout', {}) } catch { /* 幂等,失败也继续清本地状态 */ }
  emit('logout')
}
</script>

<template>
  <aside class="sidebar">
    <div class="side-brand">
      <div class="brand-mark">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
        </svg>
      </div>
      <span class="brand-name">GoFile</span>
    </div>

    <nav class="side-nav">
      <button
        v-for="n in navs"
        :key="n.key"
        class="side-item"
        :class="{ active: view === n.key }"
        @click="emit('navigate', n.key)"
      >
        <svg class="side-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path :d="n.icon"/>
        </svg>
        <span>{{ n.label }}</span>
      </button>
    </nav>

    <div class="side-foot">
      <div class="side-user">
        <div class="avatar">{{ avatar }}</div>
        <div class="user-name">{{ session.username }}</div>
      </div>
      <button class="btn btn-ghost btn-sm side-logout" @click="doLogout">退出登录</button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: var(--sidebar-w); flex-shrink: 0; position: sticky; top: 0; height: 100vh;
  display: flex; flex-direction: column;
  background: var(--surface); border-right: 1px solid var(--border);
}
.side-brand { display: flex; align-items: center; gap: 10px; padding: 18px 20px; }
.brand-mark {
  width: 34px; height: 34px; border-radius: 10px;
  background: var(--primary); color: #fff;
  display: flex; align-items: center; justify-content: center;
}
.brand-mark svg { width: 19px; height: 19px; }
.brand-name { font-size: 17px; font-weight: 700; letter-spacing: -0.3px; }
.side-nav { flex: 1; padding: 8px 12px; display: flex; flex-direction: column; gap: 2px; }
.side-item {
  display: flex; align-items: center; gap: 10px; width: 100%;
  padding: 9px 12px; border: none; border-radius: var(--radius-sm);
  background: transparent; color: var(--text-dim); font-size: 13.5px; font-weight: 500;
  cursor: pointer; transition: all .15s; font-family: inherit; text-align: left;
}
.side-item:hover { background: var(--surface-hover); color: var(--text); }
.side-item.active { background: var(--primary-soft); color: var(--primary); }
.side-icon { width: 18px; height: 18px; flex-shrink: 0; }
.side-foot { padding: 14px 14px 18px; border-top: 1px solid var(--border); }
.side-user { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.avatar {
  width: 32px; height: 32px; border-radius: 50%; background: var(--primary);
  color: #fff; display: flex; align-items: center; justify-content: center;
  font-size: 14px; font-weight: 700; flex-shrink: 0;
}
.user-name { font-size: 13px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.side-logout { width: 100%; }
</style>
