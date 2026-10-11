import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/content_favorites_controller.dart';
import '../application/content_library_controller.dart';
import '../application/content_play_history_controller.dart';
import '../application/content_theme_pack_controller.dart';
import '../domain/content_package.dart';

/// Which slice of the library the page is showing.
enum _LibraryTab { catalog, themes, favorites, history }

/// Guardian-facing content library with filters and download controls.
class ContentLibraryPage extends ConsumerStatefulWidget {
  const ContentLibraryPage({super.key});

  @override
  ConsumerState<ContentLibraryPage> createState() => _ContentLibraryPageState();
}

class _ContentLibraryPageState extends ConsumerState<ContentLibraryPage> {
  _LibraryTab _tab = _LibraryTab.catalog;

  @override
  Widget build(BuildContext context) {
    final library = ref.watch(contentLibraryControllerProvider);
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          _leave(context);
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text('内容管理'),
          leading: IconButton(
            tooltip: '返回',
            onPressed: () => _leave(context),
            icon: const Icon(Icons.arrow_back_rounded),
          ),
        ),
        body: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
              child: SegmentedButton<_LibraryTab>(
                segments: const [
                  ButtonSegment<_LibraryTab>(
                    value: _LibraryTab.catalog,
                    label: Text('全部内容'),
                    icon: Icon(Icons.grid_view_rounded),
                  ),
                  ButtonSegment<_LibraryTab>(
                    value: _LibraryTab.themes,
                    label: Text('主题'),
                    icon: Icon(Icons.collections_bookmark_rounded),
                  ),
                  ButtonSegment<_LibraryTab>(
                    value: _LibraryTab.favorites,
                    label: Text('我的收藏'),
                    icon: Icon(Icons.star_rounded),
                  ),
                  ButtonSegment<_LibraryTab>(
                    value: _LibraryTab.history,
                    label: Text('播放记录'),
                    icon: Icon(Icons.history_rounded),
                  ),
                ],
                selected: <_LibraryTab>{_tab},
                onSelectionChanged: (selection) =>
                    setState(() => _tab = selection.first),
              ),
            ),
            Expanded(
              child: switch (_tab) {
                _LibraryTab.catalog => AppStateSwitcher(
                  stateKey: library.when(
                    data: (value) => value.isEmpty
                        ? 'content-empty'
                        : 'content-list-${value.catalogRevision}',
                    error: (_, _) => 'content-error',
                    loading: () => 'content-loading',
                  ),
                  child: library.when(
                    loading: () => const Center(
                      key: ValueKey<String>('content-loading'),
                      child: CircularProgressIndicator(),
                    ),
                    error: (error, _) => _ErrorState(
                      key: const ValueKey<String>('content-error'),
                      message: _messageFor(error),
                      onRetry: () => ref
                          .read(contentLibraryControllerProvider.notifier)
                          .refresh(),
                    ),
                    data: (state) => _ContentList(
                      key: const ValueKey<String>('content-content'),
                      state: state,
                    ),
                  ),
                ),
                _LibraryTab.favorites => const _FavoritesList(),
                _LibraryTab.themes => const _ThemePackList(),
                _LibraryTab.history => const _HistoryList(),
              },
            ),
          ],
        ),
      ),
    );
  }
}

class _ContentList extends ConsumerWidget {
  const _ContentList({required this.state, super.key});

  final ContentLibraryState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return RefreshIndicator(
      onRefresh: () =>
          ref.read(contentLibraryControllerProvider.notifier).refresh(),
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(
            child: _FilterCard(
              filter: state.filter,
              onChanged: (filter) => ref
                  .read(contentLibraryControllerProvider.notifier)
                  .applyFilter(filter),
            ),
          ),
          const SizedBox(height: 16),
          if (state.isEmpty)
            const AppReveal(
              delay: Duration(milliseconds: 60),
              child: _EmptyContentCard(),
            )
          else
            for (var index = 0; index < state.entries.length; index++)
              AppReveal(
                delay: Duration(milliseconds: 45 * index.clamp(0, 6)),
                child: Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: _PackageCard(entry: state.entries[index]),
                ),
              ),
        ],
      ),
    );
  }
}

