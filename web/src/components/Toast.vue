<script setup>
import { toasts } from '../toast'
</script>

<template>
  <div class="toast-ctr">
    <div v-for="t in toasts" :key="t.id" class="toast" :class="t.type">
      <span>{{ t.type === 'ok' ? '✓' : t.type === 'err' ? '✕' : '!' }}</span>
      {{ t.msg }}
    </div>
  </div>
</template>
