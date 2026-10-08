<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { useChildProfileStore } from '@/features/child/application/childProfileStore'
import type { AgeTier, ChildProfile, DisabledPeriod } from '@/features/child/domain/childProfile'

const store = useChildProfileStore()
const selectedParentAccountId = ref('')
const expandedChildId = ref('')

const selectedFamily = computed(
  () =>
    store.families.find((family) => family.parentAccountId === selectedParentAccountId.value) ??
    null,
)
const children = computed(() => store.childrenFor(selectedParentAccountId.value))
const childrenError = computed(() => store.childrenErrorFor(selectedParentAccountId.value))
const isChildrenLoading = computed(() => store.isChildrenLoading(selectedParentAccountId.value))

onMounted(() => {
  void reloadAll()
})

async function reloadAll(): Promise<void> {
  const loadError = await store.loadFamilies()
  if (loadError !== null || store.families.length === 0) {
    selectedParentAccountId.value = ''
    expandedChildId.value = ''
    return
  }

  const firstFamily = store.families[0]
  if (firstFamily === undefined) {
    return
  }
  await selectFamily(firstFamily.parentAccountId)
}

async function selectFamily(parentAccountId: string): Promise<void> {
  selectedParentAccountId.value = parentAccountId
  expandedChildId.value = ''
  await store.loadChildren(parentAccountId)
}

function toggleChild(child: ChildProfile): void {
  if (expandedChildId.value === child.childId) {
    expandedChildId.value = ''
    return
  }

  expandedChildId.value = child.childId
}

async function reloadChildren(): Promise<void> {
  if (!selectedParentAccountId.value) {
    return
  }
  expandedChildId.value = ''
  await store.loadChildren(selectedParentAccountId.value)
}

async function reloadChildPolicy(): Promise<void> {
  if (!selectedParentAccountId.value) {
    return
  }
  await store.loadChildren(selectedParentAccountId.value)
}

function isExpanded(childId: string): boolean {
  return expandedChildId.value === childId
}

function ageTierLabel(value: AgeTier): string {
  return (
    {
      age_3_4: '3-4 岁',
      age_5_6: '5-6 岁',
      age_7_8: '7-8 岁',
    }[value] ?? '年龄段未设置'
  )
}

function contentCategoryLabels(values: string[]): string[] {
  if (values.length === 0) {
    return ['暂未选择内容分类']
  }
  return values.map((value) => {
    return (
      {
        story: '故事',
        nursery_rhyme: '儿歌',
        poetry: '古诗',
        english: '英语启蒙',
        encyclopedia: '科普百科',
        bedtime: '睡前内容',
      }[value] ?? '其他内容'
    )
  })
}

function interestLabels(values: string[]): string[] {
  if (values.length === 0) {
    return ['暂未记录兴趣']
  }
  const labels = values
    .map((value) => {
      return (
        {
          animals: '动物',
          music: '音乐',
          nature: '自然',
          science: '科学',
          art: '美术',
          dance: '舞蹈',
          sports: '运动',
          vehicles: '交通工具',
          space: '太空',
          ocean: '海洋',
          stories: '故事',
          math: '数学',
        }[value] ?? ''
      )
    })
    .filter((label) => label.length > 0)

  return labels.length > 0 ? Array.from(new Set(labels)) : ['其他兴趣']
}

function dailyLimitLabel(minutes: number): string {
  return minutes === 0 ? '不限使用时长' : `${minutes} 分钟`
}

function disabledPeriodLabel(period: DisabledPeriod): string {
  const crossesMidnight = period.endTime <= period.startTime
  return `${period.startTime} 至${crossesMidnight ? '次日 ' : ' '}${period.endTime}`
}

function guardianConsentLabel(value: boolean): string {
  return value ? '已获得监护人同意' : '等待监护人确认'
}

function formatDate(value: string): string {
  if (!value) {
    return '尚未更新'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未更新'
  }
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
  })
}
</script>

