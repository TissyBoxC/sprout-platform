import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/notification/application/notification_controller.dart';
import 'package:parent_app/features/notification/data/notification_api.dart';
import 'package:parent_app/features/notification/domain/notification.dart';

void main() {
  test('loads the first page and exposes the unread badge', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [
            _notification('n-1', isRead: false),
            _notification('n-2', isRead: true),
          ],
          unreadCount: 1,
          nextCursor: 'cursor-2',
          hasMore: true,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(notificationControllerProvider.future);

    expect(state.items, hasLength(2));
    expect(state.unreadCount, 1);
    expect(state.hasMore, isTrue);
    expect(container.read(notificationUnreadProvider), 1);
    expect(api.listCalls, 1);
    expect(api.lastCursor, isNull);
  });

  test('appends the next page and keeps the merged list', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1')],
          unreadCount: 2,
          nextCursor: 'cursor-2',
          hasMore: true,
        ),
        _page(
          items: [_notification('n-2')],
          unreadCount: 2,
          nextCursor: '',
          hasMore: false,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(notificationControllerProvider.future);
    await container.read(notificationControllerProvider.notifier).loadMore();

    final state = container.read(notificationControllerProvider).value;
    expect(api.lastCursor, 'cursor-2');
    expect(state?.items.map((item) => item.id), <String>['n-1', 'n-2']);
    expect(state?.hasMore, isFalse);
    expect(state?.nextCursor, '');
  });

  test('does not load another page when the cursor is absent', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1')],
          unreadCount: 0,
          nextCursor: '',
          hasMore: true,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(notificationControllerProvider.future);
    await container.read(notificationControllerProvider.notifier).loadMore();

    expect(api.listCalls, 1);
  });

  test('exposes a retryable error when loading the next page fails', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1')],
          unreadCount: 0,
          nextCursor: 'cursor-2',
          hasMore: true,
        ),
      ],
      failAfterFirst: true,
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(notificationControllerProvider.future);
    await container.read(notificationControllerProvider.notifier).loadMore();

    final state = container.read(notificationControllerProvider).value;
    expect(state?.items.map((item) => item.id), <String>['n-1']);
    expect(state?.hasMore, isTrue);
    expect(state?.loadMoreErrorMessage, '网络不稳定');
  });

  test('marks one notification read and decrements the badge', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1', isRead: false)],
          unreadCount: 1,
          nextCursor: '',
          hasMore: false,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(notificationControllerProvider.future);
    final result = await container
        .read(notificationControllerProvider.notifier)
        .markRead(state.items.single);

    final updated = container.read(notificationControllerProvider).value;
    expect(result.didChange, isTrue);
    expect(result.didFail, isFalse);
    expect(api.markedReadIds, <String>['n-1']);
    expect(updated?.items.single.isRead, isTrue);
    expect(updated?.unreadCount, 0);
  });

  test('restores the badge when the server rejects mark-read', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1', isRead: false)],
          unreadCount: 1,
          nextCursor: '',
          hasMore: false,
        ),
      ],
      failMarkRead: true,
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(notificationControllerProvider.future);
    final result = await container
        .read(notificationControllerProvider.notifier)
        .markRead(state.items.single);

    final restored = container.read(notificationControllerProvider).value;
    expect(result.didFail, isTrue);
    expect(result.errorMessage, '消息状态没有更新，请稍后重试');
    expect(restored?.items.single.isRead, isFalse);
    expect(restored?.unreadCount, 1);
  });

  test('marks every notification read and clears the badge', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1'), _notification('n-2')],
          unreadCount: 2,
          nextCursor: '',
          hasMore: false,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(notificationControllerProvider.future);
    final result = await container
        .read(notificationControllerProvider.notifier)
        .markAllRead();

    final state = container.read(notificationControllerProvider).value;
    expect(result.didChange, isTrue);
    expect(api.markAllReadCalls, 1);
    expect(state?.items.every((item) => item.isRead), isTrue);
    expect(state?.unreadCount, 0);
  });

  test('keeps the last good inbox when a refresh fails', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1')],
          unreadCount: 1,
          nextCursor: '',
          hasMore: false,
        ),
      ],
      failAfterFirst: true,
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(notificationControllerProvider.future);
    await container.read(notificationControllerProvider.notifier).refresh();

    final state = container.read(notificationControllerProvider);
    expect(state.hasError, isFalse);
    expect(state.value?.items.single.id, 'n-1');
  });

  test('maps an AppException to its message and hides other errors', () {
    expect(
      notificationErrorMessage(
        const AppException(
          kind: AppErrorKind.unexpected,
          message: '网络不稳定',
          retryable: true,
        ),
      ),
      '网络不稳定',
    );
    expect(notificationErrorMessage(StateError('boom')), '暂时无法读取消息，请稍后重试');
  });

  test('maps permission and network failures without hiding read state', () {
    expect(
      notificationMutationMessage(
        const AppException(
          kind: AppErrorKind.insufficientPermission,
          message: 'forbidden',
          retryable: false,
        ),
        markingAll: false,
      ),
      '当前家庭角色不能标记这条消息，消息状态没有变化',
    );
    expect(
      notificationMutationMessage(
        const AppException(
          kind: AppErrorKind.network,
          message: 'offline',
          retryable: true,
        ),
        markingAll: true,
      ),
      '网络连接不稳定，消息状态没有更新，请稍后重试',
    );
  });

  test('mark-all restores unread state when the server fails', () async {
    final api = _FakeNotificationApi(
      pages: [
        _page(
          items: [_notification('n-1'), _notification('n-2')],
          unreadCount: 2,
          nextCursor: '',
          hasMore: false,
        ),
      ],
      failMarkAllRead: true,
    );
    final container = ProviderContainer(
      overrides: [notificationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(notificationControllerProvider.future);
    final result = await container
        .read(notificationControllerProvider.notifier)
        .markAllRead();
    final state = container.read(notificationControllerProvider).value;

    expect(result.didFail, isTrue);
    expect(state?.unreadCount, 2);
    expect(state?.items.every((item) => !item.isRead), isTrue);
  });
}

GuardianNotification _notification(String id, {bool isRead = false}) {
  return GuardianNotification(
    id: id,
    category: 'family_message',
    severity: 'info',
    title: '留言',
    body: '早点休息',
    actionPath: '',
    actionLabel: '',
    deviceId: 'device-1',
    channels: const ['in_app'],
    isRead: isRead,
    publishAt: DateTime.utc(2026, 10, 9),
    expiresAt: null,
  );
}

GuardianNotificationPage _page({
  required List<GuardianNotification> items,
  required int unreadCount,
  required String nextCursor,
  required bool hasMore,
}) {
  return GuardianNotificationPage(
    items: items,
    unreadCount: unreadCount,
    nextCursor: nextCursor,
    hasMore: hasMore,
  );
}

class _FakeNotificationApi implements NotificationApi {
  _FakeNotificationApi({
    required this.pages,
    this.failMarkRead = false,
    this.failMarkAllRead = false,
    this.failAfterFirst = false,
  });

  final List<GuardianNotificationPage> pages;
  final bool failMarkRead;
  final bool failMarkAllRead;
  final bool failAfterFirst;

  int listCalls = 0;
  String? lastCursor;
  final List<String> markedReadIds = <String>[];
  int markAllReadCalls = 0;

  @override
  Future<GuardianNotificationPage> list({
    int limit = 20,
    String? cursor,
    String? category,
  }) async {
    if (failAfterFirst && listCalls >= 1) {
      throw const AppException(
        kind: AppErrorKind.unexpected,
        message: '网络不稳定',
        retryable: true,
      );
    }
    lastCursor = cursor;
    final page = pages[listCalls < pages.length ? listCalls : pages.length - 1];
    listCalls += 1;
    return page;
  }

  @override
  Future<int> unreadCount() async {
    return pages.isEmpty ? 0 : pages.first.unreadCount;
  }

  @override
  Future<void> markRead(String notificationId) async {
    if (failMarkRead) {
      throw Exception('rejected');
    }
    markedReadIds.add(notificationId);
  }

  @override
  Future<void> markAllRead() async {
    markAllReadCalls += 1;
    if (failMarkAllRead) {
      throw const AppException(
        kind: AppErrorKind.network,
        message: 'offline',
        retryable: true,
      );
    }
  }

  @override
  Future<void> sendFamilyMessage({
    required String deviceId,
    required String body,
    required bool sendToDevice,
    int displayDurationSeconds = 30,
  }) async {}
}