class _FavoritesList extends ConsumerWidget {
  const _FavoritesList();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final favorites = ref.watch(contentFavoritesControllerProvider);
    return AppStateSwitcher(
      stateKey: favorites.when(
        data: (value) => value.isEmpty ? 'favorites-empty' : 'favorites-list',
        error: (_, _) => 'favorites-error',
        loading: () => 'favorites-loading',
      ),
      child: favorites.when(
        loading: () => const Center(
          key: ValueKey<String>('favorites-loading'),
          child: CircularProgressIndicator(),
        ),
        error: (error, _) => _ErrorState(
          key: const ValueKey<String>('favorites-error'),
          message: _messageFor(error),
          onRetry: () =>
              ref.read(contentFavoritesControllerProvider.notifier).refresh(),
        ),
        data: (state) => RefreshIndicator(
          key: const ValueKey<String>('favorites-content'),
          onRefresh: () =>
              ref.read(contentFavoritesControllerProvider.notifier).refresh(),
          child: state.isEmpty
              ? const _FavoritesEmptyList()
              : ListView(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
                  children: [
                    if (state.errorMessage != null)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: Text(
                          state.errorMessage!,
                          style: TextStyle(
                            color: Theme.of(context).colorScheme.error,
                          ),
                        ),
                      ),
                    for (var index = 0; index < state.favorites.length; index++)
                      AppReveal(
                        delay: Duration(milliseconds: 45 * index.clamp(0, 6)),
                        child: Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: _FavoriteCard(
                            favorite: state.favorites[index],
                            pending: state.isPending(
                              state.favorites[index].packageId,
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
        ),
      ),
    );
  }
}

class _FavoriteCard extends ConsumerWidget {
  const _FavoriteCard({required this.favorite, required this.pending});

  final ContentFavorite favorite;
  final bool pending;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: ListTile(
        contentPadding: const EdgeInsets.fromLTRB(18, 8, 10, 8),
        leading: CircleAvatar(
          backgroundColor: Theme.of(context).colorScheme.primaryContainer,
          child: Icon(_categoryIcon(favorite.category)),
        ),
        title: Text(favorite.title),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(_favoriteSubtitle(favorite)),
        ),
        trailing: IconButton(
          tooltip: '取消收藏',
          onPressed: pending
              ? null
              : () => ref
                    .read(contentFavoritesControllerProvider.notifier)
                    .toggle(favorite.packageId),
          icon: pending
              ? const SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.star_rounded, color: Color(0xFFE0A800)),
        ),
      ),
    );
  }
}

class _FavoritesEmptyList extends StatelessWidget {
  const _FavoritesEmptyList();

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: const [
        AppReveal(child: _FavoritesEmptyCard()),
      ],
    );
  }
}

