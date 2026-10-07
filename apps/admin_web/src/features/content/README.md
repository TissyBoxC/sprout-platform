# Content Library Feature

Owns the content library: content packages, their versions, asset metadata
(file key, checksum, size), the review lifecycle
(draft / in review / published / withdrawn / archived), and the actions that
move a version through that lifecycle.

## Layers

- `application/contentLibraryStore.ts` owns list filters, pagination, the
  selected package detail, and the download-file lookup used to resolve a
  version's download address. The page keeps this store as the single source of
  truth so the table and the detail drawer never drift after a status change.
- `presentation/ContentLibraryPage.vue` renders the filter bar, the package
  table, the detail drawer, the create/edit dialogs, and the reject dialog.
- `presentation/ContentAssetFields.vue` is the shared asset editor used by both
  the create and edit dialogs. It supports choosing an already-uploaded file
  from the download inventory, which fills in the file key, checksum, and size.

## Contracts

The backend contract lives in `src/api/adminContent.ts`. A content package has
an immutable `package_id` shaped like `content_<category>_<3 digits>`, a title,
a category, and one or more age tiers. Each version carries the asset metadata,
the current status, and its review history.

## Boundaries

This feature never uploads files. Uploading and the release index stay in the
downloads feature; the content library only references files that already
exist there and links to their resolved download address.
