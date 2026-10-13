<script setup lang="ts">
import AdminLayout from '@/layouts/AdminLayout.vue'
import { useRoute } from 'vue-router'
import { computed, ref } from 'vue'

import { useAuthStore } from '@/features/auth/application/authStore'

const authStore = useAuthStore()
const route = useRoute()
const isLoginRoute = computed(() => route.meta.public === true)
const isRetrying = ref(false)

async function retryRestore(): Promise<void> {
  isRetrying.value = true
  await authStore.restoreSession()
  isRetrying.value = false
}
</script>

<template>
  <Transition name="page" mode="out-in">
    <RouterView v-if="isLoginRoute" />
    <main v-else-if="authStore.error && !authStore.isSignedIn" class="startup-state">
      <section class="startup-panel" role="alert">
        <p class="startup-eyebrow">无法恢复登录状态</p>
        <h1>暂时无法连接管理服务</h1>
        <p>{{ authStore.error.message }}</p>
        <button type="button" :disabled="isRetrying" @click="retryRestore">
          {{ isRetrying ? '正在重试…' : '重新连接' }}
        </button>
      </section>
    </main>
    <AdminLayout v-else />
  </Transition>
</template>

<style scoped>
.startup-state {
  display: grid;
  min-height: 100vh;
  place-items: center;
  padding: 24px;
  background: var(--sprout-surface);
}

.startup-panel {
  display: grid;
  width: min(100%, 520px);
  gap: 14px;
  padding: 32px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 16px 40px rgb(194 91 128 / 10%);
}

.startup-eyebrow {
  margin: 0;
  color: var(--sprout-pink-strong);
  font-size: 13px;
  font-weight: 700;
}

h1 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 26px;
}

p {
  margin: 0;
  color: var(--sprout-text-muted);
}

button {
  justify-self: start;
  min-height: 40px;
  padding: 0 18px;
  border: 1px solid #d94f83;
  border-radius: var(--sprout-radius-control);
  background: #d94f83;
  color: #ffffff;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button:disabled {
  cursor: wait;
  opacity: 0.6;
}
</style>
