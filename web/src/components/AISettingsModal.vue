<script setup lang="ts">
// AI 设置:自定义 OpenAI 协议 baseURL + API key(每用户,保存后即刻生效)
import { ref, onMounted } from 'vue';
import { api, apiJSON, type AIConfigData, type AITestResult } from '../api';
import { toast } from '../toast';

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'saved'): void;
}>();

const testing = ref<boolean>(false);
const saving = ref<boolean>(false);

const configured = ref<boolean>(false);
const baseURL = ref<string>('');
const apiKey = ref<string>(''); // 输入框:空 = 保留旧 key
const apiKeyMask = ref<string>(''); // 已保存 key 的掩码(仅展示)
const hasKey = ref<boolean>(false);
const model = ref<string>('');
const embedModel = ref<string>('');

const testResult = ref<AITestResult | null>(null); // { ok, chat_ok, embed_ok, dim, dim_mismatch, error }

async function load(): Promise<void> {
  try {
    const d = await api<AIConfigData>('/ai/config');
    configured.value = !!d.configured;
    baseURL.value = d.base_url || '';
    model.value = d.model || '';
    embedModel.value = d.embed_model || '';
    hasKey.value = !!d.has_key;
    apiKeyMask.value = d.api_key_mask || '';
    apiKey.value = '';
    testResult.value = null;
  } catch (e) {
    toast((e as Error).message, 'err');
  }
}

onMounted(load);

async function testConn(): Promise<void> {
  testing.value = true;
  testResult.value = null;
  try {
    testResult.value = await apiJSON<AITestResult>('/ai/config/test', 'POST', {
      base_url: baseURL.value.trim(),
      api_key: apiKey.value.trim() || (hasKey.value ? 'sk-keep-existing' : ''),
      model: model.value.trim(),
      embed_model: embedModel.value.trim(),
    });
  } catch (e) {
    toast((e as Error).message, 'err');
  } finally {
    testing.value = false;
  }
}

async function save(): Promise<void> {
  saving.value = true;
  try {
    await apiJSON<void>('/ai/config', 'POST', {
      base_url: baseURL.value.trim(),
      api_key: apiKey.value.trim(), // 空 = 保留旧 key
      model: model.value.trim(),
      embed_model: embedModel.value.trim(),
    });
    toast('已保存，配置即刻生效');
    emit('saved');
    await load();
  } catch (e) {
    toast((e as Error).message, 'err');
  } finally {
    saving.value = false;
  }
}

async function clearConfig(): Promise<void> {
  if (!confirm('清除后回退系统默认配置（未配置时为演示模式），确定？')) return;
  try {
    await apiJSON<void>('/ai/config', 'DELETE');
    toast('已清除');
    emit('saved');
    await load();
  } catch (e) {
    toast((e as Error).message, 'err');
  }
}
</script>

<template>
  <div class="modal-mask" @click.self="emit('close')">
    <div class="modal ai-settings">
      <div class="modal-head">
        <h3>AI 设置</h3>
        <button class="modal-x" @click="emit('close')">✕</button>
      </div>

      <div class="modal-body">
        <p class="ai-tip">
          配置 OpenAI 兼容端点（OpenAI / DeepSeek / Ollama / vLLM / OneAPI 等），
          保存后文件摘要、标签与语义搜索使用你的真实 Provider；未配置时为演示模式。
        </p>

        <div v-if="configured" class="ai-mode-badge">
          当前：<b>已启用用户自定义 Provider</b>
          <span v-if="hasKey" class="key-mask">key: {{ apiKeyMask }}</span>
        </div>

        <label class="field">
          <span>Base URL（OpenAI 协议，含 /v1）</span>
          <input v-model="baseURL" placeholder="https://api.openai.com/v1（留空 = 官方默认）" />
        </label>

        <label class="field">
          <span>API Key</span>
          <input
            v-model="apiKey"
            type="password"
            :placeholder="hasKey ? '已保存（' + apiKeyMask + '），留空保持不变' : 'sk-...'"
            autocomplete="off"
          />
        </label>

        <div class="field-row">
          <label class="field">
            <span>对话模型</span>
            <input v-model="model" placeholder="gpt-4o-mini（留空 = 默认）" />
          </label>
          <label class="field">
            <span>Embedding 模型</span>
            <input v-model="embedModel" placeholder="text-embedding-3-small" />
          </label>
        </div>

        <!-- 测试结果 -->
        <div v-if="testResult" class="test-result" :class="testResult.ok ? 'ok' : 'err'">
          <template v-if="testResult.ok">
            ✅ 连接成功：对话 ✓
            <template v-if="testResult.embed_ok"> / Embedding ✓（{{ testResult.dim }} 维）</template>
            <template v-else> / Embedding ✗（语义搜索将降级）</template>
            <div v-if="testResult.dim_mismatch" class="warn">
              ⚠ 维度 {{ testResult.dim }} ≠ 索引维度，语义搜索将降级为关键词匹配（摘要功能正常）
            </div>
          </template>
          <template v-else>❌ {{ testResult.error }}</template>
        </div>

        <div class="modal-actions">
          <button class="btn" :disabled="testing || saving" @click="testConn">
            {{ testing ? '测试中…' : '测试连接' }}
          </button>
          <button v-if="configured" class="btn btn-ghost" :disabled="saving" @click="clearConfig">清除配置</button>
          <button class="btn btn-primary" :disabled="saving" @click="save">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ai-settings { width: 520px; max-width: 94vw; }
.ai-tip { font-size: 12.5px; color: var(--text-dim); line-height: 1.6; margin: 0 0 14px; }
.ai-mode-badge {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 12.5px; color: var(--primary);
  background: var(--primary-soft); border-radius: var(--radius-sm);
  padding: 8px 12px; margin-bottom: 14px;
}
.key-mask { color: var(--text-dim); font-family: monospace; }
.field-row { display: flex; gap: 12px; }
.field-row .field { flex: 1; }
.test-result {
  font-size: 13px; border-radius: var(--radius-sm); padding: 10px 12px; margin-top: 4px;
}
.test-result.ok { background: #ecfdf5; color: #059669; border: 1px solid #a7f3d0; }
.test-result.err { background: #fef2f2; color: #dc2626; border: 1px solid #fecaca; }
.test-result .warn { margin-top: 6px; color: #d97706; }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 18px; }
</style>
