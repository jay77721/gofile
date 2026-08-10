<script setup>
import { ref } from 'vue'
import { apiPost } from '../api'
import { toast } from '../toast'

const props = defineProps({
  file: { type: Object, required: true },
  // delete = 软删除(进回收站);purge = 彻底删除
  kind: { type: String, default: 'delete' },
})
const emit = defineEmits(['close', 'done'])

const loading = ref(false)

const isPurge = props.kind === 'purge'

async function confirm() {
  loading.value = true
  try {
    await apiPost(isPurge ? '/file/purge' : '/file/delete', { filehash: props.file.filehash })
    toast(isPurge ? '已彻底删除' : '已移入回收站', 'ok')
    emit('done')
  } catch (e) { toast(e.message, 'err') } finally { loading.value = false }
}
</script>

<template>
  <div class="modal-mask on" @click.self="emit('close')">
    <div class="modal">
      <h4>{{ isPurge ? '彻底删除' : '删除文件' }}</h4>
      <p>
        确定要{{ isPurge ? '彻底删除' : '删除' }}
        <strong class="fname">{{ file.filename }}</strong> 吗？
        {{ isPurge ? '此操作不可恢复。' : '可在回收站中恢复。' }}
      </p>
      <div class="modal-ft">
        <button class="btn btn-ghost btn-sm" @click="emit('close')">取消</button>
        <button class="btn btn-danger btn-sm" :disabled="loading" @click="confirm">
          {{ loading ? '处理中…' : (isPurge ? '彻底删除' : '删除') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.fname { color: var(--text); word-break: break-all; }
</style>