<template>
  <section class="child-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">家长守护</p>
        <h1>儿童档案</h1>
        <p class="page-description">
          选择家长账号，查看宝贝档案与家长设置的守护策略。此页面只读，不会修改家长数据。
        </p>
      </div>
      <button
        type="button"
        class="secondary-button"
        :disabled="store.isLoadingFamilies"
        @click="reloadAll"
      >
        {{ store.isLoadingFamilies ? '正在更新…' : '重新加载' }}
      </button>
    </header>

    <div v-if="store.error" class="state-panel error-state">
      <span class="state-icon" aria-hidden="true">!</span>
      <div>
        <strong>{{ store.error.message }}</strong>
        <p v-if="store.error.kind === 'insufficient_permission'">
          请联系品牌管理员开通儿童档案查看权限。
        </p>
      </div>
      <button type="button" class="text-button" @click="reloadAll">重新加载</button>
    </div>

    <div v-else-if="store.isLoadingFamilies" class="state-panel">
      <span class="state-spinner" aria-hidden="true"></span>
      正在读取家长账号…
    </div>

    <div v-else-if="store.families.length === 0" class="state-panel empty-state">
      <span class="state-icon" aria-hidden="true">☆</span>
      <div>
        <strong>还没有家长账号</strong>
        <p>创建家长账号后，宝贝档案会显示在这里。</p>
      </div>
    </div>

    <div v-else class="profile-layout">
      <aside class="account-panel">
        <div class="section-heading">
          <div>
            <h2>家长账号</h2>
            <p>共 {{ store.families.length }} 个账号</p>
          </div>
        </div>
        <div class="account-list">
          <button
            v-for="family in store.families"
            :key="family.parentAccountId"
            type="button"
            class="account-item"
            :class="{ active: family.parentAccountId === selectedParentAccountId }"
            :aria-pressed="family.parentAccountId === selectedParentAccountId"
            @click="selectFamily(family.parentAccountId)"
          >
            <span class="account-avatar" aria-hidden="true">家</span>
            <span class="account-copy">
              <strong>{{ family.displayName }}</strong>
              <small>{{ family.phone }}</small>
            </span>
          </button>
        </div>
      </aside>

      <section class="profile-content">
        <div v-if="selectedFamily" class="content-heading">
          <div>
            <p class="eyebrow">当前家长</p>
            <h2>{{ selectedFamily.displayName }}</h2>
            <p>{{ selectedFamily.phone }}</p>
          </div>
          <button
            type="button"
            class="secondary-button"
            :disabled="isChildrenLoading"
            @click="reloadChildren"
          >
            {{ isChildrenLoading ? '正在更新…' : '更新档案' }}
          </button>
        </div>

        <div v-if="isChildrenLoading" class="state-panel compact-state">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取宝贝档案…
        </div>

        <div v-else-if="childrenError" class="state-panel compact-state error-state">
          <span class="state-icon" aria-hidden="true">!</span>
          <div>
            <strong>{{ childrenError.message }}</strong>
            <p v-if="childrenError.kind === 'insufficient_permission'">
              请联系品牌管理员开通儿童档案查看权限。
            </p>
          </div>
          <button type="button" class="text-button" @click="reloadChildren">重新加载</button>
        </div>

        <div v-else-if="children.length === 0" class="state-panel compact-state empty-state">
          <span class="state-icon" aria-hidden="true">☆</span>
          <div>
            <strong>这个家长还没有宝贝档案</strong>
            <p>家长在应用中创建宝贝档案后，资料会显示在这里。</p>
          </div>
        </div>

        <ul v-else class="child-list">
          <li
            v-for="child in children"
            :key="child.childId"
            class="child-item"
            :class="{ expanded: isExpanded(child.childId) }"
          >
            <button
              type="button"
              class="child-summary"
              :aria-expanded="isExpanded(child.childId)"
              @click="toggleChild(child)"
            >
              <span class="child-avatar" aria-hidden="true">芽</span>
              <span class="child-title">
                <strong>{{ child.nickname }}</strong>
                <small>
                  {{ ageTierLabel(child.ageTier) }} · {{ formatDate(child.updatedAt) }} 更新
                </small>
              </span>
              <span class="child-category">
                {{ contentCategoryLabels(child.contentCategories).join('、') }}
              </span>
              <span class="expand-mark" aria-hidden="true">
                {{ isExpanded(child.childId) ? '收起' : '查看' }}
              </span>
            </button>

            <div v-if="isExpanded(child.childId)" class="child-detail">
              <div class="profile-grid">
                <div>
                  <span>年龄段</span>
                  <strong>{{ ageTierLabel(child.ageTier) }}</strong>
                </div>
                <div>
                  <span>监护人同意</span>
                  <strong>{{ guardianConsentLabel(child.guardianConsent) }}</strong>
                </div>
                <div>
                  <span>兴趣</span>
                  <strong>{{ interestLabels(child.interests).join('、') }}</strong>
                </div>
                <div>
                  <span>内容分类</span>
                  <strong>{{ contentCategoryLabels(child.contentCategories).join('、') }}</strong>
                </div>
                <div>
                  <span>档案创建</span>
                  <strong>{{ formatDate(child.createdAt) }}</strong>
                </div>
                <div>
                  <span>最近更新</span>
                  <strong>{{ formatDate(child.updatedAt) }}</strong>
                </div>
              </div>

              <section class="policy-panel">
                <div class="section-heading">
                  <div>
                    <h3>家长策略</h3>
                    <p>家长在应用中设置的时长、内容和免打扰规则。</p>
                  </div>
                  <span
                    v-if="child.policyState.status === 'available'"
                    class="policy-version"
                  >
                    第 {{ child.policyState.policy.policyVersion }} 版
                  </span>
                </div>

                <div
                  v-if="child.policyState.status === 'unavailable'"
                  class="inline-state error"
                >
                  <span>
                    <strong>策略读取失败</strong>
                    <br />
                    {{ child.policyState.message }}
                  </span>
                  <button type="button" class="text-button" @click="reloadChildPolicy">
                    重新读取
                  </button>
                </div>
                <div
                  v-else-if="child.policyState.status === 'not_set'"
                  class="inline-state"
                >
                  监护人尚未设置时间与内容策略。监护人保存后，这里会显示最新规则。
                </div>
                <template v-else>
                  <div class="policy-grid">
                    <div>
                      <span>每日使用时长</span>
                      <strong>
                        {{
                          dailyLimitLabel(
                            child.policyState.policy.dailyLimitMinutes,
                          )
                        }}
                      </strong>
                    </div>
                    <div>
                      <span>最大音量</span>
                      <strong>{{ child.policyState.policy.maxVolumePercent }}%</strong>
                    </div>
                    <div class="wide-field">
                      <span>允许内容</span>
                      <strong>
                        {{
                          contentCategoryLabels(
                            child.policyState.policy.allowedCategories,
                          ).join('、')
                        }}
                      </strong>
                    </div>
                    <div class="wide-field">
                      <span>免打扰时段</span>
                      <strong v-if="child.policyState.policy.disabledPeriods.length">
                        {{
                          child.policyState.policy.disabledPeriods
                            .map(disabledPeriodLabel)
                            .join('；')
                        }}
                      </strong>
                      <strong v-else>未设置免打扰时段</strong>
                    </div>
                  </div>
                  <p class="policy-updated">
                    策略更新于
                    {{ formatDate(child.policyState.policy.updatedAt) }}
                  </p>
                </template>
              </section>
            </div>
          </li>
        </ul>
      </section>
    </div>
  </section>
