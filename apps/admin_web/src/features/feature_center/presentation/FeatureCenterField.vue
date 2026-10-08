<script setup lang="ts">
import { computed, ref } from 'vue'

import type {
  FeatureConfigField,
  FeatureConfigValue,
} from '@/api/adminFeatureCenter'

const props = defineProps<{
  field: FeatureConfigField
  value: FeatureConfigValue
  secretDraft: string
  secretConfigured: boolean
  locked?: boolean
}>()

const emit = defineEmits<{
  updateValue: [value: FeatureConfigValue]
  updateSecret: [value: string]
}>()

const numberValue = computed(() =>
  typeof props.value === 'number' ? props.value : '',
)

const textValue = computed(() =>
  typeof props.value === 'string' ? props.value : '',
)

const selectValue = computed(() => textValue.value)

const multiselectValue = computed(() =>
  Array.isArray(props.value)
    ? [...new Set(props.value.map((item) => String(item).trim()).filter(Boolean))]
    : [],
)

const customMultiselectValue = ref('')

const isEditable = computed(() => props.field.editable && props.locked !== true)

const optionLabels = computed(
  () =>
    new Map(
      props.field.options.map((option) => [option.value, option.label]),
    ),
)

const multiselectChips = computed(() =>
  multiselectValue.value.map((value) => ({
    value,
    label: optionLabels.value.get(value) ?? value,
  })),
)

const fieldControlId = computed(() => {
  if (props.field.type === 'multiselect') {
    return ''
  }
  return `feature-field-${props.field.key}`
})

const inputType = computed(() => {
  if (props.field.type === 'url') {
    return 'url'
  }
  if (props.field.type === 'duration') {
    return 'text'
  }
  return 'text'
})

function onBooleanChange(event: Event): void {
  const target = event.target
  if (target instanceof HTMLInputElement) {
    emit('updateValue', target.checked)
  }
}

function onNumberInput(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) {
    return
  }
  const value = target.value === '' ? 0 : Number(target.value)
  emit('updateValue', Number.isFinite(value) ? value : 0)
}

function onTextInput(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) {
    return
  }
  emit('updateValue', target.value)
}

function onTextAreaInput(event: Event): void {
  const target = event.target
  if (target instanceof HTMLTextAreaElement) {
    emit('updateValue', target.value)
  }
}

function onSelectChange(event: Event): void {
  const target = event.target
  if (target instanceof HTMLSelectElement) {
    emit('updateValue', target.value)
  }
}

function onMultiselectChange(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) {
    return
  }
  const selected = new Set(multiselectValue.value)
  if (target.checked) {
    selected.add(target.value)
  } else {
    selected.delete(target.value)
  }
  emit('updateValue', [...selected])
}

function addCustomMultiselectValue(): void {
  const nextValue = customMultiselectValue.value.trim()
  if (nextValue === '') {
    return
  }
  const selected = new Set(multiselectValue.value)
  selected.add(nextValue)
  emit('updateValue', [...selected])
  customMultiselectValue.value = ''
}

function removeMultiselectValue(value: string): void {
  emit(
    'updateValue',
    multiselectValue.value.filter((selected) => selected !== value),
  )
}

function onSecretInput(event: Event): void {
  const target = event.target
  if (target instanceof HTMLInputElement) {
    emit('updateSecret', target.value)
  }
}
</script>