class _FavoritesEmptyCard extends StatelessWidget {
  const _FavoritesEmptyCard();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Icon(
              Icons.star_border_rounded,
              size: 48,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text('还没有收藏的内容', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('在全部内容里点一下星星，收藏就会出现在这里。', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _HistoryList extends ConsumerWidget {
  const _HistoryList();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final history = ref.watch(contentPlayHistoryControllerProvider);
    return AppStateSwitcher(
      stateKey: history.when(
        data: (value) => value.isEmpty ? 'history-empty' : 'history-list',
        error: (_, _) => 'history-error',
        loading: () => 'history-loading',
      ),
      child: history.when(
        loading: () => const Center(
          key: ValueKey<String>('history-loading'),
          child: CircularProgressIndicator(),
        ),
        error: (error, _) => _ErrorState(
          key: const ValueKey<String>('history-error'),
          message: ContentPlayHistoryController.messageFor(error),
          onRetry: () =>
              ref.read(contentPlayHistoryControllerProvider.notifier).refresh(),
        ),
        data: (state) => RefreshIndicator(
          key: const ValueKey<String>('history-content'),
          onRefresh: () =>
              ref.read(contentPlayHistoryControllerProvider.notifier).refresh(),
          child: state.isEmpty
              ? const _HistoryEmptyList()
              : ListView(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
                  children: [
                    AppReveal(
                      child: _DeviceFilterBar(
                        deviceIds: state.deviceIds,
                        selected: state.deviceFilter,
                        onSelected: (deviceId) => ref
                            .read(
                              contentPlayHistoryControllerProvider.notifier,
                            )
                            .filterByDevice(deviceId),
                      ),
                    ),
                    const SizedBox(height: 16),
                    for (var index = 0; index < state.records.length; index++)
                      AppReveal(
                        delay: Duration(milliseconds: 45 * index.clamp(0, 6)),
                        child: Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: _HistoryCard(record: state.records[index]),
                        ),
                      ),
                  ],
                ),
        ),
      ),
    );
  }
}

class _ThemePackList extends ConsumerWidget {
  const _ThemePackList();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final themes = ref.watch(contentThemePackControllerProvider);
    return AppStateSwitcher(
      stateKey: themes.when(
        data: (value) => value.isEmpty ? 'themes-empty' : 'themes-list',
        error: (_, _) => 'themes-error',
        loading: () => 'themes-loading',
      ),
      child: themes.when(
        loading: () => const Center(
          key: ValueKey<String>('themes-loading'),
          child: CircularProgressIndicator(),
        ),
        error: (error, _) => _ErrorState(
          key: const ValueKey<String>('themes-error'),
          message: ContentThemePackController.messageFor(error),
          onRetry: () =>
              ref.read(contentThemePackControllerProvider.notifier).refresh(),
        ),
        data: (packs) => RefreshIndicator(
          key: const ValueKey<String>('themes-content'),
          onRefresh: () =>
              ref.read(contentThemePackControllerProvider.notifier).refresh(),
          child: packs.isEmpty
              ? const _ThemePackEmptyList()
              : ListView(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
                  children: [
                    for (var index = 0; index < packs.length; index++)
                      AppReveal(
                        delay: Duration(milliseconds: 45 * index.clamp(0, 6)),
                        child: Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: _ThemePackCard(pack: packs[index]),
                        ),
                      ),
                  ],
                ),
        ),
      ),
    );
  }
}

class _ThemePackCard extends StatelessWidget {
  const _ThemePackCard({required this.pack});

  final ContentThemePack pack;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 16, 18, 18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                CircleAvatar(
                  backgroundColor: Theme.of(context).colorScheme.primaryContainer,
                  child: const Icon(Icons.collections_bookmark_rounded),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    pack.title,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            if (pack.summary.isNotEmpty) ...[
              const SizedBox(height: 10),
              Text(pack.summary),
            ],
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final tier in pack.ageTiers)
                  Chip(
                    label: Text(tier.label),
                    visualDensity: VisualDensity.compact,
                  ),
                Chip(
                  label: Text('${pack.packageIds.length} 个内容'),
                  visualDensity: VisualDensity.compact,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _ThemePackEmptyList extends StatelessWidget {
  const _ThemePackEmptyList();

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: const [
        AppReveal(child: _ThemePackEmptyCard()),
      ],
    );
  }
}

class _ThemePackEmptyCard extends StatelessWidget {
  const _ThemePackEmptyCard();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Icon(
              Icons.collections_bookmark_outlined,
              size: 48,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text('还没有主题包', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('平台发布主题包后，会显示在这里方便挑选。', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _DeviceFilterBar extends StatelessWidget {
  const _DeviceFilterBar({
    required this.deviceIds,
    required this.selected,
    required this.onSelected,
  });

  final List<String> deviceIds;
  final String? selected;
  final ValueChanged<String?> onSelected;

  @override
  Widget build(BuildContext context) {
    // A single device needs no filter row: switching would be a no-op.
    if (deviceIds.length < 2) {
      return const SizedBox.shrink();
    }
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          _FilterChip(
            label: '全部设备',
            selected: selected == null,
            onSelected: () => onSelected(null),
          ),
          for (final deviceId in deviceIds)
            _FilterChip(
              label: _deviceLabel(deviceId),
              selected: selected == deviceId,
              onSelected: () => onSelected(deviceId),
            ),
        ],
      ),
    );
  }
}

class _HistoryCard extends StatelessWidget {
  const _HistoryCard({required this.record});

