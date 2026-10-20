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
    this.loadMoreErrorMessage,
  });

  const NotificationInboxState.empty()
    : items = const [],
      unreadCount = 0,
      hasMore = false,
      nextCursor = '',
      loadMoreErrorMessage = null;

  final List<GuardianNotification> items;
  final int unreadCount;
  final bool hasMore;
  final String nextCursor;
  final String? loadMoreErrorMessage;

  NotificationInboxState copyWith({
    List<GuardianNotification>? items,
    int? unreadCount,
    bool? hasMore,
    String? nextCursor,
    String? loadMoreErrorMessage,
    bool clearLoadMoreError = false,
  }) {
    return NotificationInboxState(
      items: items ?? this.items,
      unreadCount: unreadCount ?? this.unreadCount,
      hasMore: hasMore ?? this.hasMore,
      nextCursor: nextCursor ?? this.nextCursor,
      loadMoreErrorMessage: clearLoadMoreError
          ? null
          : loadMoreErrorMessage ?? this.loadMoreErrorMessage,
    );
  }
}

/// Loads the guardian notification inbox and mutates read state.
class NotificationController extends AsyncNotifier<NotificationInboxState> {
  static const _pageSize = 20;

  late NotificationApi _api;
  bool _isLoadingMore = false;

  bool get isLoadingMore => _isLoadingMore;

  @override
  Future<NotificationInboxState> build() async {
    _api = ref.read(notificationApiProvider);
    _isLoadingMore = false;
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
      final page = await _api.list(
        limit: _pageSize,
        cursor: current.nextCursor,
      );
      state = AsyncData(
        current.copyWith(
          items: [...current.items, ...page.items],
          unreadCount: page.unreadCount,
          hasMore: page.hasMore,
          nextCursor: page.nextCursor,
          clearLoadMoreError: true,
        ),
      );
    } on Object catch (error) {
      final latest = state.value;
      if (latest != null) {
        state = AsyncData(
          latest.copyWith(
            loadMoreErrorMessage: notificationErrorMessage(error),
          ),
        );
      }
    } finally {
      _isLoadingMore = false;
    }
  }

  Future<NotificationMutationResult> markRead(
    GuardianNotification notification,
  ) async {
    if (notification.isRead) {
      return const NotificationMutationResult.alreadyApplied();
    }
    final current = state.value;
    if (current == null) {
      return const NotificationMutationResult.unavailable();
    }
    if (!current.items.any((item) => item.id == notification.id)) {
      return const NotificationMutationResult.unavailable();
    }
    GuardianNotification? existing;
    for (final item in current.items) {
      if (item.id == notification.id) {
        existing = item;
        break;
      }
    }
    if (existing == null) {
      return const NotificationMutationResult.unavailable();
    }
    if (existing.isRead) {
      return const NotificationMutationResult.alreadyApplied();
    }
    final updated = current.items
        .map((item) => item.id == notification.id ? _copyAsRead(item) : item)
        .toList(growable: false);
    state = AsyncData(
      current.copyWith(
        items: updated,
        unreadCount: (current.unreadCount - 1).clamp(0, 9999),
      ),
    );
    try {
      await _api.markRead(notification.id);
      return const NotificationMutationResult.succeeded();
    } on Object catch (error) {
      // Restore the previous badge if the server rejected the change.
      final latest = state.value;
      if (latest != null) {
        state = AsyncData(
          latest.copyWith(
            items: latest.items
                .map<GuardianNotification>(
                  (item) => item.id == notification.id ? existing! : item,
                )
                .toList(growable: false),
            unreadCount: latest.unreadCount + 1,
          ),
        );
      }
      return NotificationMutationResult.failed(
        notificationMutationMessage(error, markingAll: false),
      );
    }
  }

  Future<NotificationMutationResult> markAllRead() async {
    final current = state.value;
    if (current == null || current.unreadCount == 0) {
      return const NotificationMutationResult.alreadyApplied();
    }
    state = AsyncData(
      current.copyWith(
        items: current.items.map(_copyAsRead).toList(growable: false),
        unreadCount: 0,
      ),
    );
    try {
      await _api.markAllRead();
      return const NotificationMutationResult.succeeded();
    } on Object catch (error) {
      final latest = state.value;
      if (latest != null) {
        final unreadById = {
          for (final item in current.items)
            if (!item.isRead) item.id: item,
        };
        state = AsyncData(
          latest.copyWith(
            items: latest.items
                .map<GuardianNotification>(
                  (item) => item.isRead ? (unreadById[item.id] ?? item) : item,
                )
                .toList(growable: false),
            unreadCount: current.unreadCount,
          ),
        );
      }
      return NotificationMutationResult.failed(
        notificationMutationMessage(error, markingAll: true),
      );
    }
  }
}

/// Result of one read-state mutation for page-level feedback.
class NotificationMutationResult {
  const NotificationMutationResult._({
    required this.didChange,
    this.errorMessage,
  });

  const NotificationMutationResult.succeeded() : this._(didChange: true);

  const NotificationMutationResult.alreadyApplied() : this._(didChange: false);

  const NotificationMutationResult.unavailable()
    : this._(didChange: false, errorMessage: '消息状态暂时无法更新，请稍后重试');

  const NotificationMutationResult.failed(String message)
    : this._(didChange: false, errorMessage: message);

  final bool didChange;
  final String? errorMessage;

  bool get didFail => errorMessage != null;
}

/// Maps read-state failures to copy that keeps the message visible and the
/// retry decision explicit.
String notificationMutationMessage(Object error, {required bool markingAll}) {
  if (error is AppException) {
    return switch (error.kind) {
      AppErrorKind.unauthenticated => '登录已过期，消息仍未标记为已读，请重新登录后再试',
      AppErrorKind.insufficientPermission =>
        markingAll ? '当前家庭角色不能全部标记已读，消息状态没有变化' : '当前家庭角色不能标记这条消息，消息状态没有变化',
      AppErrorKind.network => '网络连接不稳定，消息状态没有更新，请稍后重试',
      AppErrorKind.serviceUnavailable => '服务暂时不可用，消息状态没有更新，请稍后重试',
      _ => '消息状态没有更新，请稍后重试',
    };
  }
  return '消息状态没有更新，请稍后重试';
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
