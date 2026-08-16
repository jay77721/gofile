<script setup lang="ts">
import { ref } from 'vue';

const emit = defineEmits<{
  (e: 'search', text: string): void;
}>();

const q = ref<string>('');

function submit(): void {
  const text = q.value.trim();
  if (!text) return;
  emit('search', text);
  q.value = '';
}
</script>

<template>
  <header class="topbar">
    <div class="top-search">
      <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/>
      </svg>
      <input
        v-model="q"
        placeholder="用自然语言找文件，如「最近3天上传的PDF」「数据库优化」…"
        @keydown.enter="submit"
      >
      <button class="btn btn-primary btn-sm" @click="submit">搜索</button>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  position: sticky; top: 0; z-index: 50;
  display: flex; align-items: center; justify-content: center;
  padding: 10px 28px; background: rgba(246, 247, 249, .88);
  backdrop-filter: blur(10px); border-bottom: 1px solid var(--border);
}
.top-search { display: flex; align-items: center; gap: 8px; width: 100%; max-width: 640px; }
.search-icon { width: 16px; height: 16px; color: var(--text-muted); flex-shrink: 0; }
.top-search input {
  flex: 1; padding: 9px 12px; border-radius: var(--radius-sm);
  border: 1px solid var(--border); background: var(--surface);
  color: var(--text); font-size: 13px; font-family: inherit; outline: none;
  transition: border-color .15s, box-shadow .15s;
}
.top-search input:focus { border-color: var(--primary); box-shadow: 0 0 0 3px rgba(37, 99, 235, .12); }
</style>