  final ContentPlayRecord record;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: ListTile(
        contentPadding: const EdgeInsets.fromLTRB(18, 8, 16, 8),
        leading: CircleAvatar(
          backgroundColor: Theme.of(context).colorScheme.primaryContainer,
          child: Icon(_outcomeIcon(record.outcome)),
        ),
        title: Text(record.packageId),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(_historySubtitle(record)),
        ),
        trailing: Text(
          record.outcome.label,
          style: TextStyle(
            color: _outcomeColor(context, record.outcome),
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
    );
  }
}

class _HistoryEmptyList extends StatelessWidget {
  const _HistoryEmptyList();

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: const [
        AppReveal(child: _HistoryEmptyCard()),
      ],
    );
  }
}

class _HistoryEmptyCard extends StatelessWidget {
  const _HistoryEmptyCard();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Icon(
              Icons.history_rounded,
              size: 48,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text('还没有播放记录', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('设备播放内容后，记录会出现在这里。', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _FilterCard extends StatelessWidget {
  const _FilterCard({required this.filter, required this.onChanged});

  final ContentLibraryFilter filter;
  final ValueChanged<ContentLibraryFilter> onChanged;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('挑选内容', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('按宝贝感兴趣的类别和年龄挑选，下载后设备离线也能使用。'),
            const SizedBox(height: 16),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  _FilterChip(
                    label: '全部',
                    selected: filter.category == null,
                    onSelected: () =>
                        onChanged(filter.copyWith(clearCategory: true)),
                  ),
                  for (final category in ContentCategory.values)
                    _FilterChip(
                      label: category.label,
                      selected: filter.category == category,
                      onSelected: () =>
                          onChanged(filter.copyWith(category: category)),
                    ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  _FilterChip(
                    label: '全部年龄',
                    selected: filter.ageTier == null,
                    onSelected: () =>
                        onChanged(filter.copyWith(clearAgeTier: true)),
                  ),
                  for (final tier in ContentAgeTier.values)
                    _FilterChip(
                      label: tier.label,
                      selected: filter.ageTier == tier,
                      onSelected: () =>
                          onChanged(filter.copyWith(ageTier: tier)),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _FilterChip extends StatelessWidget {
  const _FilterChip({
    required this.label,
    required this.selected,
    required this.onSelected,
  });

  final String label;
  final bool selected;
  final VoidCallback onSelected;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(right: 8),
      child: ChoiceChip(
        label: Text(label),
        selected: selected,
        onSelected: (_) => onSelected(),
      ),
    );
  }
}

class _PackageCard extends ConsumerWidget {
  const _PackageCard({required this.entry});

  final ContentLibraryEntry entry;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final package = entry.package;
    final favorites = ref.watch(contentFavoritesControllerProvider);
    final favoriteState = favorites.value;
    final isFavorite = favoriteState?.isFavorite(package.packageId) ?? false;
    final isFavoritePending =
        favoriteState?.isPending(package.packageId) ?? false;
    return Card(
      child: ExpansionTile(
        tilePadding: const EdgeInsets.fromLTRB(18, 8, 14, 8),
        childrenPadding: const EdgeInsets.fromLTRB(18, 0, 18, 18),
        leading: CircleAvatar(
          backgroundColor: Theme.of(context).colorScheme.primaryContainer,
          child: Icon(_categoryIcon(package.category)),
        ),
        title: Text(package.title),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(
            '${package.category.label} · '
            '${package.ageTiers.map((tier) => tier.label).join('、')} · '
            '${formatContentSize(package.sizeBytes)}',
          ),
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            IconButton(
              tooltip: isFavorite ? '取消收藏' : '收藏',
              onPressed: isFavoritePending
                  ? null
                  : () => ref
                        .read(contentFavoritesControllerProvider.notifier)
                        .toggle(package.packageId),
              icon: isFavoritePending
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      isFavorite ? Icons.star_rounded : Icons.star_border_rounded,
                      color: isFavorite
                          ? const Color(0xFFE0A800)
                          : Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
            ),
            _StatusBadge(status: entry.status),
          ],
        ),
        children: [
          const Divider(height: 1),
          const SizedBox(height: 14),
          _InfoRow(
            label: '版本',
            value: '第 ${package.packageVersion} 版',
          ),
          _InfoRow(label: '发布时间', value: _formatDate(package.publishedAt)),
          _InfoRow(
            label: '状态',
            value: _statusLabel(entry.status, entry.downloadProgress),
          ),
          if (entry.errorMessage != null) ...[
            const SizedBox(height: 10),
            Text(
              entry.errorMessage!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ],
          if (entry.status == ContentDownloadStatus.downloading) ...[
            const SizedBox(height: 16),
            LinearProgressIndicator(value: entry.downloadProgress),
            const SizedBox(height: 8),
            Text('已下载 ${(entry.downloadProgress * 100).round()}%'),
          ],
          const SizedBox(height: 16),
          Row(
            children: [
              if (entry.status == ContentDownloadStatus.downloading)
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: () => ref
                        .read(contentLibraryControllerProvider.notifier)
                        .cancelDownload(package.packageId),
                    icon: const Icon(Icons.stop_circle_outlined),
                    label: const Text('取消下载'),
                  ),
                )
              else if (entry.canDownload)
                Expanded(
                  child: FilledButton.icon(
                    onPressed: () => ref
                        .read(contentLibraryControllerProvider.notifier)
                        .download(package.packageId),
                    icon: const Icon(Icons.download_rounded),
                    label: Text(
                      entry.status == ContentDownloadStatus.updateAvailable
                          ? '更新内容'
                          : '下载内容',
                    ),
                  ),
                ),
              if (entry.canDownload && entry.canDelete) ...[
                const SizedBox(width: 10),
                OutlinedButton(
                  onPressed: () => _confirmDelete(
                    context,
                    ref,
                    package.title,
                    package.packageId,
                  ),
                  child: const Text('删除'),
                ),
              ],
            ],
          ),
          if (entry.status == ContentDownloadStatus.downloaded) ...[
            const SizedBox(height: 10),
            OutlinedButton.icon(
              onPressed: () => _confirmDelete(
                context,
                ref,
                package.title,
                package.packageId,
              ),
              icon: const Icon(Icons.delete_outline_rounded),
              label: const Text('删除已下载内容'),
            ),
          ],
          if (entry.status == ContentDownloadStatus.withdrawn)
            const Padding(
              padding: EdgeInsets.only(top: 8),
              child: Text('这项内容已下架，本地文件已经清理。'),
            ),
        ],
      ),
    );
  }
}

class _StatusBadge extends StatelessWidget {
  const _StatusBadge({required this.status});

  final ContentDownloadStatus status;

  @override
  Widget build(BuildContext context) {
    final color = switch (status) {
      ContentDownloadStatus.downloaded => const Color(0xFF2E7D55),
      ContentDownloadStatus.updateAvailable => const Color(0xFF8A6D00),
      ContentDownloadStatus.withdrawn => Theme.of(context).colorScheme.error,
      ContentDownloadStatus.downloading => Theme.of(
        context,
      ).colorScheme.primary,
      ContentDownloadStatus.notDownloaded => Theme.of(
        context,
      ).colorScheme.onSurfaceVariant,
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Text(
        _statusLabel(status, 0),
        style: TextStyle(color: color, fontWeight: FontWeight.w600),
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  const _InfoRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          SizedBox(
            width: 76,
            child: Text(label, style: Theme.of(context).textTheme.bodySmall),
          ),
          Expanded(child: Text(value)),
        ],
      ),
    );
  }
}

class _EmptyContentCard extends StatelessWidget {
  const _EmptyContentCard();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Icon(
              Icons.auto_stories_outlined,
              size: 48,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text('这里还没有适合的内容', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('换个分类或年龄试试，也可以稍后下拉刷新。', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry, super.key});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_off_outlined,
              size: 46,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

Future<void> _confirmDelete(
  BuildContext context,
  WidgetRef ref,
  String title,
  String packageId,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('删除已下载内容'),
      content: Text('删除“$title”后，离线时设备将无法播放这项内容。可以重新下载。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('保留'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('删除'),
        ),
      ],
    ),
  );
  if (confirmed == true) {
    await ref.read(contentLibraryControllerProvider.notifier).delete(packageId);
  }
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/me');
  }
}

