<script setup lang="ts">
import { fmtSize, fmtDate, fileKind, kindIcon, kindColor, canPreview } from '../utils';
import type { FileItem } from '../api';
import UploadZone from './UploadZone.vue';

interface Props {
  files?: FileItem[];
}

withDefaults(defineProps<Props>(), {
  files: () => [],
});

const emit = defineEmits<{
  (e: 'changed'): void;
  (e: 'preview', file: FileItem): void;
  (e: 'rename', file: FileItem): void;
  (e: 'delete', file: FileItem): void;
  (e: 'share', file: FileItem): void;
  (e: 'similar', query: string): void;
}>();
</script>

<template>
  <div>
    <UploadZone @changed="emit('changed')" />

    <div class="files-header">
      <h3>我的文件</h3>
      <span class="count">{{ files.length }} 项</span>
    </div>

    <div v-if="files.length" class="file-grid">
      <div v-for="f in files" :key="f.filehash" class="file-card">
        <div class="fc-top">
          <div class="fc-icon" :style="{ background: kindColor(fileKind(f.filename)) + '1a', color: kindColor(fileKind(f.filename)) }">
            {{ kindIcon(fileKind(f.filename)) }}
          </div>
          <div class="fc-name" :title="f.filename">{{ f.filename }}</div>
        </div>
        <div v-if="f.summary" class="fc-summary" :title="f.summary">{{ f.summary }}</div>
        <div v-if="f.tags" class="fc-tags">
          <span v-for="t in String(f.tags).split(',')" :key="t" class="tag">{{ t.trim() }}</span>
        </div>
        <div class="fc-meta">
          <span>{{ fmtSize(f.size) }}</span>
          <span>{{ fmtDate(f.upload_time) }}</span>
        </div>
        <div class="fc-actions">
          <button v-if="canPreview(f.filename)" class="act" title="预览" @click="emit('preview', f)">预览</button>
          <button class="act" title="AI 找相似" @click="emit('similar', '与「' + f.filename + '」相似的文件')">相似</button>
          <button class="act" title="分享" @click="emit('share', f)">分享</button>
          <a class="act dl" :href="'/file/download?filehash=' + encodeURIComponent(f.filehash)">下载</a>
          <button class="act" title="重命名" @click="emit('rename', f)">重命名</button>
          <button class="act danger" title="删除" @click="emit('delete', f)">删除</button>
        </div>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="empty-icon">🗂</div>
      <div class="empty-text">还没有文件</div>
      <div class="empty-hint">拖拽文件到上方区域，或点击选择文件上传</div>
    </div>
  </div>
</template>

<style scoped>
.files-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.files-header h3 { font-size: 15px; font-weight: 600; }
.files-header .count { font-size: 12px; color: var(--text-dim); }

.file-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
.file-card {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  padding: 14px 14px 12px; display: flex; flex-direction: column; gap: 8px;
  transition: all .18s; position: relative;
}
.file-card:hover {
  border-color: var(--border-strong); box-shadow: var(--shadow);
  transform: translateY(-1px);
}
.fc-top { display: flex; align-items: center; gap: 10px; min-width: 0; }
.fc-icon {
  width: 38px; height: 38px; border-radius: 10px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center; font-size: 18px;
}
.fc-name {
  font-size: 13.5px; font-weight: 600; overflow: hidden; text-overflow: ellipsis;
  white-space: nowrap; min-width: 0;
}
.fc-summary {
  font-size: 12px; color: var(--text-dim); line-height: 1.5;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.fc-tags { display: flex; gap: 5px; flex-wrap: wrap; }
.fc-tags .tag {
  background: var(--surface-2); color: var(--text-dim); font-size: 11px;
  padding: 1px 8px; border-radius: 10px;
}
.fc-meta { display: flex; gap: 12px; font-size: 11px; color: var(--text-muted); font-family: "JetBrains Mono", monospace; }
.fc-actions { display: flex; gap: 6px; margin-top: auto; padding-top: 4px; flex-wrap: wrap; }
.fc-actions .act {
  flex: 1; min-width: 52px; padding: 5px 6px; border-radius: 6px; border: 1px solid var(--border);
  background: var(--surface); color: var(--text-dim); cursor: pointer;
  font-size: 11px; font-weight: 500; font-family: inherit; transition: all .15s;
}
.fc-actions .act:hover { background: var(--surface-hover); color: var(--text); }
.fc-actions .act.dl:hover { background: var(--primary-soft); color: var(--primary); border-color: var(--primary); text-decoration: none; }
.fc-actions .act.danger:hover { background: var(--red-soft); color: var(--red); border-color: var(--red); }
</style>
