export type FamilyRole = 'owner' | 'guardian' | 'viewer'

export interface FamilyRoleBoundary {
  role: FamilyRole
  label: string
  availability: string
  description: string
}

export const familyRoleBoundaries: readonly FamilyRoleBoundary[] = [
  {
    role: 'owner',
    label: '所有者',
    availability: '1 人',
    description: '可管理家庭成员、邀请和管理账号下的设备与 AI 服务。',
  },
  {
    role: 'guardian',
    label: '守护者',
    availability: '可邀请',
    description: '可通过家长端邀请加入，加入后可照看孩子、设备和使用设置。',
  },
  {
    role: 'viewer',
    label: '查看者',
    availability: '只读',
    description: '可通过家长端邀请加入，只查看家庭信息，不能修改成员或设备。',
  },
]

// The guardian API resolves the signed-in account's own family. Until a
// parent-scoped administrator endpoint exists, the console must not reuse it
// for another parent's family.
export const adminCanProxyFamilyWrites = false
