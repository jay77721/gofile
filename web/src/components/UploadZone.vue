<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue';
import {
  api,
  apiPost,
  apiUpload,
  makeApiError,
  multipartApi,
  uploadPart,
  type FileItem,
  type MultipartInitResp,
} from '../api';
import { toast } from '../toast';
import { CHUNK_MIN, CHUNK_SIZE, MAX_FILE, fmtSize, sha1HeadTail } from '../utils';

const props = withDefaults(defineProps<{ parentId?: number }>(), { parentId: 0 });
const emit = defineEmits<{ (e: 'changed'): void }>();

type UploadPhase = 'idle' | 'hashing' | 'initializing' | 'uploading' | 'completing' | 'error' | 'cancelled';

const selected = ref<File | null>(null);
const dragOver = ref(false);
const uploading = ref(false);
const pct = ref(0);
const phase = ref<UploadPhase>('idle');
const errorMessage = ref('');
const cancelFlag = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const activeRequest = ref<AbortController | null>(null);

const phaseLabel = computed(() => {
  switch (phase.value) {
    case 'hashing': return '正在校验文件';
    case 'initializing': return '正在准备分片上传';
    case 'uploading': return '正在上传';
    case 'completing': return '正在合并文件';
    default: return '拖拽文件到这里，或点击选择';
  }
});

function pick(file?: File | null): void {
  if (uploading.value) return;
  if (!file) return;
  if (file.size > MAX_FILE) {
    toast('文件过大（最大 100MB）', 'err');
    return;
  }
  selected.value = file;
  phase.value = 'idle';
  errorMessage.value = '';
  pct.value = 0;
}

function onDrop(event: DragEvent): void {
  if (uploading.value) return;
  dragOver.value = false;
  if (event.dataTransfer?.files.length) pick(event.dataTransfer.files[0]);
}

function onFileInputChange(event: Event): void {
  if (uploading.value) return;
  const target = event.target as HTMLInputElement | null;
  if (target?.files?.length) pick(target.files[0]);
}

function clear(): void {
  selected.value = null;
  phase.value = 'idle';
  errorMessage.value = '';
  pct.value = 0;
  cancelFlag.value = false;
  if (fileInput.value) fileInput.value.value = '';
}

function isCancelled(error: unknown): boolean {
  return cancelFlag.value || (error instanceof Error && error.message === 'cancelled');
}

function checkCancelled(): void {
  if (cancelFlag.value) throw new Error('cancelled');
}

function cancel(): void {
  if (!uploading.value) return;
  cancelFlag.value = true;
  activeRequest.value?.abort();
}

async function uploadLegacy(file: File, hash: string, signal: AbortSignal): Promise<void> {
  if (props.parentId !== 0) {
    throw makeApiError('当前存储不支持目录内分片直传，请切换到 MinIO/S3', 1009);
  }

  if (file.size >= CHUNK_MIN) {
    const uploaded = new Set<string>();
    try {
      const statusList = await api<string[] | number[]>(`/file/upload/status?filehash=${encodeURIComponent(hash)}`, signal);
      (statusList || []).forEach((index) => uploaded.add(String(index)));
    } catch {
      // 没有历史分片时从头上传。
    }

    const total = Math.ceil(file.size / CHUNK_SIZE);
    for (let index = 0; index < total; index += 1) {
      checkCancelled();
      if (uploaded.has(String(index))) continue;
      const blob = file.slice(index * CHUNK_SIZE, Math.min((index + 1) * CHUNK_SIZE, file.size));
      const form = new FormData();
      form.append('filehash', hash);
      form.append('index', String(index));
      form.append('file', blob);
      phase.value = 'uploading';
      await apiPost('/file/upload/chunk', form, signal);
      uploaded.add(String(index));
      pct.value = Math.round((uploaded.size / total) * 100);
    }

    const mergeForm = new FormData();
    mergeForm.append('filehash', hash);
    mergeForm.append('filename', file.name);
    mergeForm.append('chunks', String(total));
    phase.value = 'completing';
    await apiPost('/file/upload/merge', mergeForm, signal);
    return;
  }

  phase.value = 'uploading';
  const form = new FormData();
  form.append('filehash', hash);
  form.append('file', file);
  await apiUpload('/file/upload', form, (progress) => { pct.value = progress; }, signal);
}

async function uploadMultipart(file: File, hash: string, signal: AbortSignal): Promise<boolean> {
  phase.value = 'initializing';
  let init: MultipartInitResp | null = null;
  let uploadId = '';
  let completing = false;

  try {
    init = await multipartApi.init({
      filehash: hash,
      filename: file.name,
      filesize: file.size,
      chunk_size: CHUNK_SIZE,
      parent_id: props.parentId,
    }, signal);
    checkCancelled();
    if (init.fast_upload) {
      pct.value = 100;
      return true;
    }

    uploadId = init.upload_id || '';
    const urls = init.part_urls || [];
    const count = init.chunk_count || urls.length;
    if (!uploadId || count < 1 || urls.length !== count) {
      throw makeApiError('服务器返回的分片上传信息不完整', 1007);
    }

    const parts: { part_number: number; etag: string }[] = [];
    for (let index = 0; index < count; index += 1) {
      checkCancelled();
      const partSize = init.chunk_size || CHUNK_SIZE;
      const blob = file.slice(index * partSize, Math.min((index + 1) * partSize, file.size));
      phase.value = 'uploading';
      const etag = await uploadPart(urls[index], blob, signal, (partProgress) => {
        pct.value = Math.round(((index + partProgress / 100) / count) * 100);
      });
      parts.push({ part_number: index + 1, etag });
      pct.value = Math.round((parts.length / count) * 100);
    }

    checkCancelled();
    completing = true;
    phase.value = 'completing';
    await multipartApi.complete({ upload_id: uploadId, parts }, signal);
    return true;
  } catch (error) {
    if (uploadId) {
      try { await multipartApi.abort(uploadId); } catch { /* 过期清理任务兜底 */ }
    }
    if (isCancelled(error)) throw error;
    // 初始化或分片直传不可用时，回退到已有应用服务分片接口。
    // 已发起合并则不回退，避免重复创建文件记录。
    if (!completing) return false;
    throw error;
  }
}

