<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import type { ContentAgeTier, ThemePack, ThemePackInput } from '@/api/adminContent'
import {
  THEME_PACK_AGE_TIERS,
  themePackStatusLabel,
  useThemePackStore,
} from '@/features/content/application/themePackStore'
import { contentAgeTierLabel } from '@/features/content/application/contentLibraryStore'

interface ThemeForm {
  themeId: string
  title: string
  summary: string
  ageTiers: ContentAgeTier[]
  packageIds: string
}

const store = useThemePackStore()

const isEditorOpen = ref(false)
const isEditing = ref(false)
const formErrors = reactive<Record<string, string>>({})
const form = reactive<ThemeForm>(emptyForm())

const editorTitle = computed(() => (isEditing.value ? '编辑主题包' : '新建主题包'))

const themeIdPattern = /^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$/

onMounted(() => {
  void store.load()
})

function emptyForm(): ThemeForm {
  return {
    themeId: '',
    title: '',
    summary: '',
    ageTiers: ['age_3_4'],
    packageIds: '',
  }
}

function openCreate(): void {
  isEditing.value = false
  Object.assign(form, emptyForm())
  clearErrors()
  isEditorOpen.value = true
}

function openEdit(pack: ThemePack): void {
  isEditing.value = true
  Object.assign(form, {
    themeId: pack.themeId,
    title: pack.title,
    summary: pack.summary,
    ageTiers: [...pack.ageTiers],
    packageIds: pack.packageIds.join('\n'),
  })
  clearErrors()
  isEditorOpen.value = true
}

function clearErrors(): void {
  for (const key of Object.keys(formErrors)) {
    delete formErrors[key]
  }
}

function toggleAgeTier(tier: ContentAgeTier): void {
  if (form.ageTiers.includes(tier)) {
    form.ageTiers = form.ageTiers.filter((value) => value !== tier)
    return
  }
  form.ageTiers = [...form.ageTiers, tier]
}

function memberIds(): string[] {
  return form.packageIds
    .split(/[\s,]+/)
    .map((value) => value.trim())
    .filter((value) => value !== '')
}

function validate(): boolean {
  clearErrors()
  if (!themeIdPattern.test(form.themeId.trim())) {
    formErrors.themeId = '主题编号需为小写字母加下划线，例如 theme_ocean'
  }
  if (form.title.trim() === '') {
    formErrors.title = '请填写主题名称'
  }
  if (form.ageTiers.length === 0) {
    formErrors.ageTiers = '至少选择一个适合年龄'
  }
  const members = memberIds()
  if (members.length === 0) {
    formErrors.packageIds = '至少加入一个内容包'
  }
  if (members.length > 50) {
    formErrors.packageIds = '一个主题包最多 50 个内容包'
  }
  return Object.keys(formErrors).length === 0
}

async function submit(): Promise<void> {
  if (!validate()) {
    return
  }
  const input: ThemePackInput = {
    themeId: form.themeId.trim(),
    title: form.title.trim(),
    summary: form.summary.trim(),
    ageTiers: [...form.ageTiers],
    packageIds: memberIds(),
  }
  const result = await store.save(input)
  if (result.succeeded) {
    isEditorOpen.value = false
    return
  }
  if (result.error) {
    formErrors.submit = result.error.message
  }
}

async function publishPack(pack: ThemePack): Promise<void> {
  await store.publish(pack.themeId)
}

async function withdrawPack(pack: ThemePack): Promise<void> {
  await store.withdraw(pack.themeId)
}

async function deletePack(pack: ThemePack): Promise<void> {
  await store.remove(pack.themeId)
}

