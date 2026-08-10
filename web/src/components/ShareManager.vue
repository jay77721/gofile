<script setup>
import { ref } from 'vue'
import { apiPost } from '../api'
import { toast } from '../toast'
import { fmtDate } from '../utils'

const props = defineProps({ shares: { type: Array, default: () => [] } })
const emit = defineEmits(['changed'])

const revokeTarget = ref(null)

function shareURL(s) { return location.origin + '/share/' + s.ShareToken }

async function copy(s) {
  try {
    await navigator.clipboard.writeText(shareURL(s))
    toast(s.HasPassword ? '链接已复制（该分享设有提取码，请另行告知对方）' : '链接已复制', 'ok')
  } catch { toast('复制失败，请手动复制', 'warn') }
}

function open(s) {
  if (s.HasPassword) { toast('该分享设有提取码，请使用创建时的链接或告知提取码', 'warn'); return }
  window.open(shareURL(s), '_blank')
}

async function revoke() {
  if (!revokeTarget.value) return
  try {
    await apiPost('/file/share/revoke', { share_token: revokeTarget.value.ShareToken })
    toast('已撤销', 'ok')
    emit('changed')
  } catch (e) { toast(e.message, 'err') }
  revokeTarget.value = null
}
</script>

<template>
  <div>
    <div class="shares-head">
      <h3>我的分享</h3>
      <span class="hint">{{ shares.length }} 个分享</span>
    </div>

    <div v-if="shares.length" class="share-list">
      <div v-for="s in shares" :key="s.ID" class="share-row">
        <div class="sr-main">
          <div class="sr-file">
            <span class="sr-hash" :title="s.FileSha1">{{ s.FileSha1.slice(0, 8) }}…</span>
            <span v-if="s.HasPassword" class="badge">提取码</span>
          </div>
          <div class="sr-meta">
            <span>有效期至 {{ fmtDate(s.ExpireAt) }}</span>
          </div>
        </div>
        <div class="sr-actions">
          <button class="btn btn-soft btn-sm" @click="open(s)">打开</button>
          <button class="btn btn-ghost btn-sm" @click="copy(s)">复制</button>
          <button class="btn btn-danger btn-sm" @click="revokeTarget = s">撤销</button>
        </div>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="empty-icon">🔗</div>
      <div class="empty-text">还没有分享</div>
      <div class="empty-hint">在文件卡片上点击「分享」创建链接</div>
    </div>

    <!-- 撤销确认(复用 ConfirmModal 需 file 字段,构造兼容对象) -->
    <div v-if="revokeTarget" class="modal-mask on" @click.self="revokeTarget = null">
      <div class="modal">
        <h4>撤销分享</h4>
        <p>撤销后链接立即失效，确定要撤销吗？</p>
        <div class="modal-ft">
          <button class="btn btn-ghost btn-sm" @click="revokeTarget = null">取消</button>
          <button class="btn btn-danger btn-sm" @click="revoke">撤销</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.shares-head { display: flex; align-items: baseline; gap: 12px; margin-bottom: 16px; }
.shares-head h3 { font-size: 15px; font-weight: 600; }
.shares-head .hint { font-size: 12px; color: var(--text-muted); }
.share-list { display: flex; flex-direction: column; gap: 10px; }
.share-row {
  display: flex; align-items: center; justify-content: space-between; gap: 14px;
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  padding: 14px 16px; transition: all .18s;
}
.share-row:hover { border-color: var(--border-strong); box-shadow: var(--shadow-sm); }
.sr-file { display: flex; align-items: center; gap: 8px; }
.sr-hash { font-family: "JetBrains Mono", monospace; font-size: 13px; font-weight: 600; }
.badge {
  background: var(--amber-soft); color: var(--amber); font-size: 11px;
  padding: 1px 8px; border-radius: 10px;
}
.sr-meta { font-size: 11px; color: var(--text-muted); margin-top: 3px; }
.sr-actions { display: flex; gap: 8px; flex-shrink: 0; }
</style>
