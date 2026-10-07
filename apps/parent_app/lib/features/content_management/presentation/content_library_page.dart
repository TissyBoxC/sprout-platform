import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/content_library_controller.dart';
import '../domain/content_package.dart';

/// Guardian-facing content library with filters and download controls.
class ContentLibraryPage extends ConsumerWidget {
  const ContentLibraryPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
        body: AppStateSwitcher(
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
              onRetry: () =>
                  ref.read(contentLibraryControllerProvider.notifier).refresh(),
            ),
            data: (state) => _ContentList(
              key: const ValueKey<String>('content-content'),
              state: state,
            ),
          ),
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
        trailing: _StatusBadge(status: entry.status),
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
