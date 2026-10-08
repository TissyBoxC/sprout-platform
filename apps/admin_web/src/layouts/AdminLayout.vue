<script setup lang="ts">
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'

import { useAuthStore } from '@/features/auth/application/authStore'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

async function logout(): Promise<void> {
  await authStore.logout()
  await router.replace('/login')
}
</script>

<template>
  <div class="admin-layout">
    <aside class="admin-sidebar">
      <RouterLink class="brand" to="/">
        <img
          class="brand-avatar"
          src="/brand/sprout/brand_avatar.png"
          alt=""
          width="48"
          height="48"
        />
        <span>
          <strong>如此萌屋</strong>
          <small>品牌管理端</small>
        </span>
      </RouterLink>
      <nav class="admin-nav" aria-label="主要导航">
        <p class="nav-section">品牌管理</p>
        <RouterLink to="/">
          <span class="nav-dot" aria-hidden="true"></span>
          品牌总览
        </RouterLink>
        <RouterLink to="/families">
          <span class="nav-dot" aria-hidden="true"></span>
          家长账号
        </RouterLink>
        <RouterLink to="/children">
          <span class="nav-dot" aria-hidden="true"></span>
          儿童档案
        </RouterLink>
        <RouterLink to="/usage-reports">
          <span class="nav-dot" aria-hidden="true"></span>
          使用报告
        </RouterLink>
        <RouterLink to="/devices">
          <span class="nav-dot" aria-hidden="true"></span>
          设备管理
        </RouterLink>
        <RouterLink to="/device-diagnostics">
          <span class="nav-dot" aria-hidden="true"></span>
          设备诊断
        </RouterLink>
        <RouterLink to="/releases">
          <span class="nav-dot" aria-hidden="true"></span>
          内容发布
        </RouterLink>
        <RouterLink to="/content">
          <span class="nav-dot" aria-hidden="true"></span>
          内容库
        </RouterLink>
        <RouterLink to="/downloads">
          <span class="nav-dot" aria-hidden="true"></span>
          下载文件
        </RouterLink>
        <RouterLink to="/ui-text">
          <span class="nav-dot" aria-hidden="true"></span>
          界面文案
        </RouterLink>
        <p class="nav-section nav-section-services">服务管理</p>
        <RouterLink to="/services">
          <span class="nav-dot" aria-hidden="true"></span>
          服务版本
        </RouterLink>
        <RouterLink to="/feature-center">
          <span class="nav-dot" aria-hidden="true"></span>
          功能中心
        </RouterLink>
        <RouterLink to="/settings">
          <span class="nav-dot" aria-hidden="true"></span>
          系统设置
        </RouterLink>
      </nav>
      <div class="account-panel">
        <p>{{ authStore.account?.displayName }}</p>
        <small>{{ authStore.account?.email }}</small>
        <button type="button" @click="logout">退出登录</button>
      </div>
    </aside>
    <main class="admin-content">
      <RouterView v-slot="{ Component }">
        <Transition name="page" mode="out-in">
          <component :is="Component" :key="route.path" />
        </Transition>
      </RouterView>
    </main>
  </div>
</template>

<style scoped>
.admin-layout {
  display: grid;
  min-height: 100vh;
  grid-template-columns: 248px minmax(0, 1fr);
  background: transparent;
}

.admin-sidebar {
  position: sticky;
  top: 0;
  display: flex;
  height: 100vh;
  flex-direction: column;
  padding: 24px 18px;
  border-right: 1px solid rgb(240 189 203 / 72%);
  background: rgb(255 255 255 / 88%);
  backdrop-filter: blur(18px);
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 32px;
  color: #4a2e3b;
  text-decoration: none;
  transition: transform var(--sprout-duration-base) var(--sprout-ease-out);
}

.brand:hover {
  transform: translateY(-1px);
}

.brand-avatar {
  flex: 0 0 auto;
  border-radius: 50%;
  box-shadow: 0 8px 20px rgb(217 79 131 / 14%);
}

.brand span {
  display: grid;
  gap: 2px;
}

.brand strong {
  font-size: 18px;
}

.brand small {
  color: #6b4f5a;
  font-size: 12px;
}

.admin-nav {
  display: grid;
  gap: 8px;
}

.nav-section {
  margin: 3px 14px 1px;
  color: #6b4f5a;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.nav-section-services {
  margin-top: 14px;
}

.admin-nav a {
  position: relative;
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 11px 14px;
  color: #6b4f5a;
  text-decoration: none;
  border: 1px solid transparent;
  border-radius: 16px;
  transition:
    transform var(--sprout-duration-base) var(--sprout-ease-out),
    background-color var(--sprout-duration-base) ease,
    border-color var(--sprout-duration-base) ease,
    color var(--sprout-duration-base) ease,
    box-shadow var(--sprout-duration-base) ease;
}

.admin-nav a:hover {
  border-color: #f7d9e2;
  background: #fffafb;
  color: #b23a68;
  transform: translateX(2px);
}

.nav-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border: 1.5px solid #e7a1b6;
  border-radius: 999px;
  background: transparent;
  transition:
    width var(--sprout-duration-base) var(--sprout-ease-out),
    background-color var(--sprout-duration-base) ease,
    border-color var(--sprout-duration-base) ease,
    box-shadow var(--sprout-duration-base) ease;
}

.admin-nav a.router-link-active {
  background: #fff0f4;
  color: #c94175;
  font-weight: 700;
  box-shadow: inset 0 0 0 1px rgb(240 189 203 / 60%);
}

.admin-nav a.router-link-active .nav-dot {
  width: 18px;
  border-color: #f7a8bf;
  background: #f7a8bf;
  box-shadow: 0 4px 10px rgb(217 79 131 / 18%);
}

.account-panel {
  display: grid;
  gap: 4px;
  margin-top: auto;
  padding-top: 20px;
  border-top: 1px solid #f7d9e2;
}

.account-panel p,
.account-panel small {
  margin: 0;
  overflow-wrap: anywhere;
}

.account-panel p {
  color: #4a2e3b;
  font-weight: 700;
}

.account-panel small {
  color: #6b4f5a;
}

.account-panel button {
  min-height: 38px;
  margin-top: 10px;
  border: 1px solid #f0bdcb;
  border-radius: 19px;
  background: #ffffff;
  color: #c94175;
  font: inherit;
  cursor: pointer;
}

.account-panel button:hover {
  border-color: #e9a5b8;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

.admin-content {
  min-width: 0;
  padding: 32px;
}

@media (max-width: 760px) {
  .admin-layout {
    grid-template-columns: 1fr;
  }

  .admin-sidebar {
    position: static;
    height: auto;
    border-right: 0;
    border-bottom: 1px solid #f0bdcb;
  }

  .brand {
    margin-bottom: 18px;
  }

  .admin-nav {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .account-panel {
    margin-top: 18px;
  }

  .admin-content {
    padding: 22px 16px;
  }
}
</style>
