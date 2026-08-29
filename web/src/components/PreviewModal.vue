<script setup lang="ts">
import { ref, computed } from 'vue';
import { extOf, fmtSize } from '../utils';
import type { FileItem } from '../api';

type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text';

interface Props {
  file: FileItem;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  (e: 'close'): void;
}>();

const text = ref<string>('');
const textLoading = ref<boolean>(false);

const ext = computed<string>(() => extOf(props.file.filename));
const kind = computed<PreviewKind>(() => {
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext.value)) return 'image';
  if (['mp4', 'webm', 'mov'].includes(ext.value)) return 'video';
  if (['mp3', 'wav', 'ogg', 'flac'].includes(ext.value)) return 'audio';
  if (ext.value === 'pdf') return 'pdf';
  return 'text';
});

const src = computed<string>(() => '/file/preview?filehash=' + encodeURIComponent(props.file.filehash));

// Async load for text types
if (kind.value === 'text') {
  textLoading.value = true;
  fetch(src.value, { credentials: 'include' })
    .then((r) => (r.status === 401 ? Promise.reject(new Error('未登录')) : r.text()))
    .then((t) => {
      text.value = t;
    })
    .catch((e) => {
      text.value = '加载失败：' + (e as Error).message;
    })
    .finally(() => {
      textLoading.value = false;
    });
}

const icon = computed<string>(() => {
  const map: Record<PreviewKind, string> = {
    image: '🖼',
    video: '🎥',
    audio: '🎵',
    pdf: '📄',
    text: '📝',
  };
  return map[kind.value] || '📄';
});
</script>

<template>
  <div class="modal-mask on preview-mask" @click.self="emit('close')">
    <div class="preview-modal">
      <div class="pv-header">
        <div class="pv-title">
          <span class="pv-icon">{{ icon }}</span>
          <div>
            <div class="pv-fname">{{ file.filename }}</div>
            <div class="pv-meta">{{ fmtSize(file.size) }}</div>
          </div>
        </div>
        <button class="pv-close" @click="emit('close')">✕</button>
      </div>

      <div class="pv-body">
        <!-- Image -->
        <img v-if="kind === 'image'" :src="src" alt="preview" class="pv-image">
        <!-- Video (backend supports Range, seekable) -->
        <video v-else-if="kind === 'video'" :src="src" class="pv-media" controls autoplay playsinline></video>
        <!-- Audio -->
        <audio v-else-if="kind === 'audio'" :src="src" class="pv-audio" controls></audio>
        <!-- PDF -->
        <iframe v-else-if="kind === 'pdf'" :src="src" class="pv-pdf"></iframe>
        <!-- Text -->
        <pre v-else class="pv-text">{{ textLoading ? '加载中…' : text }}</pre>
      </div>
    </div>
  </div>
</template>

<style scoped>
.preview-mask { align-items: center; }
.preview-modal {
  display: flex; flex-direction: column; width: 92vw; max-width: 880px; max-height: 90vh;
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius-lg); overflow: hidden; box-shadow: var(--shadow-lg);
  animation: modalIn .18s ease;
}
@keyframes modalIn { from { opacity: 0; transform: scale(.97); } to { opacity: 1; transform: none; } }
.pv-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 18px; border-bottom: 1px solid var(--border);
}
.pv-title { display: flex; align-items: center; gap: 12px; min-width: 0; }
.pv-icon { font-size: 26px; line-height: 1; }
.pv-fname { font-size: 14px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 420px; }
.pv-meta { font-size: 11px; color: var(--text-muted); font-family: "JetBrains Mono", monospace; }
.pv-close {
  border: none; background: var(--surface-2); color: var(--text-dim);
  width: 30px; height: 30px; border-radius: 8px; cursor: pointer; font-size: 13px;
  transition: all .15s;
}
.pv-close:hover { background: var(--surface-hover); color: var(--text); }
.pv-body { flex: 1; min-height: 0; overflow: auto; background: #fafbfc; }
.pv-image { display: block; max-width: 100%; max-height: 74vh; margin: 0 auto; }
.pv-media { display: block; width: 100%; max-height: 74vh; background: #000; }
.pv-audio { display: block; width: 100%; padding: 24px; }
.pv-pdf { width: 100%; height: 74vh; border: none; }
.pv-text {
  padding: 18px 22px; font-family: "JetBrains Mono", monospace; font-size: 12.5px;
  line-height: 1.7; color: var(--text); white-space: pre-wrap; word-break: break-all;
}
</style>
