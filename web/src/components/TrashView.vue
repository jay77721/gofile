<script setup lang="ts">
import { ref } from 'vue';
import { apiPost, type FileItem } from '../api';
import { toast } from '../toast';
import { fmtSize, fmtDate } from '../utils';
import ConfirmModal from './ConfirmModal.vue';

interface Props {
  trash?: FileItem[];
}

withDefaults(defineProps<Props>(), {
  trash: () => [],
});

const emit = defineEmits<{
  (e: 'changed'): void;
}>();

const purgeTarget = ref<FileItem | null>(null);

async function restore(f: FileItem): Promise<void> {
  try {
    await apiPost('/file/restore', { filehash: f.filehash });
    toast('已恢复', 'ok');
    emit('changed');
  } catch (e) {
    toast((e as Error).message, 'err');
  }
}
</script>

<template>
  <div>
    <div class="trash-head">
      <h3>回收站</h3>
      <span class="hint">删除的文件保留 7 天后自动清理</span>
    </div>

    <div v-if="trash.length" class="file-grid">
      <div v-for="f in trash" :key="f.filehash" class="file-card">
        <div class="fc-name" :title="f.filename">{{ f.filename }}</div>
        <div class="fc-meta">
          <span>{{ fmtSize(f.size) }}</span>
          <span>{{ fmtDate(f.upload_time) }}</span>
        </div>
        <div class="fc-actions">
          <button class="act restore" @click="restore(f)">恢复</button>
          <button class="act danger" @click="purgeTarget = f">彻底删除</button>
        </div>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="empty-icon">🗑</div>
      <div class="empty-text">回收站是空的</div>
      <div class="empty-hint">删除的文件会先进入回收站</div>
    </div>

    <ConfirmModal v-if="purgeTarget" :file="purgeTarget" kind="purge" @close="purgeTarget = null" @done="purgeTarget = null; emit('changed')" />
  </div>
</template>

<style scoped>
.trash-head { display: flex; align-items: baseline; gap: 12px; margin-bottom: 16px; }
.trash-head h3 { font-size: 15px; font-weight: 600; }
.trash-head .hint { font-size: 12px; color: var(--text-muted); }
.file-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
.file-card {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  padding: 14px; display: flex; flex-direction: column; gap: 8px; transition: all .18s;
}
.file-card:hover { border-color: var(--border-strong); box-shadow: var(--shadow); }
.fc-name { font-size: 13.5px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fc-meta { display: flex; gap: 12px; font-size: 11px; color: var(--text-muted); font-family: "JetBrains Mono", monospace; }
.fc-actions { display: flex; gap: 6px; margin-top: auto; }
.fc-actions .act {
  flex: 1; padding: 5px 6px; border-radius: 6px; border: 1px solid var(--border);
  background: var(--surface); color: var(--text-dim); cursor: pointer;
  font-size: 11px; font-weight: 500; font-family: inherit; transition: all .15s;
}
.fc-actions .act.restore:hover { background: var(--green-soft); color: var(--green); border-color: var(--green); }
.fc-actions .act.danger:hover { background: var(--red-soft); color: var(--red); border-color: var(--red); }
</style>
