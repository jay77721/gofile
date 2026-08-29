<script setup lang="ts">
import { ref } from 'vue';
import { apiPost, type FileItem } from '../api';
import { toast } from '../toast';

interface Props {
  file: FileItem;
  // delete = soft delete (move to trash); purge = permanent deletion
  kind?: 'delete' | 'purge' | string;
}

const props = withDefaults(defineProps<Props>(), {
  kind: 'delete',
});

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'done'): void;
}>();

const loading = ref<boolean>(false);

const isPurge = props.kind === 'purge';

async function confirm(): Promise<void> {
  loading.value = true;
  try {
    await apiPost(isPurge ? '/file/purge' : '/file/delete', { filehash: props.file.filehash });
    toast(isPurge ? '已彻底删除' : '已移入回收站', 'ok');
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
