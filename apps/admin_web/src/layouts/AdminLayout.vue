<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS, type AdminPermission } from '@/features/auth/domain/adminPermission'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const accountInitial = computed(() => {
  const displayName = authStore.account?.displayName.trim() ?? ''
  return displayName.slice(0, 1).toUpperCase() || '管'
})

/// One navigable destination. The permission is checked before the entry is
/// rendered, so an operator never sees a link that would answer 403.
interface AdminNavItem {
  label: string
  to: string
  permission: AdminPermission
  /// A path that also implies this entry, used when several links share a
  /// prefix such as /content.
  activePrefix?: string
}

/// One collapsible group. A group with `collapsible` false always shows its
/// entries, which suits the short, frequently used groups.
interface AdminNavGroup {
  id: string
  label: string
  collapsible: boolean
  items: AdminNavItem[]
}

/// The sidebar is grouped by the job an operator is doing rather than by data
/// model, because a flat list of 39 links forced people to read all of it to
/// find one page. Order runs from daily work outward to platform plumbing.
const navGroups: AdminNavGroup[] = [
  {
    id: 'overview',
    label: '总览',
    collapsible: false,
    items: [
      { label: '品牌总览', to: '/', permission: ADMIN_PERMISSIONS.statsRead },
    ],
  },
  {
    id: 'family',
    label: '家庭与成长',
    collapsible: false,
    items: [
      { label: '家长账号', to: '/families', permission: ADMIN_PERMISSIONS.familyRead },
      { label: '儿童档案', to: '/children', permission: ADMIN_PERMISSIONS.familyRead },
      { label: '使用报告', to: '/usage-reports', permission: ADMIN_PERMISSIONS.familyRead },
      {
        label: '消息通知',
        to: '/notifications',
        permission: ADMIN_PERMISSIONS.notificationsRead,
      },
    ],
  },
  {
    id: 'device',
    label: '设备',
    collapsible: false,
    items: [
      { label: '设备管理', to: '/devices', permission: ADMIN_PERMISSIONS.devicesRead },
      {
        label: '设备诊断',
        to: '/device-diagnostics',
        permission: ADMIN_PERMISSIONS.devicesRead,
      },
    ],
  },
  {
    id: 'capability',
    label: '设备能力',
    collapsible: true,
    items: [
      { label: '屏幕与护眼', to: '/display', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '屏幕能力', to: '/screen', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '触摸交互', to: '/touch', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '摄像头与视觉', to: '/vision', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '蓝牙音箱', to: '/bluetooth-audio', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '蜂窝网络', to: '/cellular', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '电池与省电', to: '/power', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '动作与电机', to: '/motion', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '形态能力', to: '/form-factor', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '视频陪伴', to: '/video-companion', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '视频会话', to: '/video-session', permission: ADMIN_PERMISSIONS.statsRead },
      {
        label: '学习可视化',
        to: '/learning-visualization',
        permission: ADMIN_PERMISSIONS.statsRead,
      },
      { label: '图片隐私', to: '/image-privacy', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '穿戴与定位', to: '/wearable', permission: ADMIN_PERMISSIONS.statsRead },
      { label: '多设备', to: '/multi-device', permission: ADMIN_PERMISSIONS.statsRead },
    ],
  },
  {
    id: 'billing',
    label: '计费',
    collapsible: true,
    items: [
      { label: '订单管理', to: '/billing/orders', permission: ADMIN_PERMISSIONS.billingRead },
      { label: '退款审批', to: '/billing/refunds', permission: ADMIN_PERMISSIONS.billingRead },
      { label: '套餐设置', to: '/billing/plans', permission: ADMIN_PERMISSIONS.billingRead },
      {
        label: '计费对账',
        to: '/billing/reconciliation',
        permission: ADMIN_PERMISSIONS.billingRead,
      },
    ],
  },
  {
    id: 'release',
    label: '发布与内容',
    collapsible: true,
    items: [
      { label: '内容发布', to: '/releases', permission: ADMIN_PERMISSIONS.storageRead },
      { label: '下载文件', to: '/downloads', permission: ADMIN_PERMISSIONS.storageRead },
      { label: '设备 OTA', to: '/device-ota', permission: ADMIN_PERMISSIONS.otaRead },
      { label: '内容库', to: '/content', permission: ADMIN_PERMISSIONS.contentRead },
      {
        label: '主题包',
        to: '/content/theme-packs',
        permission: ADMIN_PERMISSIONS.contentRead,
      },
      {
        label: '离线下载策略',
        to: '/content/offline-policy',
        permission: ADMIN_PERMISSIONS.contentRead,
      },
      { label: '界面文案', to: '/ui-text', permission: ADMIN_PERMISSIONS.contentRead },
    ],
  },
  {
    id: 'platform',
    label: '平台',
    collapsible: false,
    items: [
      { label: '服务版本', to: '/services', permission: ADMIN_PERMISSIONS.serviceRead },
      {
        label: '功能中心',
        to: '/feature-center',
        permission: ADMIN_PERMISSIONS.featureCenterRead,
      },
      { label: '系统设置', to: '/settings', permission: ADMIN_PERMISSIONS.settingsRead },
      {
        label: '管理员权限',
        to: '/admin-permissions',
        permission: ADMIN_PERMISSIONS.adminsRead,
      },
      { label: '操作审计', to: '/audit', permission: ADMIN_PERMISSIONS.auditRead },
    ],
  },
]