</template>

<style scoped>
.child-page {
  display: grid;
  gap: 20px;
}

.page-header,
.account-panel,
.profile-content {
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 24px;
}

.eyebrow {
  margin: 0 0 6px;
  color: var(--sprout-pink-strong);
  font-size: 13px;
  font-weight: 700;
}

h1,
h2,
h3 {
  margin: 0;
  color: var(--sprout-text);
}

h1 {
  font-size: 28px;
}

h2 {
  font-size: 20px;
}

h3 {
  font-size: 17px;
}

.page-description,
.section-heading p,
.content-heading p,
.state-panel p,
.policy-panel p {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

button {
  transition:
    transform var(--sprout-duration-fast) var(--sprout-ease-out),
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-base) ease;
}

button:not(:disabled):active {
  transform: scale(0.98);
}

button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.secondary-button {
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

.secondary-button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

.text-button {
  min-height: auto;
  padding: 2px 0;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.text-button:not(:disabled):hover {
  color: #a82d5a;
}

.profile-layout {
  display: grid;
  grid-template-columns: 286px minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}

.account-panel {
  display: grid;
  gap: 14px;
  padding: 18px;
}

.account-list {
  display: grid;
  gap: 8px;
  max-height: min(620px, calc(100vh - 220px));
  overflow: auto;
}

.account-item {
  display: flex;
  align-items: center;
  gap: 11px;
  width: 100%;
  padding: 11px;
  border: 1px solid transparent;
  border-radius: 16px;
  background: #fffbfc;
  color: var(--sprout-text);
  text-align: left;
  cursor: pointer;
}

.account-item:hover {
  border-color: #f0bdcb;
  background: #fff7fa;
}

.account-item.active {
  border-color: #f7a8bf;
  background: #fff0f4;
  box-shadow: inset 0 0 0 1px rgb(240 189 203 / 55%);
}

.account-avatar,
.child-avatar {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 14px;
  background: #fff0f4;
  color: #c94175;
  font-weight: 800;
}

.account-avatar {
  width: 34px;
  height: 34px;
  font-size: 13px;
}

.account-copy,
.child-title {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.account-copy strong,
.account-copy small,
.child-title strong,
.child-title small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-copy small,
.child-title small {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.profile-content {
  min-width: 0;
  padding: 22px;
}

.content-heading,
.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.content-heading {
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid #f7d9e2;
}

.child-list {
  display: grid;
  gap: 12px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.child-item {
  overflow: hidden;
  border: 1px solid #f7d9e2;
  border-radius: 20px;
  background: #ffffff;
  transition:
    border-color var(--sprout-duration-base) ease,
    box-shadow var(--sprout-duration-base) ease;
}

.child-item.expanded {
  border-color: #e9a5b8;
  box-shadow: 0 12px 28px rgb(194 91 128 / 9%);
}

.child-summary {
  display: grid;
  width: 100%;
  grid-template-columns: auto minmax(0, 1fr) minmax(0, 1.4fr) auto;
  align-items: center;
  gap: 14px;
  padding: 15px 16px;
  border: 0;
  background: transparent;
  color: var(--sprout-text);
  text-align: left;
  cursor: pointer;
}

.child-summary:hover {
  background: #fffbfc;
}

.child-avatar {
  width: 42px;
  height: 42px;
  background: linear-gradient(145deg, #ffe8ef, #f7a8bf);
  color: #ffffff;
  box-shadow: 0 7px 16px rgb(217 79 131 / 16%);
}

.child-category {
  overflow: hidden;
  color: var(--sprout-text-muted);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.expand-mark {
  flex: 0 0 auto;
  color: #c94175;
  font-size: 13px;
  font-weight: 700;
}

.child-detail {
  display: grid;
  gap: 18px;
  padding: 0 16px 18px;
  border-top: 1px solid #f7d9e2;
  background: #fffbfc;
}

.profile-grid,
.policy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  padding-top: 16px;
}

.profile-grid > div,
.policy-grid > div {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 12px;
  border-radius: 14px;
  background: #ffffff;
}

.profile-grid span,
.policy-grid span {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.profile-grid strong,
.policy-grid strong {
  overflow-wrap: anywhere;
  color: var(--sprout-text);
  line-height: 1.5;
}

.policy-panel {
  display: grid;
  gap: 12px;
  padding-top: 4px;
}

.policy-version {
  flex: 0 0 auto;
  padding: 4px 9px;
  border-radius: 999px;
  background: #fff3cd;
  color: #755600;
  font-size: 12px;
  font-weight: 700;
}

.inline-state {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 14px 15px;
  border-radius: 15px;
  background: #fff7fa;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.inline-state.error {
  background: #fff0f2;
  color: #a12b4a;
}

.wide-field {
  grid-column: 1 / -1;
}

.policy-updated {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.state-panel {
  display: flex;
  min-height: 132px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 28px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
  text-align: center;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.compact-state {
  min-height: 220px;
}

.state-panel > div {
  text-align: left;
}

.state-panel strong {
  color: var(--sprout-text);
}

.error-state {
  color: #a12b4a;
}

.error-state strong {
  color: #a12b4a;
}

.empty-state {
  flex-wrap: wrap;
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

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 980px) {
  .profile-layout {
    grid-template-columns: 1fr;
  }

  .account-list {
    display: flex;
    max-height: none;
    overflow-x: auto;
    padding-bottom: 2px;
  }

  .account-item {
    min-width: 210px;
  }
}

@media (max-width: 720px) {
  .page-header,
  .content-heading,
  .section-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .page-header .secondary-button,
  .content-heading .secondary-button {
    width: 100%;
  }

  .child-summary {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .child-category {
    display: none;
  }

  .profile-grid,
  .policy-grid {
    grid-template-columns: 1fr;
  }

  .wide-field {
    grid-column: auto;
  }

  .state-panel {
    align-items: flex-start;
    flex-direction: column;
  }

  .state-panel > div {
    text-align: left;
  }
}
</style>
