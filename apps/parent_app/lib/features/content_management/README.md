# Content Management Feature

Owns the guardian content catalog, age and category filters, incremental
catalog synchronization, verified downloads, local download state, deletion,
and withdrawal cleanup for the 初芽 device.

The page is reached from 我的 → 内容管理 and keeps all user-visible states in
Simplified Chinese. Downloaded packages live under the application support
directory; the JSON index is written through a temporary file and atomic
rename so an interrupted session cannot publish a partial catalog.