IconData _categoryIcon(ContentCategory category) {
  return switch (category) {
    ContentCategory.story => Icons.menu_book_rounded,
    ContentCategory.nurseryRhyme => Icons.music_note_rounded,
    ContentCategory.poetry => Icons.history_edu_rounded,
    ContentCategory.english => Icons.abc_rounded,
    ContentCategory.encyclopedia => Icons.explore_rounded,
    ContentCategory.bedtime => Icons.nightlight_round,
  };
}

String _statusLabel(ContentDownloadStatus status, double progress) {
  return switch (status) {
    ContentDownloadStatus.notDownloaded => '未下载',
    ContentDownloadStatus.downloading => '下载中 ${(progress * 100).round()}%',
    ContentDownloadStatus.downloaded => '已下载',
    ContentDownloadStatus.updateAvailable => '需更新',
    ContentDownloadStatus.withdrawn => '已撤回',
  };
}

String _favoriteSubtitle(ContentFavorite favorite) {
  final parts = <String>[
    favorite.category.label,
    if (favorite.ageTiers.isNotEmpty)
      favorite.ageTiers.map((tier) => tier.label).join('、'),
    if (!favorite.available) '已下架',
  ];
  return parts.join(' · ');
}

IconData _outcomeIcon(PlayOutcome outcome) {
  return switch (outcome) {
    PlayOutcome.completed => Icons.check_circle_rounded,
    PlayOutcome.skipped => Icons.skip_next_rounded,
    PlayOutcome.interrupted => Icons.pause_circle_rounded,
    PlayOutcome.failed => Icons.error_rounded,
  };
}

