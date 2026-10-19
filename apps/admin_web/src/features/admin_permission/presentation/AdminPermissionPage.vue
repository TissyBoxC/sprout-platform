<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import {
  useAdminPermissionStore,
  type AdminPermissionGroup,
} from '@/features/admin_permission/application/adminPermissionStore'

const store = useAdminPermissionStore()
const selectedAccountId = ref('')

const selectedAccount = computed(
  () => store.accounts.find((account) => account.accountId === selectedAccountId.value) ?? null,
)
const selectedSaveError = computed(() => store.saveErrors[selectedAccountId.value] ?? null)
const selectedSaveWarning = computed(
  () => store.saveWarnings[selectedAccountId.value] ?? '',
)
const selectedIsSaving = computed(() => store.savingAccountId === selectedAccountId.value)
const selectedIsDirty = computed(
  () => selectedAccount.value !== null && store.isDirty(selectedAccount.value.accountId),
)
const selectedUsesWildcard = computed(
  () => selectedAccount.value !== null && store.isWildcard(selectedAccount.value.accountId),
)
const selectedPermissionCount = computed(() => {
  if (selectedAccount.value === null) {
    return 0
  }
  return store.isWildcard(selectedAccount.value.accountId)
    ? store.catalog.length
    : store.draftFor(selectedAccount.value.accountId).length
})

onMounted(() => {
  void load()
})

async function load(): Promise<void> {
  await store.load()
  if (
    selectedAccountId.value === '' ||
    !store.accounts.some((account) => account.accountId === selectedAccountId.value)
  ) {
    selectedAccountId.value = store.accounts[0]?.accountId ?? ''
  }
}

function selectAccount(accountId: string): void {
  selectedAccountId.value = accountId
}

function groupHasSelection(group: AdminPermissionGroup): boolean {
  const accountId = selectedAccount.value?.accountId
  if (accountId === undefined) {
    return false
  }
  return group.options.some((option) => store.hasPermission(accountId, option.id))
}

function toggleWildcard(event: Event): void {
  const accountId = selectedAccount.value?.accountId
  if (accountId === undefined) {
    return
  }
  store.setWildcard(accountId, (event.target as HTMLInputElement).checked)
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">管理员权限</p>
        <h1>管理员权限</h1>
        <p class="page-description">
          给每位管理员分配刚好够用的权限。没有勾选的入口，对方登录后也无法使用。
        </p>
      </div>
      <button type="button" class="primary" :disabled="store.isLoading" @click="load">
        重新加载
      </button>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message">
        {{ store.lastMessage }}
      </p>
    </Transition>

    <div v-if="store.isLoading" class="state-panel">
      <span class="state-spinner" aria-hidden="true"></span>
      正在读取管理员权限…
    </div>

    <div v-else-if="store.error" class="state-panel state-error">
      <p>{{ store.error.message }}</p>
      <button type="button" class="primary" @click="load">重新加载</button>
    </div>

    <div v-else-if="store.accounts.length === 0" class="state-panel">
      <p>还没有可分配权限的管理员。创建管理员后，会显示在这里。</p>
    </div>

    <div v-else class="permission-layout">
      <aside class="account-column" aria-label="管理员列表">
        <button
          v-for="account in store.accounts"
          :key="account.accountId"
          type="button"
          class="account-card"
          :class="{ selected: account.accountId === selectedAccountId }"
          @click="selectAccount(account.accountId)"
        >
          <strong>{{ account.displayName || '未命名管理员' }}</strong>
          <small>{{ account.email }}</small>
          <span class="account-summary">
            {{
              account.permissions.includes('*')
                ? '全部管理权限'
                : `已分配 ${account.permissions.length} 项权限`
            }}
          </span>
        </button>
      </aside>

      <div v-if="selectedAccount" class="permission-panel">
        <div class="panel-heading">
          <div>
            <h2>{{ selectedAccount.displayName || '未命名管理员' }}</h2>
            <p>{{ selectedAccount.email }}</p>
          </div>
          <span class="permission-count"> 已选 {{ selectedPermissionCount }} 项 </span>
        </div>

        <label class="wildcard-row">
          <span>
            <strong>全部管理权限</strong>
            <small>选择后可以使用全部管理功能，适合日常维护账号。</small>
          </span>
          <input type="checkbox" :checked="selectedUsesWildcard" @change="toggleWildcard" />
        </label>

        <fieldset
          v-for="group in store.groups"
          :key="group.group"
          class="permission-group"
          :class="{ muted: selectedUsesWildcard }"
          :disabled="selectedUsesWildcard"
        >
          <legend>
            {{ group.group }}
            <span v-if="groupHasSelection(group)" aria-hidden="true">·</span>
          </legend>
          <div class="permission-grid">
            <label v-for="option in group.options" :key="option.id" class="permission-item">
              <input
                type="checkbox"
                :checked="store.hasPermission(selectedAccount.accountId, option.id)"
                @change="store.togglePermission(selectedAccount.accountId, option.id)"
              />
              <span>{{ option.label }}</span>
            </label>
          </div>
        </fieldset>

        <p v-if="selectedSaveError" class="error-message">
          {{ selectedSaveError.message }}
        </p>
        <p v-if="selectedSaveWarning" class="warning-message" role="alert">
          {{ selectedSaveWarning }}
        </p>

        <div class="panel-footer">
          <p>
            {{
              selectedUsesWildcard
                ? '保存后该管理员可以访问全部管理功能。'
                : '保存后立即生效，对方下次打开页面即可看到新的可用范围。'
            }}
          </p>
          <button
            type="button"
            class="primary"
            :disabled="selectedIsSaving || !selectedIsDirty"
            @click="store.save(selectedAccount.accountId)"
          >
            {{ selectedIsSaving ? '正在保存…' : '保存权限' }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 22px;
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 8px 24px rgb(194 91 128 / 5%);
}

.eyebrow {
  margin: 0 0 6px;
  color: var(--sprout-pink-strong);
  font-size: 13px;
  font-weight: 700;
}

h1 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 28px;
}

