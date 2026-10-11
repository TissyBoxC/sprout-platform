<script setup lang="ts">
import type { UiTextPlatform } from '@/api/adminUiText'

export interface UiTextEntryForm {
  uiTextKey: string
  value: string
  maxLength: number
  variablesText: string
}

export interface UiTextDraftForm {
  packageId: string
  language: string
  platform: UiTextPlatform
  minClientVersion: string
  entries: UiTextEntryForm[]
}

const props = defineProps<{
  form: UiTextDraftForm
  lockIdentity: boolean
}>()

const emit = defineEmits<{
  addEntry: []
  removeEntry: [index: number]
}>()

function addEntry(): void {
  emit('addEntry')
}

function removeEntry(index: number): void {
  emit('removeEntry', index)
}
</script>

<template>
  <div class="field-grid">
    <label>
      <span>文案标识</span>
      <input
        v-model.trim="props.form.packageId"
        type="text"
        required
        :disabled="props.lockIdentity"
        placeholder="例如 home_welcome"
      />
    </label>
    <label>
      <span>语言</span>
      <select v-model="props.form.language" :disabled="props.lockIdentity">
        <option value="zh-CN">简体中文</option>
        <option value="en-US">英语（美国）</option>
      </select>
    </label>
    <label>
      <span>平台</span>
      <select v-model="props.form.platform" :disabled="props.lockIdentity">
        <option value="parent_app">家长端</option>
        <option value="admin_web">管理后台</option>
        <option value="device_firmware">设备固件</option>
      </select>
    </label>
    <label>
      <span>最低客户端版本</span>
      <input v-model.trim="props.form.minClientVersion" type="text" placeholder="留空表示不限" />
    </label>
  </div>
  <div class="entries-editor">
    <div class="entries-editor-head">
      <h3>文案条目</h3>
      <button type="button" class="ghost" @click="addEntry">添加条目</button>
    </div>
    <div v-for="(entry, index) in props.form.entries" :key="index" class="entry-row">
      <label>
        <span>键名</span>
        <input v-model.trim="entry.uiTextKey" type="text" required placeholder="home.title" />
      </label>
      <label class="span-two">
        <span>文案</span>
        <textarea v-model="entry.value" rows="2" required></textarea>
      </label>
      <label>
        <span>最长字数</span>
        <input v-model.number="entry.maxLength" type="number" min="1" max="2000" required />
      </label>
      <label>
        <span>变量（逗号分隔）</span>
        <input v-model.trim="entry.variablesText" type="text" placeholder="name, count" />
      </label>
      <button
        type="button"
        class="ghost remove"
        :disabled="props.form.entries.length <= 1"
        @click="removeEntry(index)"
      >
        删除
      </button>
    </div>
  </div>
</template>

<style scoped>
.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 18px;
}

.field-grid label,
.entry-row label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

input,
select,
textarea {
  min-height: 38px;
  padding: 8px 10px;
  border: 1px solid var(--sprout-outline);
  border-radius: 12px;
  background: #ffffff;
  color: var(--sprout-text);
  font: inherit;
}

textarea {
  resize: vertical;
}

button {
  min-height: 38px;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button.ghost {
  border-color: transparent;
  background: transparent;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.entries-editor-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.entries-editor-head h3 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 15px;
}

.entry-row {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 14px;
  padding: 14px;
  border: 1px solid var(--sprout-outline);
  border-radius: 14px;
}

.entry-row .span-two {
  grid-column: span 2;
}

.entry-row .remove {
  align-self: end;
  color: #a12b4a;
}

@media (max-width: 720px) {
  .field-grid,
  .entry-row {
    grid-template-columns: 1fr;
  }

  .entry-row .span-two {
    grid-column: span 1;
  }
}
</style>