<template>
  <div class="config-field" :class="{ 'config-field-secret': field.type === 'secret' }">
    <div class="field-heading">
      <label :for="fieldControlId || undefined">
        {{ field.label }}
        <span v-if="field.required" class="required-mark" aria-label="必填">必填</span>
      </label>
      <span v-if="field.type === 'secret'" class="field-type">密钥</span>
    </div>
    <p v-if="field.description" class="field-description">{{ field.description }}</p>

    <label v-if="field.type === 'boolean'" class="toggle-row">
      <input
        :id="`feature-field-${field.key}`"
        type="checkbox"
        :checked="value === true"
        :disabled="!isEditable"
        @change="onBooleanChange"
      />
      <span class="toggle-track" aria-hidden="true">
        <span class="toggle-thumb"></span>
      </span>
      <span class="toggle-copy">{{ value === true ? '已开启' : '已关闭' }}</span>
    </label>

    <input
      v-else-if="field.type === 'integer' || field.type === 'number'"
      :id="`feature-field-${field.key}`"
      :value="numberValue"
      type="number"
      :min="field.minimum ?? undefined"
      :max="field.maximum ?? undefined"
      :step="field.step ?? (field.type === 'integer' ? 1 : 'any')"
      :placeholder="field.placeholder || undefined"
      :disabled="!isEditable"
      @input="onNumberInput"
    />

    <select
      v-else-if="field.type === 'select'"
      :id="`feature-field-${field.key}`"
      :value="selectValue"
      :disabled="!isEditable"
      @change="onSelectChange"
    >
      <option value="" disabled>请选择</option>
      <option
        v-for="option in field.options"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>

    <div
      v-else-if="field.type === 'multiselect'"
      class="multiselect-field"
    >
      <div
        v-if="multiselectChips.length > 0"
        class="multiselect-chips"
        aria-live="polite"
      >
        <span
          v-for="chip in multiselectChips"
          :key="chip.value"
          class="multiselect-chip"
        >
          <span>{{ chip.label }}</span>
          <button
            type="button"
            :aria-label="`移除 ${chip.label}`"
            :disabled="!isEditable"
            @click="removeMultiselectValue(chip.value)"
          >
            ×
          </button>
        </span>
      </div>
      <p v-else class="multiselect-empty">
        {{
          field.allowCustom
            ? '暂未选择，可从下方选项或输入框添加。'
            : '暂未选择，可从下方选项中选择。'
        }}
      </p>

      <div v-if="field.options.length > 0" class="multiselect-options">
        <label
        v-for="option in field.options"
        :key="option.value"
          class="multiselect-option"
        >
          <input
            type="checkbox"
            :value="option.value"
            :checked="multiselectValue.includes(option.value)"
            :disabled="!isEditable"
            @change="onMultiselectChange"
          />
          <span>{{ option.label }}</span>
        </label>
      </div>
      <p v-else class="multiselect-empty">
        {{
          field.allowCustom
            ? '暂时没有预设选项，可在下方输入值后添加。'
            : '暂时没有可选项。'
        }}
      </p>

      <div v-if="field.allowCustom" class="custom-value-row">
        <input
          :id="`feature-field-${field.key}-custom`"
          v-model="customMultiselectValue"
          type="text"
          :aria-label="field.label"
          :placeholder="field.placeholder || '输入自定义值'"
          :disabled="!isEditable"
          @keydown.enter.prevent="addCustomMultiselectValue"
        />
        <button
          type="button"
          :disabled="!isEditable || customMultiselectValue.trim() === ''"
          @click="addCustomMultiselectValue"
        >
          添加
        </button>
      </div>
    </div>

    <input
      v-else-if="field.type === 'secret'"
      :id="`feature-field-${field.key}`"
      class="secret-input"
      type="password"
      autocomplete="new-password"
      spellcheck="false"
      :value="secretDraft"
      :placeholder="secretConfigured ? '已配置，输入新值可替换' : '未配置，请输入密钥'"
      :disabled="!isEditable"
      @input="onSecretInput"
    />

    <textarea
      v-else-if="field.type === 'string' && field.key === 'system_prompt'"
      :id="`feature-field-${field.key}`"
      :value="textValue"
      rows="5"
      :placeholder="field.placeholder || undefined"
      :disabled="!isEditable"
      @input="onTextAreaInput"
    ></textarea>

    <input
      v-else
      :id="`feature-field-${field.key}`"
      :value="textValue"
      :type="inputType"
      :inputmode="field.type === 'duration' ? 'text' : undefined"
      :placeholder="field.placeholder || undefined"
      spellcheck="false"
      :disabled="!isEditable"
      @input="onTextInput"
    />

    <p v-if="field.type === 'secret'" class="field-state">
      {{ secretConfigured ? '当前状态：已配置' : '当前状态：未配置' }}
      <span v-if="secretDraft.trim()">· 保存后替换现有密钥</span>
    </p>
  </div>
</template>

<style scoped>
.config-field {
  display: grid;
  gap: 8px;
  padding: 16px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 18px;
  background: #fffbfc;
}

