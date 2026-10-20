/**
 * Content mutations require the review capability on the server. The console
 * must apply the same boundary to every create, edit, review, publish, and
 * archive action so a read-only content operator never sees a dead control.
 */
export function canManageContent(canReview: boolean): boolean {
  return canReview
}
