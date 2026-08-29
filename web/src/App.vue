<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, session, type FileItem, type ShareItem, type UserInfo, type PagedList } from './api';
import { toast } from './toast';

import Sidebar from './components/Sidebar.vue';
import TopBar from './components/TopBar.vue';
import AuthView from './components/AuthView.vue';
import FileListView from './components/FileListView.vue';
import TrashView from './components/TrashView.vue';
import SearchPanel from './components/SearchPanel.vue';
import ShareManager from './components/ShareManager.vue';
import PreviewModal from './components/PreviewModal.vue';
import RenameModal from './components/RenameModal.vue';
import ConfirmModal from './components/ConfirmModal.vue';
import ShareModal from './components/ShareModal.vue';
import AISettingsModal from './components/AISettingsModal.vue';
import ToastHost from './components/Toast.vue';

// ---- View state machine: files | trash | search | shares ----
const authed = ref<boolean>(false);
const view = ref<string>('files');

// ---- Data ----
const files = ref<FileItem[]>([]);
const trash = ref<FileItem[]>([]);
const shares = ref<ShareItem[]>([]);

// ---- Global action targets ----
const previewTarget = ref<FileItem | null>(null);
const renameTarget = ref<FileItem | null>(null);
const deleteTarget = ref<FileItem | null>(null);
const shareTarget = ref<FileItem | null>(null);
const aiSettingsOpen = ref<boolean>(false);

// ---- Search ----
const searchQuery = ref<string>('');

async function loadFiles(): Promise<void> {
  try {
    const d = await api<FileItem[]>('/file/query');
    files.value = d || [];
  } catch (e) {
    toast((e as Error).message, 'err');
  }
}
async function loadTrash(): Promise<void> {
  try {
    const d = await api<PagedList<FileItem>>('/file/trash?page=1&size=100');
    trash.value = d?.list || [];
  } catch (e) {
    toast((e as Error).message, 'err');
  }
}
async function loadShares(): Promise<void> {
  try {
    const d = await api<ShareItem[]>('/file/share/list');
    shares.value = d || [];
  } catch (e) {
    toast((e as Error).message, 'err');
  }
}

function switchView(v: string): void {
  view.value = v;
  if (v === 'trash') loadTrash();
  if (v === 'shares') loadShares();
}
function doSearch(q: string): void {
  searchQuery.value = q;
  view.value = 'search';
}

// ---- Child component events ----
function onFilesChanged(): void {
  loadFiles();
}
function onTrashChanged(): void {
  loadTrash();
  loadFiles();
}
function onSharesChanged(): void {
  loadShares();
}

onMounted(async () => {
  try {
    const u = await api<UserInfo>('/user/info');
    session.username = u.Username || u.username || '';
    authed.value = true;
    loadFiles();
  } catch {
    /* Not logged in, stay on AuthView */
  }
});
</script>

<template>
  <AuthView v-if="!authed" @authed="authed = true; loadFiles()" />

  <div v-else class="layout">
    <Sidebar :view="view" @navigate="switchView" @settings="aiSettingsOpen = true" @logout="authed = false; view = 'files'" />

    <div class="main">
      <TopBar @search="doSearch" />
      <main class="content">
        <FileListView
          v-if="view === 'files'"
          :files="files"
          @changed="onFilesChanged"
          @preview="previewTarget = $event"
          @rename="renameTarget = $event"
          @delete="deleteTarget = $event"
          @share="shareTarget = $event"
          @similar="doSearch"
        />
        <TrashView v-else-if="view === 'trash'" :trash="trash" @changed="onTrashChanged" />
        <SearchPanel v-else-if="view === 'search'" :query="searchQuery" @similar="doSearch" />
        <ShareManager v-else :shares="shares" @changed="onSharesChanged" />
      </main>
    </div>

    <!-- Global modals -->
    <PreviewModal v-if="previewTarget" :file="previewTarget" @close="previewTarget = null" />
    <RenameModal v-if="renameTarget" :file="renameTarget" @close="renameTarget = null" @done="renameTarget = null; onFilesChanged()" />
    <ConfirmModal
      v-if="deleteTarget"
      :file="deleteTarget"
      kind="delete"
      @close="deleteTarget = null"
      @done="deleteTarget = null; onFilesChanged()"
    />
    <ShareModal v-if="shareTarget" :file="shareTarget" @close="shareTarget = null" />
    <AISettingsModal v-if="aiSettingsOpen" @close="aiSettingsOpen = false" />
    <ToastHost />
  </div>
</template>

<style scoped>
.layout { display: flex; min-height: 100vh; }
.main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.content { flex: 1; width: 100%; max-width: 1120px; margin: 0 auto; padding: 24px 28px 72px; }
</style>