async function start(): Promise<void> {
  if (!selected.value || uploading.value) return;
  const file = selected.value;
  uploading.value = true;
  cancelFlag.value = false;
  errorMessage.value = '';
  phase.value = 'hashing';
  pct.value = 0;
  const controller = new AbortController();
  activeRequest.value = controller;

  try {
    const hash = await sha1HeadTail(file);
    try {
      await api<FileItem>(`/file/meta?filehash=${encodeURIComponent(hash)}`, controller.signal);
      pct.value = 100;
      toast('文件已存在，已完成秒传', 'ok');
      emit('changed');
      clear();
      return;
    } catch {
      // 未命中当前用户文件，继续上传。
    }

    checkCancelled();
    if (file.size >= CHUNK_MIN || props.parentId !== 0) {
      const completed = await uploadMultipart(file, hash, controller.signal);
      if (!completed) await uploadLegacy(file, hash, controller.signal);
    } else {
      await uploadLegacy(file, hash, controller.signal);
    }

    pct.value = 100;
    toast('上传完成', 'ok');
    emit('changed');
    clear();
  } catch (error) {
    if (isCancelled(error)) {
      phase.value = 'cancelled';
      errorMessage.value = '上传已取消，可以重试或移除文件。';
    } else {
      phase.value = 'error';
      errorMessage.value = error instanceof Error ? error.message : '上传失败，请稍后重试。';
      toast(errorMessage.value, 'err');
    }
  } finally {
    uploading.value = false;
    if (activeRequest.value === controller) activeRequest.value = null;
    cancelFlag.value = false;
  }
}

onUnmounted(() => {
  activeRequest.value?.abort();
});
</script>

<template>
  <div>
    <div
      class="upload-zone"
      :class="{ drag: dragOver, uploading }"
      @click="!uploading && !selected && fileInput?.click()"
      @dragover.prevent="dragOver = true"
      @dragleave="dragOver = false"
      @drop.prevent="onDrop"
    >
      <input ref="fileInput" type="file" hidden @change="onFileInputChange">
      <template v-if="!selected">
        <div class="uz-icon" aria-hidden="true">⇧</div>
        <div class="uz-title">{{ phaseLabel }}</div>
        <div class="uz-hint">支持断点续传 · 单文件最大 100MB</div>
      </template>
      <template v-else>
        <div class="file-preview">
          <span class="fp-icon" aria-hidden="true">▣</span>
          <span class="fp-name" :title="selected.name">{{ selected.name }}</span>
          <span class="fp-size">{{ fmtSize(selected.size) }}</span>
          <button v-if="!uploading" type="button" class="fp-remove" aria-label="移除文件" @click.stop="clear">×</button>
        </div>
        <div class="upload-row">
          <button v-if="!uploading && phase !== 'error' && phase !== 'cancelled'" type="button" class="btn btn-primary btn-sm" @click.stop="start">开始上传</button>
          <button v-if="!uploading && (phase === 'error' || phase === 'cancelled')" type="button" class="btn btn-primary btn-sm" @click.stop="start">重试上传</button>
          <button v-if="!uploading" type="button" class="btn btn-ghost btn-sm" @click.stop="clear">移除</button>
          <button v-if="uploading" type="button" class="btn btn-ghost btn-sm" @click.stop="cancel">取消上传</button>
        </div>
      </template>

      <div v-if="uploading" class="progress-wrap active" aria-live="polite">
        <div class="progress-head"><span>{{ phaseLabel }}</span><span>{{ pct }}%</span></div>
        <div class="progress-bar" role="progressbar" :aria-valuenow="pct" aria-valuemin="0" aria-valuemax="100">
          <div class="progress-fill" :style="{ width: pct + '%' }"></div>
        </div>
      </div>
      <div v-if="errorMessage" class="upload-feedback" :class="{ cancelled: phase === 'cancelled' }" role="alert">
        {{ errorMessage }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.upload-zone {
  background: var(--surface); border: 1.5px dashed var(--border-strong);
  border-radius: var(--radius-lg); padding: 44px 24px; text-align: center;
  cursor: pointer; transition: border-color .2s, background .2s, transform .2s;
  margin-bottom: 26px; position: relative;
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
.fp-remove { border: none; background: none; color: var(--text-muted); cursor: pointer; font-size: 20px; line-height: 1; padding: 4px; }
.upload-row { display: flex; gap: 10px; margin-top: 14px; justify-content: center; }
.progress-wrap { display: none; margin-top: 16px; }
.progress-wrap.active { display: block; }
.progress-head { display: flex; justify-content: space-between; font-size: 12px; margin-bottom: 6px; color: var(--text-dim); }
.progress-bar { height: 6px; border-radius: 3px; background: var(--border); overflow: hidden; }
.progress-fill { height: 100%; border-radius: 3px; background: var(--primary); transition: width .25s ease; }
.upload-feedback { margin-top: 14px; color: var(--red); font-size: 12px; line-height: 1.5; }
.upload-feedback.cancelled { color: var(--amber); }
.fp-remove:focus-visible, .btn:focus-visible { outline: 3px solid rgba(37, 99, 235, .28); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { .upload-zone, .progress-fill { transition: none; } }
</style>
