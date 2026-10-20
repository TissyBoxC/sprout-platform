import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createBillingClient,
  type BillingOrder,
  type BillingOrderDetail,
  type BillingPagination,
} from '@/features/billing/api/billingClient'

export interface BillingOrderFilters {
  status: string
  planCode: string
  from: string
  to: string
}

export interface BillingOrderQuery extends BillingOrderFilters {
  page: number
  pageSize: number
}

export const DEFAULT_ORDER_FILTERS: BillingOrderFilters = {
  status: '',
  planCode: '',
  from: '',
  to: '',
}

const DEFAULT_PAGE_SIZE = 20
const MAX_PAGE_SIZE = 100

/// 订单列表与订单详情。每次加载都带递增序号，晚到的旧结果直接丢弃，
/// 因此快速切换筛选条件不会把旧页写回界面。
export const useBillingOrderStore = defineStore('admin-billing-orders', () => {
  const client = createBillingClient()
  const orders = ref<BillingOrder[]>([])
  const filters = ref<BillingOrderFilters>({ ...DEFAULT_ORDER_FILTERS })
  const pagination = ref<BillingPagination>({
    page: 1,
    pageSize: DEFAULT_PAGE_SIZE,
    total: 0,
    totalPages: 0,
  })
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)
  const detailOrderNo = ref('')
  const detail = ref<BillingOrderDetail | null>(null)
  const isDetailLoading = ref(false)
  const detailError = ref<ApiError | null>(null)

  let listRequestSequence = 0
  let listLoadingSequence = 0
  let activeListLoadingRequests = 0
  let detailRequestSequence = 0

  const hasFilters = computed(
    () =>
      filters.value.status !== '' ||
      filters.value.planCode.trim() !== '' ||
      filters.value.from !== '' ||
      filters.value.to !== '',
  )
  const pageStart = computed(() =>
    pagination.value.total === 0 ? 0 : (pagination.value.page - 1) * pagination.value.pageSize + 1,
  )
  const pageEnd = computed(() =>
    Math.min(pagination.value.page * pagination.value.pageSize, pagination.value.total),
  )

  async function load(targetPage = pagination.value.page): Promise<void> {
    await loadPage(targetPage, pagination.value.pageSize)
  }

  async function loadPage(targetPage: number, pageSize: number): Promise<void> {
    const requestedPage = Math.max(1, Math.trunc(targetPage))
    const requestedPageSize = Math.min(MAX_PAGE_SIZE, Math.max(1, Math.trunc(pageSize)))
    const sequence = ++listRequestSequence
    const loadingSequence = beginLoading()
    error.value = null
    try {
      const result = await client.listOrders({
        ...toQuery(filters.value),
        page: requestedPage,
        pageSize: requestedPageSize,
      })
      if (sequence !== listRequestSequence) {
        return
      }
      orders.value = result.orders
      pagination.value = result.pagination
      // 删除或退款会让总页数变小；此时回到最后一页，避免停在空白页。
      if (
        orders.value.length === 0 &&
        pagination.value.total > 0 &&
        requestedPage > Math.max(1, pagination.value.totalPages)
      ) {
        await loadPage(Math.max(1, pagination.value.totalPages), requestedPageSize)
      }
    } catch (caught: unknown) {
      if (sequence === listRequestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      finishLoading(loadingSequence)
    }
  }

  async function applyFilters(): Promise<void> {
    await loadPage(1, pagination.value.pageSize)
  }

  async function resetFilters(): Promise<void> {
    filters.value = { ...DEFAULT_ORDER_FILTERS }
    await loadPage(1, pagination.value.pageSize)
  }

  async function setFilterStatus(status: string): Promise<void> {
    filters.value = { ...filters.value, status }
    await applyFilters()
  }

  async function goToPage(targetPage: number): Promise<void> {
    if (isLoading.value || targetPage < 1) {
      return
    }
    if (pagination.value.totalPages > 0 && targetPage > pagination.value.totalPages) {
      return
    }
    await loadPage(targetPage, pagination.value.pageSize)
  }

  async function setPageSize(pageSize: number): Promise<void> {
    if (isLoading.value) {
      return
    }
    await loadPage(1, pageSize)
  }

  async function loadDetail(orderNo: string): Promise<void> {
    const normalized = orderNo.trim()
    if (normalized === '') {
      return
    }
    const sequence = ++detailRequestSequence
    detailOrderNo.value = normalized
    detail.value = null
    detailError.value = null
    isDetailLoading.value = true
    try {
      const result = await client.loadOrder(normalized)
      if (sequence !== detailRequestSequence) {
        return
      }
      detail.value = result
    } catch (caught: unknown) {
      if (sequence === detailRequestSequence) {
        detailError.value = mapApiError(caught)
      }
    } finally {
      if (sequence === detailRequestSequence) {
        isDetailLoading.value = false
      }
    }
  }

  function closeDetail(): void {
    detailRequestSequence += 1
    detailOrderNo.value = ''
    detail.value = null
    detailError.value = null
    isDetailLoading.value = false
  }

  function beginLoading(): number {
    const loadingSequence = ++listLoadingSequence
    activeListLoadingRequests += 1
    isLoading.value = true
    return loadingSequence
  }

  function finishLoading(loadingSequence: number): void {
    if (loadingSequence !== listLoadingSequence) {
      return
    }
    activeListLoadingRequests = Math.max(0, activeListLoadingRequests - 1)
    if (activeListLoadingRequests === 0) {
      isLoading.value = false
    }
  }

  return {
    applyFilters,
    closeDetail,
    detail,
    detailError,
    detailOrderNo,
    error,
    filters,
    goToPage,
    hasFilters,
    isDetailLoading,
    isLoading,
    load,
    loadDetail,
    orders,
    pageEnd,
    pageStart,
    pagination,
    resetFilters,
    setFilterStatus,
    setPageSize,
  }
})

function toQuery(filters: BillingOrderFilters): BillingOrderFilters {
  return {
    status: filters.status,
    planCode: filters.planCode.trim(),
    // 订单接口按日期筛选；起止日由页面上的日期控件给出。
    from: filters.from,
    to: filters.to,
  }
}
