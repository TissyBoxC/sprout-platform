<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'

import type { AdminAuditEntry } from '@/features/audit/api/adminAudit'
import { useAuditStore } from '@/features/audit/application/auditStore'
import {
  auditActionDescription,
  auditActionLabel,
  auditActorLabel,
  auditDetailEntries,
  auditTargetLabel,
} from '@/features/audit/domain/auditPresentation'

const store = useAuditStore()
const filters = reactive({
  action: '',
  actorAccountId: '',
  targetAccountId: '',
  from: '',
  to: '',
})

const pageStart = computed(() =>
  store.total === 0 ? 0 : (store.page - 1) * store.filters.pageSize + 1,
)
const pageEnd = computed(() =>
  store.total === 0
    ? 0
    : Math.min(store.page * store.filters.pageSize, store.total),
)
const canGoPrevious = computed(() => store.page > 1 && !store.isLoading)
const canGoNext = computed(
  () => store.totalPages > 0 && store.page < store.totalPages && !store.isLoading,
)

onMounted(() => {
  syncFilterForm()
  void store.load(1)
})

function syncFilterForm(): void {
  filters.action = store.filters.action
  filters.actorAccountId = store.filters.actorAccountId
  filters.targetAccountId = store.filters.targetAccountId
  filters.from = dateInputValue(store.filters.from)
  filters.to = dateInputValue(store.filters.to)
}

async function applyFilters(): Promise<void> {
  store.filters.action = filters.action
  store.filters.actorAccountId = filters.actorAccountId.trim()
  store.filters.targetAccountId = filters.targetAccountId.trim()
  store.filters.from = filters.from
  store.filters.to = filters.to
  await store.applyFilters()
}

async function resetFilters(): Promise<void> {
  await store.resetFilters()
  syncFilterForm()
}

function selectAction(value: string): void {
  filters.action = value
}

function formatTime(value: string): string {
  if (!value) {
    return '时间待补充'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '时间待补充'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function dateInputValue(value: string): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value.slice(0, 10)
  }
  return date.toISOString().slice(0, 10)
}

function entryDetails(entry: AdminAuditEntry) {
  return auditDetailEntries(entry.detail)
}
</script>

