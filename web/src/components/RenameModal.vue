<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue';
import { apiPost, type FileItem } from '../api';
import { toast } from '../toast';

interface Props {
  file: FileItem;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'done'): void;
}>();

const name = ref<string>('');
const loading = ref<boolean>(false);
const inputEl = ref<HTMLInputElement | null>(null);

onMounted(() => {
  const idx = props.file.filename.lastIndexOf('.');
  name.value = idx > 0 ? props.file.filename.slice(0, idx) : props.file.filename;
  nextTick(() => inputEl.value?.focus());
});

async function confirm(): Promise<void> {
  const base = name.value.trim();
  if (!base) return;
  const idx = props.file.filename.lastIndexOf('.');
  const full = base + (idx > 0 ? props.file.filename.slice(idx) : '');
  loading.value = true;
  try {
    await apiPost('/file/update', { op: '0', filehash: props.file.filehash, filename: full });
    toast('已重命名', 'ok');
    emit('done');
  } catch (e) {
    toast((e as Error).message, 'err');
  } finally {
    loading.value = false;
  }
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