Color _outcomeColor(BuildContext context, PlayOutcome outcome) {
  return switch (outcome) {
    PlayOutcome.completed => const Color(0xFF2E7D55),
    PlayOutcome.skipped => Theme.of(context).colorScheme.onSurfaceVariant,
    PlayOutcome.interrupted => const Color(0xFF8A6D00),
    PlayOutcome.failed => Theme.of(context).colorScheme.error,
  };
}

String _historySubtitle(ContentPlayRecord record) {
  final parts = <String>[
    _deviceLabel(record.deviceId),
    '第 ${record.packageVersion} 版',
    '${_formatDuration(record.playedSeconds)} · ${_formatDateTime(record.playedAt)}',
  ];
  return parts.join(' · ');
}

String _deviceLabel(String deviceId) {
  // Device ids are opaque; show a stable short suffix so a guardian can tell
  // two devices apart without exposing the full identifier.
  if (deviceId.length <= 6) {
    return deviceId;
  }
  return '设备 ${deviceId.substring(deviceId.length - 6)}';
}

String _formatDuration(int seconds) {
  if (seconds < 60) {
    return '$seconds 秒';
  }
  final minutes = seconds ~/ 60;
  final remainder = seconds % 60;
  if (remainder == 0) {
    return '$minutes 分钟';
  }
  return '$minutes 分 $remainder 秒';
}

String _formatDateTime(DateTime value) {
  final local = value.toLocal();
  final hour = local.hour.toString().padLeft(2, '0');
  final minute = local.minute.toString().padLeft(2, '0');
  return '${local.month}月${local.day}日 $hour:$minute';
}

String _formatDate(DateTime value) {
  final local = value.toLocal();
  return '${local.year}年${local.month}月${local.day}日';
}

String _messageFor(Object error) {
  if (error is AppException) {
    return error.message;
  }
  return '内容暂时没有加载出来，请稍后重试';
}
