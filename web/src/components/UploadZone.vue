<script setup lang="ts">
import { ref } from 'vue';
import { api, apiPost, apiUpload, type FileItem } from '../api';
import { toast } from '../toast';
import { CHUNK_SIZE, CHUNK_MIN, MAX_FILE, sha1HeadTail, fmtSize } from '../utils';

const emit = defineEmits<{
  (e: 'changed'): void;
}>();

const selected = ref<File | null>(null);
const dragOver = ref<boolean>(false);
const uploading = ref<boolean>(false);
const pct = ref<number>(0);
const cancelFlag = ref<boolean>(false);
const fileInput = ref<HTMLInputElement | null>(null);

function pick(f?: File | null): void {
  if (!f) return;
  if (f.size > MAX_FILE) {
    toast('文件过大（最大 100MB）', 'err');
    return;
  }
  selected.value = f;
}

function onDrop(e: DragEvent): void {
  dragOver.value = false;
  if (e.dataTransfer?.files.length) {
    pick(e.dataTransfer.files[0]);
  }
}

function onFileInputChange(e: Event): void {
  const target = e.target as HTMLInputElement | null;
  if (target?.files?.length) {
    pick(target.files[0]);
  }
}

function clear(): void {
  selected.value = null;
  cancelFlag.value = false;
  if (fileInput.value) {
    fileInput.value.value = '';
  }
}

function cancel(): void {
  cancelFlag.value = true;
  toast('已取消', 'warn');
}

async function start(): Promise<void> {
  if (!selected.value || uploading.value) return;
  const file = selected.value;
  uploading.value = true;
  cancelFlag.value = false;
  pct.value = 0;
  try {
    const hash = await sha1HeadTail(file);
    // 秒传检测:meta 命中即已存在
    try {
      await api<FileItem>('/file/meta?filehash=' + encodeURIComponent(hash));
      toast('秒传成功', 'ok');
      emit('changed');
      clear();
      return;
    } catch {
      /* 未命中，继续上传 */
    }

    if (file.size >= CHUNK_MIN) {
      // 分片上传 + 断点续传
      let uploaded: string[] = [];
      try {
        const statusList = await api<number[]>('/file/upload/status?filehash=' + encodeURIComponent(hash));
        uploaded = (statusList || []).map(String);
      } catch {
        /* 无历史 */
      }
      const upSet = new Set<string>(uploaded);
      const total = Math.ceil(file.size / CHUNK_SIZE);
      for (let i = 0; i < total; i++) {
        if (cancelFlag.value) throw new Error('cancelled');
        if (upSet.has(String(i))) continue;
        const blob = file.slice(i * CHUNK_SIZE, Math.min((i + 1) * CHUNK_SIZE, file.size));
        const fd = new FormData();
        fd.append('filehash', hash);
        fd.append('index', String(i));
        fd.append('file', blob);
        await apiPost('/file/upload/chunk', fd);
        upSet.add(String(i));
        pct.value = Math.round((upSet.size / total) * 100);
      }
      const fd = new FormData();
      fd.append('filehash', hash);
      fd.append('filename', file.name);
      fd.append('chunks', String(total));
      await apiPost('/file/upload/merge', fd);
    } else {
      // 小文件直传(带 filehash 走秒传检测,与分片模式 hash 一致)
      const fd = new FormData();
      fd.append('filehash', hash);
      fd.append('file', file);
      await apiUpload('/file/upload', fd, (p: number) => {
        pct.value = p;
      });
    }
    toast('上传完成', 'ok');
    emit('changed');
  } catch (e) {
    if ((e as Error).message !== 'cancelled') toast((e as Error).message, 'err');
  } finally {
    uploading.value = false;
    clear();
  }
}
</script>

<template>
  <div>
    <div
      class="upload-zone"
      :class="{ drag: dragOver, uploading }"
      @click="!uploading && fileInput?.click()"
      @dragover.prevent="dragOver = true"
      @dragleave="dragOver = false"
      @drop.prevent="onDrop"
    >
      <input ref="fileInput" type="file" hidden @change="onFileInputChange">
      <template v-if="!selected">
        <div class="uz-icon">⬆️</div>
        <div class="uz-title">{{ uploading ? '正在上传…' : '拖拽文件到这里，或点击选择' }}</div>
        <div class="uz-hint">支持断点续传 · 单文件最大 100MB</div>
      </template>
      <template v-else>
        <div class="file-preview">
          <span class="fp-icon">📄</span>
          <span class="fp-name">{{ selected.name }}</span>
          <span class="fp-size">{{ fmtSize(selected.size) }}</span>
          <button v-if="!uploading" class="fp-remove" @click.stop="clear">✕</button>
        </div>
        <div class="upload-row">
          <button v-if="!uploading" class="btn btn-primary btn-sm" @click.stop="start">开始上传</button>
          <button v-if="!uploading" class="btn btn-ghost btn-sm" @click.stop="clear">取消</button>
          <button v-if="uploading" class="btn btn-ghost btn-sm" @click.stop="cancel">取消上传</button>
        </div>
      </template>

      <div v-if="uploading" class="progress-wrap active">
        <div class="progress-head">
          <span>{{ selected?.name }}</span>
          <span>{{ pct }}%</span>
        </div>
        <div class="progress-bar"><div class="progress-fill" :style="{ width: pct + '%' }"></div></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.upload-zone {
  background: var(--surface); border: 1.5px dashed var(--border-strong);
  border-radius: var(--radius-lg); padding: 44px 24px; text-align: center;
  cursor: pointer; transition: all .2s; margin-bottom: 26px; position: relative;
}
.upload-zone:hover, .upload-zone.drag { border-color: var(--primary); background: var(--primary-soft); }
.upload-zone.drag { transform: scale(1.005); }
.upload-zone.uploading { cursor: default; }
.uz-icon { font-size: 34px; margin-bottom: 10px; opacity: .75; }
.uz-title { font-size: 15px; font-weight: 600; margin-bottom: 4px; }
.uz-hint { font-size: 12px; color: var(--text-dim); }
.file-preview {
  display: inline-flex; align-items: center; gap: 10px; margin-top: 6px;
  padding: 8px 14px; background: var(--surface-2); border-radius: var(--radius-sm);
  font-size: 13px; max-width: 100%;
}
.fp-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fp-size { color: var(--text-dim); font-family: "JetBrains Mono", monospace; font-size: 11px; flex-shrink: 0; }
.fp-remove { border: none; background: none; color: var(--text-muted); cursor: pointer; font-size: 14px; line-height: 1; }
.upload-row { display: flex; gap: 10px; margin-top: 14px; justify-content: center; }
.progress-wrap { display: none; margin-top: 16px; }
.progress-wrap.active { display: block; }
.progress-head { display: flex; justify-content: space-between; font-size: 12px; margin-bottom: 6px; color: var(--text-dim); }
.progress-bar { height: 6px; border-radius: 3px; background: var(--border); overflow: hidden; }
.progress-fill { height: 100%; border-radius: 3px; background: var(--primary); transition: width .25s ease; }
</style>