function statusClass(status: string): string {
  return `status-${status}`
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">内容运营</p>
        <h1>主题包</h1>
        <p class="page-description">
          把内容包整理成有序的主题合集，发布后家长端和设备即可按主题浏览、批量下载。
        </p>
      </div>
      <div class="header-actions">
        <button type="button" :disabled="store.isLoading" @click="store.load">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
        </button>
        <button type="button" class="primary" @click="openCreate">新建主题包</button>
      </div>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message" role="alert">
        {{ store.error.message }}
      </p>
    </Transition>

    <section class="stat-row" aria-label="主题包统计">
      <div class="stat-card">
        <span class="stat-label">主题包总数</span>
        <strong>{{ store.packs.length }}</strong>
      </div>
      <div class="stat-card">
        <span class="stat-label">草稿</span>
        <strong>{{ store.draftCount }}</strong>
      </div>
      <div class="stat-card">
        <span class="stat-label">已发布</span>
        <strong>{{ store.publishedCount }}</strong>
      </div>
    </section>

    <Transition name="page" mode="out-in">
      <div :key="store.isLoading ? 'loading' : store.packs.length === 0 ? 'empty' : 'table'">
        <div v-if="store.isLoading" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取主题包…
        </div>
        <div v-else-if="store.packs.length === 0" class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <span>当前没有主题包。新建主题包后，它会显示在这里。</span>
        </div>
        <div v-else class="table-shell">
          <table>
            <thead>
              <tr>
                <th>主题</th>
                <th>适合年龄</th>
                <th>内容包</th>
                <th>状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="pack in store.packs" :key="pack.themeId">
                <td>
                  <div class="content-name">
                    <strong>{{ pack.title || '未命名主题' }}</strong>
                    <small>{{ pack.themeId }}</small>
                    <small v-if="pack.summary">{{ pack.summary }}</small>
                  </div>
                </td>
                <td>{{ pack.ageTiers.map(contentAgeTierLabel).join('、') }}</td>
                <td>{{ pack.packageIds.length }} 个</td>
                <td>
                  <span :class="['status', statusClass(pack.status)]">
                    {{ themePackStatusLabel(pack.status) }}
                  </span>
                </td>
                <td class="row-actions">
                  <button
                    v-if="pack.status === 'draft'"
                    type="button"
                    class="text-button"
                    :disabled="store.isSubmitting"
                    @click="openEdit(pack)"
                  >
                    编辑
                  </button>
                  <button
                    v-if="pack.status === 'draft'"
                    type="button"
                    class="text-button"
                    :disabled="store.isSubmitting"
                    @click="publishPack(pack)"
                  >
                    发布
                  </button>
                  <button
                    v-if="pack.status === 'published'"
                    type="button"
                    class="text-button"
                    :disabled="store.isSubmitting"
                    @click="withdrawPack(pack)"
                  >
                    撤回
                  </button>
                  <button
                    v-if="pack.status !== 'published'"
                    type="button"
                    class="text-button danger"
                    :disabled="store.isSubmitting"
                    @click="deletePack(pack)"
                  >
                    删除
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </Transition>

    <div v-if="isEditorOpen" class="dialog-backdrop" @click.self="isEditorOpen = false">
      <div class="dialog" role="dialog" aria-modal="true">
        <header class="dialog-header">
          <div>
            <h2>{{ editorTitle }}</h2>
            <p class="dialog-hint">主题包按顺序引用已存在的内容包，发布后才会下发给家长端。</p>
          </div>
        </header>
        <div class="dialog-body">
          <label class="field">
            <span>主题编号</span>
            <input
              v-model.trim="form.themeId"
              type="text"
              :disabled="isEditing"
              placeholder="theme_ocean"
            />
            <small v-if="formErrors.themeId" class="field-error">{{ formErrors.themeId }}</small>
          </label>
          <label class="field">
            <span>主题名称</span>
            <input v-model.trim="form.title" type="text" placeholder="海洋主题" />
            <small v-if="formErrors.title" class="field-error">{{ formErrors.title }}</small>
          </label>
          <label class="field">
            <span>主题简介</span>
            <textarea v-model.trim="form.summary" rows="2" placeholder="简要说明主题内容"></textarea>
          </label>
          <div class="field">
            <span>适合年龄</span>
            <div class="chip-row">
              <button
                v-for="tier in THEME_PACK_AGE_TIERS"
                :key="tier"
                type="button"
                :class="['chip', { selected: form.ageTiers.includes(tier) }]"
                @click="toggleAgeTier(tier)"
              >
                {{ contentAgeTierLabel(tier) }}
              </button>
            </div>
            <small v-if="formErrors.ageTiers" class="field-error">{{ formErrors.ageTiers }}</small>
          </div>
          <label class="field">
            <span>内容包编号（每行一个，按顺序）</span>
            <textarea
              v-model="form.packageIds"
              rows="5"
              placeholder="content_story_001&#10;content_story_002"
            ></textarea>
            <small v-if="formErrors.packageIds" class="field-error">
              {{ formErrors.packageIds }}
            </small>
          </label>
          <small v-if="formErrors.submit" class="field-error">{{ formErrors.submit }}</small>
        </div>
        <footer class="dialog-actions">
          <button type="button" class="secondary" @click="isEditorOpen = false">取消</button>
          <button type="button" class="primary" :disabled="store.isSubmitting" @click="submit">
            {{ store.isSubmitting ? '正在保存…' : '保存主题包' }}
          </button>
        </footer>
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
  font-size: 28px;
}

