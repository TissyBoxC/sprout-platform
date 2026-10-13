<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

function goBack(): void {
  if (globalThis.history.length > 1) {
    router.back()
    return
  }
  void router.replace('/')
}
</script>

<template>
  <section class="state-panel state-error" role="alert">
    <p class="eyebrow">权限不足</p>
    <h1>你没有权限访问此页面</h1>
    <p>当前账号没有打开这个功能所需的权限，请联系管理员调整后重试。</p>
    <button type="button" class="primary" @click="goBack">返回上一页</button>
  </section>
</template>

<style scoped>
.state-panel {
  display: grid;
  max-width: 560px;
  gap: 14px;
  padding: 32px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.eyebrow {
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

button:hover {
  background: #c94175;
}
</style>
