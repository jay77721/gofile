<script setup lang="ts">
import { ref, watch, onMounted } from 'vue';
import { api, type FileItem, type PagedList, type AIConfigData } from '../api';
import { fmtSize, canPreview } from '../utils';

interface Props {
  query?: string;
}

const props = withDefaults(defineProps<Props>(), {
  query: '',
});

const emit = defineEmits<{
  (e: 'similar', query: string): void;
}>();

const q = ref<string>(props.query);
const results = ref<FileItem[]>([]);
const loading = ref<boolean>(false);
const msg = ref<string>('');
const aiMode = ref<string>(''); // '' 未知 | openai 真实 Provider | mock 演示模式

onMounted(async () => {
  try {
    const d = await api<AIConfigData>('/ai/config');
    aiMode.value = d?.mode === 'openai' ? 'openai' : 'mock';
  } catch {
    /* AI 未启用时保持未知 */
  }
});

async function search(text?: string): Promise<void> {
  const query = (text ?? q.value).trim();
  if (!query) return;
  q.value = query;
  loading.value = true;
  msg.value = '';
  try {
    const d = await api<PagedList<FileItem>>('/file/ai/search?q=' + encodeURIComponent(query) + '&page=1&size=20');
    results.value = d?.list || [];
    if (!results.value.length) msg.value = '没有找到相关文件，换个说法试试';
  } catch (e) {
    results.value = [];
    msg.value = /Unexpected|Failed to fetch|404/.test((e as Error).message)
      ? 'AI 搜索未启用，请在服务端设置 AI_ENABLED=true 后重试'
      : (e as Error).message;
  } finally {
    loading.value = false;
  }
}

// App 侧(相似推荐等)传入新查询时自动触发
watch(
  () => props.query,
  (v) => {
    if (v) search(v);
  },
  { immediate: false }
);

if (props.query) search(props.query);
</script>

<template>
  <div class="search-view">
    <div class="search-head">
      <h3>AI 语义搜索</h3>
      <span class="hint">支持自然语言：时间 + 类型 + 语义组合，如「最近3天上传的PDF」</span>
      <span v-if="aiMode === 'openai'" class="mode-badge real">真实 Provider</span>
      <span v-else-if="aiMode === 'mock'" class="mode-badge">演示模式</span>
    </div>

    <div class="search-bar">
      <input v-model="q" placeholder="输入自然语言查询…" @keydown.enter="search()">
      <button class="btn btn-primary" :disabled="loading" @click="search()">{{ loading ? '搜索中…' : '搜索' }}</button>
    </div>

    <div v-if="msg" class="search-msg">{{ msg }}</div>

    <div v-if="results.length" class="file-grid">
      <div v-for="f in results" :key="f.filehash" class="file-card">
        <div class="fc-name" :title="f.filename">{{ f.filename }}</div>
        <div v-if="f.summary" class="fc-summary" :title="f.summary">{{ f.summary }}</div>
        <div v-if="f.tags" class="fc-tags">
          <span v-for="t in String(f.tags).split(',')" :key="t" class="tag">{{ t.trim() }}</span>
        </div>
        <div class="fc-meta">
          <span>{{ fmtSize(f.size) }}</span>
          <span v-if="f.score != null" class="score">相似度 {{ Math.round(f.score * 100) }}%</span>
        </div>
        <div class="fc-actions">
          <a class="act" :href="'/file/preview?filehash=' + encodeURIComponent(f.filehash)" target="_blank" v-if="canPreview(f.filename)">预览</a>
          <a class="act" :href="'/file/download?filehash=' + encodeURIComponent(f.filehash)">下载</a>
          <button class="act" @click="emit('similar', '与「' + f.filename + '」相似的文件')">找相似</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.search-head { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.search-head h3 { font-size: 15px; font-weight: 600; }
.search-head .hint { font-size: 12px; color: var(--text-muted); }
.mode-badge {
  font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 999px;
  background: var(--surface-hover); color: var(--text-dim);
}
.mode-badge.real { background: var(--primary-soft); color: var(--primary); }
.search-bar { display: flex; gap: 8px; margin-bottom: 16px; }
.search-bar input {
  flex: 1; padding: 10px 14px; border-radius: var(--radius-sm);
  border: 1px solid var(--border); background: var(--surface); color: var(--text);
  font-size: 14px; font-family: inherit; outline: none;
  transition: border-color .15s, box-shadow .15s;
}
.search-bar input:focus { border-color: var(--primary); box-shadow: 0 0 0 3px rgba(37, 99, 235, .12); }
.search-msg { font-size: 13px; color: var(--text-dim); padding: 10px 2px; }

.file-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
.file-card {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  padding: 14px; display: flex; flex-direction: column; gap: 8px; transition: all .18s;
}
.file-card:hover { border-color: var(--border-strong); box-shadow: var(--shadow); transform: translateY(-1px); }
.fc-name { font-size: 13.5px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fc-summary {
  font-size: 12px; color: var(--text-dim); line-height: 1.5;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.fc-tags { display: flex; gap: 5px; flex-wrap: wrap; }
.fc-tags .tag { background: var(--surface-2); color: var(--text-dim); font-size: 11px; padding: 1px 8px; border-radius: 10px; }
.fc-meta { display: flex; gap: 12px; font-size: 11px; color: var(--text-muted); font-family: "JetBrains Mono", monospace; }
.fc-meta .score { color: var(--primary); }
.fc-actions { display: flex; gap: 6px; margin-top: auto; }
.fc-actions .act {
  flex: 1; padding: 5px 6px; border-radius: 6px; border: 1px solid var(--border);
  background: var(--surface); color: var(--text-dim); cursor: pointer;
  font-size: 11px; font-weight: 500; font-family: inherit; text-align: center; text-decoration: none;
  transition: all .15s;
}
.fc-actions .act:hover { background: var(--surface-hover); color: var(--text); }
</style>