h2 {
  font-size: 22px;
}

.page-description,
.dialog-hint {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.header-actions,
.dialog-actions,
.chip-row,
.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

button {
  min-height: 40px;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #b23a68;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button.primary {
  border-color: #d94f83;
  background: linear-gradient(180deg, #d94f83, #c94175);
  color: #ffffff;
}

button.text-button {
  min-height: 32px;
  padding: 0 8px;
  border-color: transparent;
  background: transparent;
}

button.danger {
  color: #a12b4a;
}

button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

button.chip {
  border-radius: 999px;
}

button.chip.selected {
  border-color: #d94f83;
  background: #fff0f5;
}

.success-message,
.error-message {
  margin: 16px 0 0;
  padding: 12px 16px;
  border-radius: var(--sprout-radius-control);
}

.success-message {
  background: #eaf6ef;
  color: #1f6b45;
}

.error-message {
  background: #fdecef;
  color: #a12b4a;
}

.stat-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
  margin: 20px 0;
}

.stat-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 18px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
}

.stat-label {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.stat-card strong {
  color: var(--sprout-text);
  font-size: 26px;
}

.state-panel {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 18px;
  padding: 22px;
  border: 1px dashed var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
}

.state-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid #f3c6d6;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.state-icon {
  font-size: 22px;
  color: #d94f83;
}

.table-shell {
  margin-top: 18px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  overflow: hidden;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 14px 16px;
  text-align: left;
  border-bottom: 1px solid var(--sprout-outline);
  vertical-align: top;
}

th {
  color: var(--sprout-text-muted);
  font-size: 13px;
  font-weight: 700;
}

.content-name {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.content-name small {
  color: var(--sprout-text-muted);
}

.status {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 13px;
  font-weight: 700;
}

.status-draft {
  background: #f3f1ee;
  color: #6d675f;
}

.status-published {
  background: #eaf6ef;
  color: #1f6b45;
}

.status-withdrawn {
  background: #fdecef;
  color: #a12b4a;
}

.dialog-backdrop {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(52 33 42 / 42%);
  z-index: 40;
}

.dialog {
  width: min(560px, 100%);
  max-height: 88vh;
  overflow: auto;
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 24px 60px rgb(52 33 42 / 24%);
}

.dialog-header,
.dialog-body,
.dialog-actions {
  padding: 22px 24px;
}

.dialog-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
  border-top: 1px solid var(--sprout-outline);
  border-bottom: 1px solid var(--sprout-outline);
}

.dialog-actions {
  justify-content: flex-end;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  color: var(--sprout-text);
  font-weight: 600;
}

.field input,
.field textarea {
  padding: 10px 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  font: inherit;
  color: var(--sprout-text);
}

.field-error {
  color: #a12b4a;
  font-weight: 600;
}

.toast-enter-active,
.toast-leave-active,
.page-enter-active,
.page-leave-active {
  transition: opacity 0.18s ease;
}

.toast-enter-from,
.toast-leave-to,
.page-enter-from,
.page-leave-to {
  opacity: 0;
}

@media (max-width: 720px) {
  .page-header {
    flex-direction: column;
  }

  .stat-row {
    grid-template-columns: 1fr;
  }
}
</style>
