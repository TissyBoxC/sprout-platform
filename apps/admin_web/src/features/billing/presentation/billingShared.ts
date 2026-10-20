// 计费页共用的只读提示与错误文案。权限判断留在页面里，这里只提供统一的
// 中文说明，保证四个页面说的是同一句话。

/// 只读提示。`scope` 描述当前页面能看什么，`actions` 描述被锁住的操作，
/// 避免在只有查看权限时承诺一个页面上根本不会出现的按钮。
export function readonlyNoticeFor(scope: string, actions: string): string {
  return `你当前只有查看权限。可以查看${scope}，但${actions}需要「处理退款与套餐」权限。`
}

/// 把接口错误转成家长与运营都能读懂的说明；原始错误对象永远不进界面。
export function billingErrorMessage(error: { kind: string; message: string } | null): string {
  if (error === null) {
    return ''
  }
  if (error.kind === 'insufficient_permission') {
    return '你没有权限查看这部分内容，请联系拥有计费权限的管理员。'
  }
  return error.message
}

/// 空状态文案：说明为什么是空的，以及接下来做什么。
export function emptyStateCopy(
  hasFilters: boolean,
  emptyTitle: string,
  emptyHint: string,
): { title: string; hint: string } {
  if (hasFilters) {
    return {
      title: '没有符合条件的记录',
      hint: '试试放宽筛选条件，或者清除筛选后重新查看。',
    }
  }
  return { title: emptyTitle, hint: emptyHint }
}
