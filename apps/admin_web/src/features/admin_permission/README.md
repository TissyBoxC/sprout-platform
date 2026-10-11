# Admin Permission Feature

Owns the administrator permission screen: reading the assignable capability
catalog, reviewing each administrator's current permissions, and granting or
revoking capabilities. It talks to `/api/v1/admin/permissions`; reading needs
`admins.read` and saving needs `admins.manage`.
