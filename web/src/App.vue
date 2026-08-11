<script setup>
import { ref, onMounted } from 'vue'
import { api, session } from './api'
import { toast } from './toast'

import Sidebar from './components/Sidebar.vue'
import TopBar from './components/TopBar.vue'
import AuthView from './components/AuthView.vue'
import FileListView from './components/FileListView.vue'
import TrashView from './components/TrashView.vue'
import SearchPanel from './components/SearchPanel.vue'
import ShareManager from './components/ShareManager.vue'
import PreviewModal from './components/PreviewModal.vue'
import RenameModal from './components/RenameModal.vue'
import ConfirmModal from './components/ConfirmModal.vue'
import ShareModal from './components/ShareModal.vue'
import AISettingsModal from './components/AISettingsModal.vue'
import ToastHost from './components/Toast.vue'

// ---- 视图状态机:files | trash | search | shares ----
const authed = ref(false)
const view = ref('files')

// ---- 数据 ----
const files = ref([])
const trash = ref([])
const shares = ref([])

// ---- 全局操作目标 ----
const previewTarget = ref(null)
const renameTarget = ref(null)
const deleteTarget = ref(null)
const shareTarget = ref(null)
const aiSettingsOpen = ref(false)

// ---- 搜索 ----
const searchQuery = ref('')

async function loadFiles() {
  try { files.value = (await api('/file/query')) || [] } catch (e) { toast(e.message, 'err') }
}
async function loadTrash() {
  try { const d = await api('/file/trash?page=1&size=100'); trash.value = d?.list || [] } catch (e) { toast(e.message, 'err') }
}
async function loadShares() {
  try { shares.value = (await api('/file/share/list')) || [] } catch (e) { toast(e.message, 'err') }
}

function switchView(v) {
  view.value = v
  if (v === 'trash') loadTrash()
  if (v === 'shares') loadShares()
}
function doSearch(q) {
  searchQuery.value = q
  view.value = 'search'
}

// ---- 子组件事件 ----
function onFilesChanged() { loadFiles() }
function onTrashChanged() { loadTrash(); loadFiles() }
function onSharesChanged() { loadShares() }

onMounted(async () => {
  try {
    const u = await api('/user/info')
    session.username = u.Username || u.username || ''
    authed.value = true
    loadFiles()
  } catch { /* 未登录,停留在 AuthView */ }
})
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

    <!-- 全局弹窗 -->
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