.config-field-secret {
  border-color: #f3d18e;
  background: #fffaf0;
}

.field-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.field-heading label {
  color: var(--sprout-text);
  font-size: 14px;
  font-weight: 700;
}

.field-type {
  padding: 3px 8px;
  border-radius: 999px;
  background: #fff0d3;
  color: #7a5500;
  font-size: 11px;
  font-weight: 700;
}

.required-mark {
  margin-left: 5px;
  color: #b3261e;
  font-size: 11px;
}

.field-description {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 12px;
  line-height: 1.55;
}

input:not([type='checkbox']),
select,
textarea {
  width: 100%;
  min-height: 43px;
  padding: 0 13px;
  border: 1px solid var(--sprout-outline);
  border-radius: 14px;
  background: #ffffff;
  color: var(--sprout-text);
  font: inherit;
}

textarea {
  min-height: 112px;
  padding: 12px 13px;
  resize: vertical;
}

input:focus,
select:focus,
textarea:focus {
  border-color: var(--sprout-pink-strong);
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

.toggle-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: fit-content;
  cursor: pointer;
}

.toggle-row input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.toggle-track {
  display: flex;
  width: 48px;
  height: 28px;
  align-items: center;
  padding: 3px;
  border: 1px solid #e9a5b8;
  border-radius: 999px;
  background: #f5eef1;
  transition:
    background-color var(--sprout-duration-base) ease,
    border-color var(--sprout-duration-base) ease;
}

.toggle-thumb {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: #ffffff;
  box-shadow: 0 2px 6px rgb(74 46 59 / 18%);
  transition: transform var(--sprout-duration-base) var(--sprout-ease-out);
}

.toggle-row input:checked + .toggle-track {
  border-color: var(--sprout-pink-strong);
  background: var(--sprout-pink-strong);
}

.toggle-row input:checked + .toggle-track .toggle-thumb {
  transform: translateX(20px);
}

.toggle-row input:focus-visible + .toggle-track {
  outline: 2px solid var(--sprout-pink-strong);
  outline-offset: 3px;
}

.toggle-row input:disabled + .toggle-track,
input:disabled,
select:disabled,
textarea:disabled {
  cursor: not-allowed;
  opacity: 0.62;
}

.toggle-copy {
  color: var(--sprout-text-muted);
  font-size: 13px;
  font-weight: 700;
}

.field-state {
  margin: 0;
  color: #7a5500;
  font-size: 12px;
}

.multiselect-field {
  display: grid;
  gap: 10px;
}

.multiselect-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  min-height: 32px;
  align-items: center;
}

.multiselect-chip {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 6px;
  padding: 5px 6px 5px 10px;
  border: 1px solid #f0bdcb;
  border-radius: 999px;
  background: #fff0f4;
  color: #8d2f55;
  font-size: 12px;
  font-weight: 700;
}

.multiselect-chip > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.multiselect-chip button {
  display: grid;
  width: 20px;
  height: 20px;
  flex: 0 0 auto;
  place-items: center;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: rgb(217 79 131 / 12%);
  color: #8d2f55;
  font: inherit;
  font-size: 15px;
  line-height: 1;
  cursor: pointer;
}

.multiselect-chip button:not(:disabled):hover {
  background: #d94f83;
  color: #ffffff;
}

.multiselect-chip button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.multiselect-options {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 7px;
}

.multiselect-option {
  display: flex;
  min-height: 40px;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 13px;
  background: #ffffff;
  color: var(--sprout-text);
  cursor: pointer;
}

.multiselect-option:hover {
  border-color: #f0bdcb;
  background: #fffafb;
}

.multiselect-option input {
  width: 16px;
  height: 16px;
  flex: 0 0 auto;
  accent-color: var(--sprout-pink-strong);
}

.multiselect-option span {
  overflow-wrap: anywhere;
  font-size: 12px;
}

.multiselect-empty {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.custom-value-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

.custom-value-row button {
  min-height: 43px;
  padding: 0 14px;
  border: 1px solid #e9a5b8;
  border-radius: 13px;
  background: #fff7fa;
  color: #a83260;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.custom-value-row button:not(:disabled):hover {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
}

.custom-value-row button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
</style>
