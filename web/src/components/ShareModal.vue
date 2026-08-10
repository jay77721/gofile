<script setup>
import { ref } from 'vue'
import { apiPost } from '../api'
import { toast } from '../toast'

const props = defineProps({ file: { type: Object, required: true } })
const emit = defineEmits(['close'])

const days = ref(7)
const pwd = ref('')
const url = ref('')
const creating = ref(false)

async function create() {
  creating.value = true
  try {
    const fd = new FormData()
    fd.append('filehash', props.file.filehash)
    fd.append('days', String(days.value))
    if (pwd.value) fd.append('password', pwd.value)
    const d = await apiPost('/file/share', fd)
    // 有提取码时链接附带 pwd,接收者可直接打开
    url.value = location.origin + d.url + (pwd.value ? '?pwd=' + encodeURIComponent(pwd.value) : '')
    toast('分享成功', 'ok')
  } catch (e) { toast(e.message, 'err') } finally { creating.value = false }
}

async function copy() {
  try {
    await navigator.clipboard.writeText(url.value)
    toast('链接已复制', 'ok')
  } catch { toast('复制失败，请手动选择复制', 'warn') }
}
</script>

<template>
  <div class="modal-mask on" @click.self="emit('close')">
    <div class="modal">
      <h4>分享「{{ file.filename }}」</h4>
      <div class="field">
        <label>有效期（天，1-30）</label>
        <input v-model.number="days" type="number" min="1" max="30">
      </div>
      <div class="field">
        <label>提取码（留空为无密码）</label>
        <input v-model="pwd" placeholder="可选，建议 8 位以上" maxlength="32">
      </div>
      <div v-if="url" class="share-result">
        <input :value="url" readonly @click="$event.target.select()">
        <button class="btn btn-primary btn-sm" @click="copy">复制</button>
      </div>
      <div class="modal-ft">
        <button class="btn btn-ghost btn-sm" @click="emit('close')">关闭</button>
        <button class="btn btn-primary btn-sm" :disabled="creating" @click="create">
          {{ creating ? '创建中…' : '创建分享' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.share-result { display: flex; gap: 8px; margin-top: 4px; }
.share-result input {
  flex: 1; padding: 8px 10px; border-radius: 6px; border: 1px solid var(--border);
  background: var(--surface-2); color: var(--text); font-size: 12px;
  font-family: "JetBrains Mono", monospace; outline: none;
}
</style>
