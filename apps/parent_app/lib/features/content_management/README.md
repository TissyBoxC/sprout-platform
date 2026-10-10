# Content Management Feature

Owns the guardian content catalog, age and category filters, incremental
catalog synchronization, verified downloads, local download state, deletion,
withdrawal cleanup, and bookmarks for the 初芽 device.

The page is reached from 我的 → 内容管理 and keeps all user-visible states in
Simplified Chinese. Downloaded packages live under the application support
directory; the JSON index is written through a temporary file and atomic
rename so an interrupted session cannot publish a partial catalog.

The page has two slices: 全部内容 renders the merged catalog with a per-package
bookmark star, and 我的收藏 renders the guardian's bookmarks from
`/api/v1/content/favorites`. Bookmark toggles are optimistic so the star reacts
immediately, then reconcile with the platform; a failed toggle rolls back and
shows the reason.
