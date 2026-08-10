<script setup>
import { ref, nextTick, onMounted } from 'vue'
import { apiPost } from '../api'
import { toast } from '../toast'

const props = defineProps({ file: { type: Object, required: true } })
const emit = defineEmits(['close', 'done'])

const name = ref('')
const loading = ref(false)

onMounted(() => {
  const idx = props.file.filename.lastIndexOf('.')
  name.value = idx > 0 ? props.file.filename.slice(0, idx) : props.file.filename
  nextTick(() => inputEl.value?.focus())
})

const inputEl = ref(null)

async function confirm() {
  const base = name.value.trim()
  if (!base) return
  const idx = props.file.filename.lastIndexOf('.')
  const full = base + (idx > 0 ? props.file.filename.slice(idx) : '')
  loading.value = true
  try {
    await apiPost('/file/update', { op: '0', filehash: props.file.filehash, filename: full })
    toast('已重命名', 'ok')
    emit('done')
  } catch (e) { toast(e.message, 'err') } finally { loading.value = false }
}
</script>

<template>
  <div class="modal-mask on" @click.self="emit('close')">
    <div class="modal">
      <h4>重命名</h4>
      <div class="field">
        <label>文件名</label>
        <input ref="inputEl" v-model="name" @keydown.enter="confirm">
      </div>
      <div class="modal-ft">
        <button class="btn btn-ghost btn-sm" @click="emit('close')">取消</button>
        <button class="btn btn-primary btn-sm" :disabled="loading || !name.trim()" @click="confirm">
          {{ loading ? '保存中…' : '保存' }}
        </button>
      </div>
    </div>
  </div>
</template>