/// Only the groups a group needs are shown, so a narrower role sees a shorter
/// sidebar instead of many empty headings.
const visibleGroups = computed(() =>
  navGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => authStore.can(item.permission)),
    }))
    .filter((group) => group.items.length > 0),
)

const openGroups = ref<Record<string, boolean>>({})

function isItemActive(item: AdminNavItem): boolean {
  return route.path === item.to || route.path.startsWith(`${item.to}/`)
}

function groupHasActiveItem(group: AdminNavGroup): boolean {
  return group.items.some(isItemActive)
}

// Start with the group holding the current page open and the rest closed, so
// the sidebar opens compact but never hides where the operator already is.
watch(
  () => route.path,
  () => {
    for (const group of visibleGroups.value) {
      if (!group.collapsible) {
        continue
      }
      if (groupHasActiveItem(group)) {
        openGroups.value[group.id] = true
      } else if (openGroups.value[group.id] === undefined) {
        openGroups.value[group.id] = false
      }
    }
  },
  { immediate: true },
)

function toggleGroup(group: AdminNavGroup): void {
  openGroups.value[group.id] = !isGroupOpen(group)
}

function isGroupOpen(group: AdminNavGroup): boolean {
  if (!group.collapsible) {
    return true
  }
  return openGroups.value[group.id] === true
}

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
          <section
            v-for="group in visibleGroups"
            :key="group.id"
            class="nav-group"
          >
            <button
              v-if="group.collapsible"
              type="button"
              class="nav-group-toggle"
              :class="{ expanded: isGroupOpen(group), current: groupHasActiveItem(group) }"
              :aria-expanded="isGroupOpen(group)"
              :aria-controls="`nav-group-${group.id}`"
              @click="toggleGroup(group)"
            >
              <span class="nav-group-label">{{ group.label }}</span>
              <span class="nav-group-count" aria-hidden="true">{{ group.items.length }}</span>
              <svg
                class="nav-group-chevron"
                viewBox="0 0 16 16"
                aria-hidden="true"
                focusable="false"
              >
                <path
                  d="M4 6l4 4 4-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.8"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <p v-else class="nav-section">{{ group.label }}</p>
            <div
              v-show="isGroupOpen(group)"
              :id="`nav-group-${group.id}`"
              class="nav-group-items"
            >
              <RouterLink
                v-for="item in group.items"
                :key="item.to"
                :to="item.to"
                :class="{ 'router-link-active': isItemActive(item) }"
              >
                <span class="nav-dot" aria-hidden="true"></span>
                {{ item.label }}
              </RouterLink>
            </div>
          </section>
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

.nav-group {
  display: grid;
  gap: 3px;
}

.nav-group + .nav-group {
  margin-top: 6px;
}

.nav-group-items {
  display: grid;
  gap: 3px;
}

/* The toggle mirrors a nav link so a collapsed group still reads as one row. */
.nav-group-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 11px;
  color: #6b4f5a;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-align: left;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 14px;
  cursor: pointer;
  transition:
    background-color var(--sprout-duration-base) ease,
    color var(--sprout-duration-base) ease;
}

.nav-group-toggle:hover {
  color: #b23a68;
  background: #fffafb;
}

.nav-group-toggle:focus-visible {
  outline: 2px solid #f7a8bf;
  outline-offset: 1px;
}

.nav-group-toggle.current {
  color: #c94175;
}

.nav-group-label {
  flex: 1 1 auto;
  min-width: 0;
}

/* A count tells the operator how much a collapsed group holds. */
.nav-group-count {
  flex: 0 0 auto;
  padding: 1px 7px;
  color: #8a6b77;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0;
  background: #fff0f4;
  border-radius: 999px;
}

.nav-group-chevron {
  flex: 0 0 auto;
  width: 14px;
  height: 14px;
  color: #b4899a;
  transition: transform var(--sprout-duration-base) var(--sprout-ease-out);
}

.nav-group-toggle.expanded .nav-group-chevron {
  transform: rotate(180deg);
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
