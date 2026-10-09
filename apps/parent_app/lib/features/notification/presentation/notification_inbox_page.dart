import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/notification_controller.dart';
import '../domain/notification.dart';

/// Guardian notification inbox with read state and family-message access.
class NotificationInboxPage extends ConsumerWidget {
  const NotificationInboxPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final inbox = ref.watch(notificationControllerProvider);
    final unread = ref.watch(notificationUnreadProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('消息'),
        actions: [
          if (unread > 0)
            TextButton(
              onPressed: () => ref
                  .read(notificationControllerProvider.notifier)
                  .markAllRead(),
              child: const Text('全部已读'),
            ),
          IconButton(
            tooltip: '给设备留言',
            onPressed: () => context.push('/me/notifications/message'),
            icon: const Icon(Icons.chat_bubble_outline_rounded),
          ),
          const SizedBox(width: 4),
        ],
      ),
      body: AppStateSwitcher(
        stateKey: inbox.when(
          data: (value) => value.items.isEmpty
              ? 'notifications-empty'
              : 'notifications-content',
          error: (_, _) => 'notifications-error',
          loading: () => 'notifications-loading',
        ),
        child: inbox.when(
          loading: () => const Center(
            key: ValueKey<String>('notifications-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('notifications-error'),
            message: notificationErrorMessage(error),
            onRetry: () =>
                ref.read(notificationControllerProvider.notifier).refresh(),
          ),
          data: (state) {
            if (state.items.isEmpty) {
              return _EmptyState(
                key: const ValueKey<String>('notifications-empty'),
                onRetry: () =>
                    ref.read(notificationControllerProvider.notifier).refresh(),
                onCompose: () => context.push('/me/notifications/message'),
              );
            }
            return RefreshIndicator(
              key: const ValueKey<String>('notifications-content'),
              onRefresh: () =>
                  ref.read(notificationControllerProvider.notifier).refresh(),
              child: NotificationListener<ScrollNotification>(
                onNotification: (notification) {
                  if (notification.metrics.pixels >=
                      notification.metrics.maxScrollExtent - 240) {
                    ref
                        .read(notificationControllerProvider.notifier)
                        .loadMore();
                  }
                  return false;
                },
                child: ListView(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 28),
                  children: [
                    for (final item in state.items)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: AppReveal(
                          child: _NotificationCard(
                            notification: item,
                            onTap: () => _openNotification(context, ref, item),
                          ),
                        ),
                      ),
                    if (state.hasMore)
                      const Padding(
                        padding: EdgeInsets.only(top: 4, bottom: 8),
                        child: Center(
                          child: SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            );
          },
        ),
      ),
    );
  }

  Future<void> _openNotification(
    BuildContext context,
    WidgetRef ref,
    GuardianNotification notification,
  ) async {
    await ref
        .read(notificationControllerProvider.notifier)
        .markRead(notification);
    if (!context.mounted) {
      return;
    }
    final path = notification.actionPath.trim();
    if (path.startsWith('/')) {
      context.push(path);
    }
  }
}

class _NotificationCard extends StatelessWidget {
  const _NotificationCard({required this.notification, required this.onTap});

  final GuardianNotification notification;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final accent = _severityColor(theme, notification.severity);
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 5,
                    ),
                    decoration: BoxDecoration(
                      color: accent.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(99),
                    ),
                    child: Text(
                      notificationCategoryLabel(notification.category),
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: accent,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  if (!notification.isRead)
                    Container(
                      width: 8,
                      height: 8,
                      decoration: BoxDecoration(
                        color: theme.colorScheme.primary,
                        shape: BoxShape.circle,
                      ),
                    ),
                  const Spacer(),
                  Text(
                    _timeLabel(notification.publishAt),
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Text(
                notification.title,
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: notification.isRead
                      ? FontWeight.w600
                      : FontWeight.w800,
                ),
              ),
              if (notification.body.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(
                  notification.body,
                  style: theme.textTheme.bodyMedium,
                ),
              ],
              if (notification.hasAction) ...[
                const SizedBox(height: 10),
                Row(
                  children: [
                    Text(
                      notification.actionLabel.isEmpty
                          ? '查看详情'
                          : notification.actionLabel,
                      style: theme.textTheme.labelLarge?.copyWith(
                        color: theme.colorScheme.primary,
                      ),
                    ),
                    const SizedBox(width: 4),
                    Icon(
                      Icons.chevron_right_rounded,
                      size: 18,
                      color: theme.colorScheme.primary,
                    ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.onRetry, required this.onCompose, super.key});

  final Future<void> Function() onRetry;
  final VoidCallback onCompose;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const SizedBox(height: 48),
        Icon(
          Icons.mark_email_unread_outlined,
          size: 48,
          color: Theme.of(context).colorScheme.primary,
        ),
        const SizedBox(height: 16),
        Text(
          '还没有新消息',
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.titleMedium,
        ),
        const SizedBox(height: 6),
        const Text(
          '设备状态、内容更新和平台通知会显示在这里。',
          textAlign: TextAlign.center,
        ),
        const SizedBox(height: 20),
        FilledButton.icon(
          onPressed: onCompose,
          icon: const Icon(Icons.chat_bubble_outline_rounded),
          label: const Text('给设备留言'),
        ),
        const SizedBox(height: 10),
        OutlinedButton(onPressed: onRetry, child: const Text('重新加载')),
      ],
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
            const Icon(Icons.cloud_off_outlined, size: 42),
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

Color _severityColor(ThemeData theme, String severity) {
  switch (severity) {
    case 'success':
      return const Color(0xFF2E7D5B);
    case 'warning':
      return const Color(0xFFB7791F);
    case 'critical':
      return const Color(0xFFB12848);
    default:
      return theme.colorScheme.primary;
  }
}

String _timeLabel(DateTime? time) {
  if (time == null) {
    return '';
  }
  final local = time.toLocal();
  final now = DateTime.now();
  final sameDay =
      local.year == now.year && local.month == now.month && local.day == now.day;
  if (sameDay) {
    final hour = local.hour.toString().padLeft(2, '0');
    final minute = local.minute.toString().padLeft(2, '0');
    return '$hour:$minute';
  }
  return '${local.month} 月 ${local.day} 日';
}
