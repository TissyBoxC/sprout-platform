<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const accountInitial = computed(() => {
  const displayName = authStore.account?.displayName.trim() ?? ''
  return displayName.slice(0, 1).toUpperCase() || '管'
})

async function logout(): Promise<void> {
  await authStore.logout()
  await router.replace('/login')
}
</script>

<template>
  <div class="admin-layout">
    <aside class="admin-sidebar">
      <div class="sidebar-brand">
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
      </div>
      <nav class="admin-nav" aria-label="主要导航">
        <div class="admin-nav-inner">
        <p class="nav-section">品牌管理</p>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/">
          <span class="nav-dot" aria-hidden="true"></span>
          品牌总览
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.familyRead)" to="/families">
          <span class="nav-dot" aria-hidden="true"></span>
          家长账号
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.familyRead)" to="/children">
          <span class="nav-dot" aria-hidden="true"></span>
          儿童档案
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.familyRead)" to="/usage-reports">
          <span class="nav-dot" aria-hidden="true"></span>
          使用报告
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.notificationsRead)" to="/notifications">
          <span class="nav-dot" aria-hidden="true"></span>
          消息通知
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.devicesRead)" to="/devices">
          <span class="nav-dot" aria-hidden="true"></span>
          设备管理
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.devicesRead)" to="/device-diagnostics">
          <span class="nav-dot" aria-hidden="true"></span>
          设备诊断
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/vision">
          <span class="nav-dot" aria-hidden="true"></span>
          摄像头与视觉
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/display">
          <span class="nav-dot" aria-hidden="true"></span>
          屏幕与护眼
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/screen">
          <span class="nav-dot" aria-hidden="true"></span>
          屏幕能力
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/touch">
          <span class="nav-dot" aria-hidden="true"></span>
          触摸交互
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/cellular">
          <span class="nav-dot" aria-hidden="true"></span>
          蜂窝网络
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/power">
          <span class="nav-dot" aria-hidden="true"></span>
          电池与省电
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/motion">
          <span class="nav-dot" aria-hidden="true"></span>
          动作与电机
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/form-factor">
          <span class="nav-dot" aria-hidden="true"></span>
          形态能力
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/video-companion">
          <span class="nav-dot" aria-hidden="true"></span>
          视频陪伴
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/video-session">
          <span class="nav-dot" aria-hidden="true"></span>
          视频会话
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/bluetooth-audio">
          <span class="nav-dot" aria-hidden="true"></span>
          蓝牙音箱
        </RouterLink>
        <RouterLink
          v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)"
          to="/learning-visualization"
        >
          <span class="nav-dot" aria-hidden="true"></span>
          学习可视化
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/image-privacy">
          <span class="nav-dot" aria-hidden="true"></span>
          图片隐私
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/wearable">
          <span class="nav-dot" aria-hidden="true"></span>
          穿戴与定位
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.statsRead)" to="/multi-device">
          <span class="nav-dot" aria-hidden="true"></span>
          多设备
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.auditRead)" to="/audit">
          <span class="nav-dot" aria-hidden="true"></span>
          操作审计
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.storageRead)" to="/releases">
          <span class="nav-dot" aria-hidden="true"></span>
          内容发布
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.otaRead)" to="/device-ota">
          <span class="nav-dot" aria-hidden="true"></span>
          设备 OTA
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.contentRead)" to="/content">
          <span class="nav-dot" aria-hidden="true"></span>
          内容库
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.contentRead)" to="/content/theme-packs">
          <span class="nav-dot" aria-hidden="true"></span>
          主题包
        </RouterLink>
        <RouterLink
          v-if="authStore.can(ADMIN_PERMISSIONS.contentRead)"
          to="/content/offline-policy"
        >
          <span class="nav-dot" aria-hidden="true"></span>
          离线下载策略
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.storageRead)" to="/downloads">
          <span class="nav-dot" aria-hidden="true"></span>
          下载文件
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.contentRead)" to="/ui-text">
          <span class="nav-dot" aria-hidden="true"></span>
          界面文案
        </RouterLink>
        <p class="nav-section nav-section-services">服务管理</p>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.serviceRead)" to="/services">
          <span class="nav-dot" aria-hidden="true"></span>
          服务版本
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.featureCenterRead)" to="/feature-center">
          <span class="nav-dot" aria-hidden="true"></span>
          功能中心
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.settingsRead)" to="/settings">
          <span class="nav-dot" aria-hidden="true"></span>
          系统设置
        </RouterLink>
        <RouterLink v-if="authStore.can(ADMIN_PERMISSIONS.adminsRead)" to="/admin-permissions">
          <span class="nav-dot" aria-hidden="true"></span>
          管理员权限
        </RouterLink>
        </div>
      </nav>
      <div class="sidebar-account">
        <div class="account-identity">
          <span class="account-avatar" aria-hidden="true">
            {{ accountInitial }}
          </span>
          <span class="account-copy">
            <strong>{{ authStore.account?.displayName || '管理员' }}</strong>
            <small>{{ authStore.account?.email }}</small>
          </span>
        </div>
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
  grid-template-columns: 272px minmax(0, 1fr);
  gap: 20px;
  padding: 16px;
  background: transparent;
}