<template>
  <section class="audit-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">安全追踪</p>
        <h1>操作审计</h1>
        <p class="page-description">
          查看管理员、系统任务和隐私处理的操作记录。记录只保留操作结果和必要的说明，不包含密码、密钥或儿童内容。
        </p>
      </div>
      <button
        type="button"
        class="primary"
        :disabled="store.isLoading"
        @click="store.retry"
      >
        {{ store.isLoading ? '正在刷新…' : '刷新记录' }}
      </button>
    </header>

    <form class="filter-panel" aria-label="审计筛选" @submit.prevent="applyFilters">
      <div class="filter-heading">
        <div>
          <h2>筛选记录</h2>
          <p>按操作类型、操作者、目标账号或时间范围缩小结果。</p>
        </div>
        <span class="record-count">共 {{ store.total }} 条</span>
      </div>

      <div class="filter-grid">
        <label>
          <span>操作类型</span>
          <select v-model="filters.action">
            <option value="">全部操作</option>
            <option v-for="action in store.actions" :key="action" :value="action">
              {{ auditActionLabel(action) }}
            </option>
          </select>
        </label>
        <label>
          <span>操作者账号</span>
          <input
            v-model.trim="filters.actorAccountId"
            type="search"
            placeholder="输入管理员账号编号"
          />
        </label>
        <label>
          <span>目标账号</span>
          <input
            v-model.trim="filters.targetAccountId"
            type="search"
            placeholder="输入家长账号编号"
          />
        </label>
        <label>
          <span>开始日期</span>
          <input v-model="filters.from" type="date" />
        </label>
        <label>
          <span>结束日期</span>
          <input v-model="filters.to" type="date" />
        </label>
        <label>
          <span>每页数量</span>
          <select v-model.number="store.filters.pageSize" @change="applyFilters">
            <option :value="10">10 条</option>
            <option :value="20">20 条</option>
            <option :value="50">50 条</option>
          </select>
        </label>
      </div>

      <div class="filter-actions">
        <button
          type="button"
          class="secondary"
          :disabled="store.isLoading"
          @click="resetFilters"
        >
          清除筛选
        </button>
        <button type="submit" class="primary" :disabled="store.isLoading">
          {{ store.isLoading ? '正在查询…' : '查询记录' }}
        </button>
      </div>
    </form>

    <Transition name="toast">
      <p v-if="store.error && store.items.length > 0" class="inline-warning" role="alert">
        {{ store.error.message }}
        <button type="button" @click="store.retry">重新加载</button>
      </p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div
        v-if="store.isLoading && store.items.length === 0"
        key="loading"
        class="state-panel"
      >
        <span class="state-spinner" aria-hidden="true"></span>
        正在读取操作记录…
      </div>
      <div
        v-else-if="store.error && store.items.length === 0"
        key="error"
        class="state-panel error-state"
      >
        <span class="state-icon" aria-hidden="true">!</span>
        <div>
          <strong>暂时无法读取操作记录</strong>
          <p>{{ store.error.message }}</p>
        </div>
        <button type="button" @click="store.retry">重新加载</button>
      </div>
      <div v-else-if="store.items.length === 0" key="empty" class="state-panel">
        <span class="state-icon" aria-hidden="true">☆</span>
        <div>
          <strong>{{ store.hasFilters ? '没有符合条件的记录' : '还没有操作记录' }}</strong>
          <p>
            {{
              store.hasFilters
                ? '可以调整筛选条件后重新查询。'
                : '管理员操作、隐私处理和系统任务发生后会显示在这里。'
            }}
          </p>
        </div>
      </div>
      <div v-else key="table" class="table-shell">
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>时间</th>
                <th>操作</th>
                <th>操作者</th>
                <th>目标账号</th>
                <th>说明</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="entry in store.items" :key="entry.id">
                <td class="time-cell">{{ formatTime(entry.createdAt) }}</td>
                <td>
                  <span class="action-cell">
                    <strong>{{ auditActionLabel(entry.action) }}</strong>
                    <small>{{ auditActionDescription(entry.action) }}</small>
                  </span>
                </td>
                <td>
                  <span class="identity-cell">{{ auditActorLabel(entry) }}</span>
                </td>
                <td>
                  <span class="identity-cell">{{ auditTargetLabel(entry) }}</span>
                </td>
                <td>
                  <span v-if="entryDetails(entry).length === 0" class="detail-empty">
                    没有补充说明
                  </span>
                  <dl v-else class="detail-list">
                    <div v-for="detail in entryDetails(entry)" :key="detail.key">
                      <dt>{{ detail.label }}</dt>
                      <dd>{{ detail.value }}</dd>
                    </div>
                  </dl>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer class="pagination">
          <p>
            第 {{ pageStart }}–{{ pageEnd }} 条，共 {{ store.total }} 条
          </p>
          <div class="pagination-actions">
            <button
              type="button"
              class="secondary"
              :disabled="!canGoPrevious"
              @click="store.goToPage(store.page - 1)"
            >
              上一页
            </button>
            <span>第 {{ store.page }} / {{ Math.max(store.totalPages, 1) }} 页</span>
            <button
              type="button"
              class="secondary"
              :disabled="!canGoNext"
              @click="store.goToPage(store.page + 1)"
            >
              下一页
            </button>
          </div>
        </footer>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.audit-page {
  min-width: 0;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 20px;
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

h1,
h2 {
  margin: 0;
  color: var(--sprout-text);
}

h1 {
  font-size: 28px;
}

h2 {
  font-size: 17px;
}

.page-description,
.filter-heading p {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

button {
  min-height: 40px;
  padding: 0 17px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #b23a68;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
  transition:
    transform var(--sprout-duration-fast) var(--sprout-ease-out),
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-fast) ease;
}

button.primary {
  border-color: #d94f83;
  background: linear-gradient(180deg, #d94f83, #c94175);
  color: #ffffff;
  box-shadow: 0 8px 18px rgb(217 79 131 / 15%);
}

button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  color: #b23a68;
  box-shadow: 0 9px 20px rgb(217 79 131 / 12%);
}

button.primary:not(:disabled):hover {
  background: linear-gradient(180deg, #c94175, #b23a68);
  color: #ffffff;
}

button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.filter-panel,
.table-shell,
.state-panel {
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.filter-panel {
  overflow: hidden;
}

.filter-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.record-count {
  flex: 0 0 auto;
  padding: 5px 10px;
  border-radius: 999px;
  background: #fff2f5;
  color: #b23a68;
  font-size: 12px;
  font-weight: 700;
}

.filter-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  padding: 18px 20px 0;
}

label {
  display: grid;
  gap: 7px;
  color: var(--sprout-text);
  font-size: 13px;
  font-weight: 700;
}

input,
select {
  width: 100%;
  min-height: 42px;
  padding: 0 13px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: var(--sprout-text);
  font: inherit;
  transition:
    border-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease;
}

input:focus,
select:focus {
  border-color: #d94f83;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

.filter-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 16px 20px 20px;
}

.inline-warning {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 14px 0 0;
  padding: 12px 15px;
  border: 1px solid #efd39a;
  border-radius: 15px;
  background: #fffaf0;
  color: #755600;
}

.inline-warning button {
  min-height: 32px;
  padding: 0 12px;
  border-radius: 12px;
}

.table-shell {
  margin-top: 18px;
  overflow: hidden;
}

.table-scroll {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 14px 16px;
  border-bottom: 1px solid var(--sprout-outline-soft);
  text-align: left;
  vertical-align: top;
}

th {
  color: var(--sprout-text-muted);
  font-size: 13px;
  white-space: nowrap;
}

td {
  color: var(--sprout-text);
}

tbody tr {
  transition: background-color var(--sprout-duration-fast) ease;
}

tbody tr:hover {
  background: #fff9fb;
}

tbody tr:last-child td {
  border-bottom: 0;
}

.time-cell {
  color: #6b4f5a;
  font-size: 13px;
  white-space: nowrap;
}

.action-cell,
.identity-cell {
  display: block;
}

.action-cell strong {
  display: block;
  color: var(--sprout-text);
}

.action-cell small {
  display: block;
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 12px;
  line-height: 1.4;
}

.identity-cell {
  overflow-wrap: anywhere;
}

.detail-list {
  display: grid;
  min-width: 220px;
  gap: 5px;
  margin: 0;
}

.detail-list div {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 8px;
}

.detail-list dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.detail-list dd {
  min-width: 0;
  margin: 0;
  color: var(--sprout-text);
  font-size: 13px;
  overflow-wrap: anywhere;
}

.detail-empty {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  background: #fffbfc;
}

.pagination p,
.pagination span {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.pagination-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.state-panel {
  display: flex;
  min-height: 180px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 18px;
  padding: 28px;
  color: var(--sprout-text-muted);
  text-align: center;
}

.state-panel strong,
.state-panel p {
  display: block;
  margin: 0;
}

.state-panel strong {
  color: var(--sprout-text);
}

.state-panel p {
  margin-top: 5px;
  line-height: 1.55;
}

.state-panel.error-state {
  display: grid;
  min-height: 220px;
  justify-items: center;
  align-content: center;
}

.state-panel.error-state button {
  min-height: 38px;
}

.state-spinner {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  border: 2px solid #f0bdcb;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 700ms linear infinite;
}

.state-icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 12px;
  background: #fff2f5;
  color: #d94f83;
  font-size: 20px;
}

.error-state .state-icon {
  background: #fff0f2;
  color: #a12b4a;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1080px) {
  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .filter-grid {
    grid-template-columns: 1fr;
  }

  .filter-actions,
  .pagination {
    align-items: stretch;
    flex-direction: column;
  }

  .pagination-actions {
    flex-wrap: wrap;
  }
}

@media (max-width: 480px) {
  .filter-actions button,
  .pagination-actions button {
    width: 100%;
  }

  .pagination-actions span {
    order: -1;
  }
}
</style>
