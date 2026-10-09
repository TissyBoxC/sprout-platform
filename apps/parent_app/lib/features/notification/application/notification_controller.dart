import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/notification_api.dart';
import '../domain/notification.dart';

/// Inbox state: one loaded page plus its unread badge.
class NotificationInboxState {
  const NotificationInboxState({
    required this.items,
    required this.unreadCount,
    required this.hasMore,
    required this.nextCursor,
  });

  const NotificationInboxState.empty()
    : items = const [],
      unreadCount = 0,
      hasMore = false,
      nextCursor = '';

  final List<GuardianNotification> items;
  final int unreadCount;
  final bool hasMore;
  final String nextCursor;

  NotificationInboxState copyWith({
    List<GuardianNotification>? items,
    int? unreadCount,
    bool? hasMore,
    String? nextCursor,
  }) {
    return NotificationInboxState(
      items: items ?? this.items,
      unreadCount: unreadCount ?? this.unreadCount,
      hasMore: hasMore ?? this.hasMore,
      nextCursor: nextCursor ?? this.nextCursor,
    );
  }
}

/// Loads the guardian notification inbox and mutates read state.
class NotificationController extends AsyncNotifier<NotificationInboxState> {
  static const _pageSize = 20;

  late final NotificationApi _api;
  bool _isLoadingMore = false;

  bool get isLoadingMore => _isLoadingMore;

  @override
  Future<NotificationInboxState> build() async {
    _api = ref.read(notificationApiProvider);
    return _loadFirstPage();
  }

  Future<NotificationInboxState> _loadFirstPage() async {
    final page = await _api.list(limit: _pageSize);
    return NotificationInboxState(
      items: page.items,
      unreadCount: page.unreadCount,
      hasMore: page.hasMore,
      nextCursor: page.nextCursor,
    );
  }

  Future<void> refresh() async {
    final current = state.value;
    final next = await AsyncValue.guard(_loadFirstPage);
    if (next.hasValue) {
      state = next;
    } else if (current != null) {
      // Keep the last good inbox visible when a refresh fails.
      state = AsyncData(current);
    } else {
      state = next;
    }
  }

  Future<void> loadMore() async {
    final current = state.value;
    if (current == null ||
        !current.hasMore ||
        current.nextCursor.isEmpty ||
        _isLoadingMore) {
      return;
    }
    _isLoadingMore = true;
    try {
      final page = await _api.list(limit: _pageSize, cursor: current.nextCursor);
      state = AsyncData(
        current.copyWith(
          items: [...current.items, ...page.items],
          unreadCount: page.unreadCount,
          hasMore: page.hasMore,
          nextCursor: page.nextCursor,
        ),
      );
    } on Object {
      // A failed "load more" keeps the already loaded notifications.
    } finally {
      _isLoadingMore = false;
    }
  }

  Future<void> markRead(GuardianNotification notification) async {
    if (notification.isRead) {
      return;
    }
    final current = state.value;
    if (current == null) {
      return;
    }
    final updated = current.items
        .map(
          (item) => item.id == notification.id
              ? _copyAsRead(item)
              : item,
        )
        .toList(growable: false);
    state = AsyncData(
      current.copyWith(
        items: updated,
        unreadCount: (current.unreadCount - 1).clamp(0, 9999),
      ),
    );
    try {
      await _api.markRead(notification.id);
    } on Object {
      // Restore the previous badge if the server rejected the change.
      state = AsyncData(current);
    }
  }

  Future<void> markAllRead() async {
    final current = state.value;
    if (current == null || current.unreadCount == 0) {
      return;
    }
    state = AsyncData(
      current.copyWith(
        items: current.items.map(_copyAsRead).toList(growable: false),
        unreadCount: 0,
      ),
    );
    try {
      await _api.markAllRead();
    } on Object {
      state = AsyncData(current);
    }
  }
}

GuardianNotification _copyAsRead(GuardianNotification notification) {
  return GuardianNotification(
    id: notification.id,
    category: notification.category,
    severity: notification.severity,
    title: notification.title,
    body: notification.body,
    actionPath: notification.actionPath,
    actionLabel: notification.actionLabel,
    deviceId: notification.deviceId,
    channels: notification.channels,
    isRead: true,
    publishAt: notification.publishAt,
    expiresAt: notification.expiresAt,
  );
}

final notificationApiProvider = Provider<NotificationApi>((ref) {
  return NotificationApi(ref.read(apiClientProvider));
});

final notificationControllerProvider =
    AsyncNotifierProvider<NotificationController, NotificationInboxState>(
      NotificationController.new,
    );

/// Unread badge kept in sync with the inbox controller.
final notificationUnreadProvider = Provider<int>((ref) {
  final state = ref.watch(notificationControllerProvider);
  return state.value?.unreadCount ?? 0;
});

/// Returns copy safe to display for the notification module.
String notificationErrorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时无法读取消息，请稍后重试';
}