.admin-sidebar {
  position: sticky;
  top: 16px;
  display: flex;
  height: calc(100vh - 32px);
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 24px;
  background: rgb(255 255 255 / 96%);
  box-shadow: 0 18px 44px rgb(143 69 94 / 10%);
}

.sidebar-brand {
  flex: 0 0 auto;
  padding: 18px 20px 14px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
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
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 2px 12px 16px;
  scrollbar-color: rgb(240 189 203 / 75%) transparent;
  scrollbar-width: thin;
}

.admin-nav-inner {
  display: grid;
  gap: 5px;
}

.nav-section {
  margin: 7px 11px 3px;
  color: #6b4f5a;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.nav-section-services {
  margin-top: 18px;
}

.admin-nav a {
  position: relative;
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 10px 13px;
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

.sidebar-account {
  flex: 0 0 auto;
  display: grid;
  gap: 12px;
  margin: 0 12px 12px;
  padding: 14px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 18px;
  background: #fff8fa;
}

.account-identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.account-avatar {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 12px;
  background: #ffe7ee;
  color: var(--sprout-pink-deep);
  font-size: 14px;
  font-weight: 800;
}

.account-copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.account-copy strong,
.account-copy small {
  margin: 0;
  overflow-wrap: anywhere;
}

.account-copy strong {
  color: #4a2e3b;
  font-weight: 700;
}

.account-copy small {
  color: #6b4f5a;
  font-size: 11px;
}

.sidebar-account button {
  min-height: 38px;
  border: 1px solid #f0bdcb;
  border-radius: 19px;
  background: #ffffff;
  color: #c94175;
  font: inherit;
  cursor: pointer;
}

.sidebar-account button:hover {
  border-color: #e9a5b8;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

.admin-content {
  min-width: 0;
  padding: 16px 16px 32px 0;
}

@media (max-width: 760px) {
  .admin-layout {
    grid-template-columns: 1fr;
    gap: 14px;
    padding: 12px;
  }

  .admin-sidebar {
    position: static;
    height: auto;
    overflow: visible;
    border-radius: 22px;
  }

  .sidebar-brand {
    padding: 16px 16px 12px;
  }

  .admin-nav {
    max-height: min(52vh, 430px);
    overflow-y: auto;
    padding: 0 10px 12px;
  }

  .admin-nav-inner {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .nav-section {
    grid-column: 1 / -1;
  }

  .sidebar-account {
    margin: 0 10px 10px;
  }

  .admin-content {
    padding: 4px 0 22px;
  }
}
</style>