.page-description {
  max-width: 720px;
  margin: 10px 0 0;
  color: var(--sprout-text-muted);
}

button {
  min-height: 40px;
  padding: 0 18px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button.primary {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
}

button.primary:not(:disabled):hover {
  background: #c94175;
  box-shadow: 0 10px 22px rgb(217 79 131 / 18%);
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.success-message,
.error-message {
  margin: 0 0 14px;
  padding: 12px 14px;
  border-radius: 14px;
}

.success-message {
  border: 1px solid #bfe3c8;
  background: #f2fbf4;
  color: #2f6b40;
}

.warning-message {
  margin: 0 0 14px;
  padding: 12px 14px;
  border: 1px solid #e6c56d;
  border-radius: 14px;
  background: #fff9e8;
  color: #7a5411;
}

.error-message {
  border: 1px solid #f0b6c4;
  background: #fff5f8;
  color: #a5325b;
}

.state-panel {
  display: flex;
  min-height: 160px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.state-error p {
  margin: 0;
}

.state-spinner {
  width: 18px;
  height: 18px;
  border: 2px solid #f0bdcb;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 700ms linear infinite;
}

.permission-layout {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}

.account-column {
  display: grid;
  gap: 10px;
}

.account-card {
  display: grid;
  gap: 4px;
  min-height: 84px;
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #ffffff;
  color: var(--sprout-text);
  text-align: left;
}

.account-card small {
  color: var(--sprout-text-muted);
}

.account-card .account-summary {
  margin-top: 2px;
  color: #c94175;
  font-size: 12px;
  font-weight: 700;
}

.account-card.selected {
  border-color: #e9a5b8;
  background: #fff0f4;
  box-shadow: inset 0 0 0 1px rgb(240 189 203 / 60%);
}

.permission-panel {
  display: grid;
  gap: 16px;
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding-bottom: 14px;
  border-bottom: 1px solid #f7d9e2;
}

.panel-heading h2,
.panel-heading p {
  margin: 0;
}

.panel-heading h2 {
  color: var(--sprout-text);
  font-size: 20px;
}

.panel-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
}

.permission-count {
  padding: 6px 12px;
  border: 1px solid #f7d9e2;
  border-radius: 999px;
  background: #fff7fa;
  color: #c94175;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}

.wildcard-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 22px;
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
  cursor: pointer;
}

.wildcard-row span {
  display: grid;
  gap: 3px;
}

.wildcard-row strong {
  color: var(--sprout-text);
}

.wildcard-row small {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.wildcard-row input {
  width: 24px;
  min-width: 24px;
  height: 24px;
  accent-color: #d94f83;
}

.permission-group {
  margin: 0;
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.permission-group.muted {
  opacity: 0.5;
}

.permission-group legend {
  padding: 0 8px;
  color: #4a2e3b;
  font-weight: 700;
}

.permission-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.permission-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid transparent;
  border-radius: 12px;
  color: var(--sprout-text);
  cursor: pointer;
}

.permission-item:hover {
  border-color: #f0bdcb;
  background: #fff7fa;
}

.permission-item input {
  width: 20px;
  min-width: 20px;
  height: 20px;
  accent-color: #d94f83;
}

.panel-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding-top: 14px;
  border-top: 1px solid #f7d9e2;
}

.panel-footer p {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 900px) {
  .permission-layout {
    grid-template-columns: minmax(0, 1fr);
  }

  .permission-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
