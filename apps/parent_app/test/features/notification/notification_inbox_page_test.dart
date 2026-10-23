import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/notification/application/notification_controller.dart';
import 'package:parent_app/features/notification/data/notification_api.dart';
import 'package:parent_app/features/notification/domain/notification.dart';
import 'package:parent_app/features/notification/presentation/notification_inbox_page.dart';

void main() {
  final rootNavigatorKey = GlobalKey<NavigatorState>();
  testWidgets('opens a valid destination before the read request finishes', (
    tester,
  ) async {
    final readGate = Completer<void>();
    final api = _InboxApi(readGate: readGate);
    final router = _router(api, rootNavigatorKey);

    await tester.pumpWidget(_app(api, router: router));
    await tester.pumpAndSettle();
      expect(find.text('设备离线'), findsOneWidget);
      await tester.tap(find.text('设备离线'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

    expect(find.text('设备详情'), findsOneWidget);
    expect(api.markReadStarted, isTrue);
    expect(api.markReadIds, isEmpty);

    readGate.complete();
    await tester.pumpAndSettle();
    expect(api.markReadIds, <String>['notice-1']);
  });

  testWidgets('shows a visible error when a read request is forbidden', (
    tester,
  ) async {
    final api = _InboxApi(
      readError: const AppException(
        kind: AppErrorKind.insufficientPermission,
        message: 'forbidden',
        retryable: false,
      ),
    );
    final router = _router(api, rootNavigatorKey);

    await tester.pumpWidget(_app(api, router: router));
    await tester.pumpAndSettle();
    await tester.tap(find.text('设备离线'));
    await tester.pumpAndSettle();

    expect(find.text('设备详情'), findsOneWidget);
    expect(find.text('当前家庭角色不能标记这条消息，消息状态没有变化'), findsOneWidget);
  });

  testWidgets('shows a visible network error without blocking navigation', (
    tester,
  ) async {
    final api = _InboxApi(
      readError: const AppException(
        kind: AppErrorKind.network,
        message: 'offline',
        retryable: true,
      ),
    );
    final router = _router(api, rootNavigatorKey);

    await tester.pumpWidget(_app(api, router: router));
    await tester.pumpAndSettle();
    await tester.tap(find.text('设备离线'));
    await tester.pumpAndSettle();

    expect(find.text('设备详情'), findsOneWidget);
    expect(find.text('网络连接不稳定，消息状态没有更新，请稍后重试'), findsOneWidget);
  });

  testWidgets('a malformed action path never leaves the inbox', (tester) async {
    final api = _InboxApi(
      item: _notification(actionPath: 'https://example.com/phish'),
    );
    final router = _router(api, rootNavigatorKey);

    await tester.pumpWidget(_app(api, router: router));
    await tester.pumpAndSettle();
    await tester.tap(find.text('设备离线'));
    await tester.pumpAndSettle();

    expect(find.text('消息'), findsOneWidget);
    expect(find.text('这条消息没有可打开的页面，内容仍可在消息列表中查看'), findsOneWidget);
    await tester.pump(const Duration(milliseconds: 300));
    expect(api.markReadIds, <String>['notice-1']);
    expect(router.routerDelegate.currentConfiguration.uri.path, '/inbox');
  });

  testWidgets('all-read failure restores the badge and shows the reason', (
    tester,
  ) async {
    final api = _InboxApi(
      markAllError: const AppException(
        kind: AppErrorKind.network,
        message: 'offline',
        retryable: true,
      ),
    );
    final router = _router(api, rootNavigatorKey);

    await tester.pumpWidget(_app(api, router: router));
    await tester.pumpAndSettle();
    await tester.tap(find.text('全部已读'));
    await tester.pumpAndSettle();

    expect(find.text('网络连接不稳定，消息状态没有更新，请稍后重试'), findsOneWidget);
    expect(find.text('全部已读'), findsOneWidget);
  });
}

Widget _app(_InboxApi api, {required GoRouter router}) {
  return ProviderScope(
    overrides: [notificationApiProvider.overrideWithValue(api)],
    child: MaterialApp.router(routerConfig: router),
  );
}

GoRouter _router(_InboxApi api, GlobalKey<NavigatorState> rootNavigatorKey) {
  return GoRouter(
    navigatorKey: rootNavigatorKey,
    initialLocation: '/inbox',
    routes: [
      GoRoute(path: '/inbox', builder: (_, _) => const NotificationInboxPage()),
      GoRoute(
        path: '/details',
        parentNavigatorKey: rootNavigatorKey,
        builder: (_, _) => const Scaffold(body: Center(child: Text('详情'))),
      ),
      GoRoute(
        path: '/devices/device-1',
        parentNavigatorKey: rootNavigatorKey,
        builder: (_, _) => const Scaffold(body: Center(child: Text('设备详情'))),
      ),
    ],
  );
}

class _InboxApi implements NotificationApi {
  _InboxApi({this.item, this.readGate, this.readError, this.markAllError});

  final GuardianNotification? item;
  final Completer<void>? readGate;
  final Object? readError;
  final Object? markAllError;
  final List<String> markReadIds = <String>[];
  bool markReadStarted = false;

  GuardianNotification get _item =>
      item ??
      _notification(actionPath: '/devices/device-1', actionLabel: '查看设备');

  @override
  Future<GuardianNotificationPage> list({
    int limit = 20,
    String? cursor,
    String? category,
  }) async {
    return GuardianNotificationPage(
      items: [_item],
      unreadCount: 1,
      nextCursor: '',
      hasMore: false,
    );
  }

  @override
  Future<int> unreadCount() async => 1;

  @override
  Future<void> markRead(String notificationId) async {
    markReadStarted = true;
    await readGate?.future;
    final error = readError;
    if (error != null) {
      throw error;
    }
    markReadIds.add(notificationId);
  }

  @override
  Future<void> markAllRead() async {
    final error = markAllError;
    if (error != null) {
      throw error;
    }
  }

  @override
  Future<void> sendFamilyMessage({
    required String deviceId,
    required String body,
    required bool sendToDevice,
    int displayDurationSeconds = 30,
  }) async {}

  @override
  Future<List<DeviceMessage>> deviceMessages(
    String deviceId, {
    int limit = 20,
  }) async {
    return const <DeviceMessage>[];
  }
}

GuardianNotification _notification({
  String actionPath = '/details',
  String actionLabel = '查看详情',
}) {
  return GuardianNotification(
    id: 'notice-1',
    category: 'device_status',
    severity: 'warning',
    title: '设备离线',
    body: '请检查设备和网络。',
    actionPath: actionPath,
    actionLabel: actionLabel,
    deviceId: 'device-1',
    channels: const ['in_app'],
    isRead: false,
    publishAt: DateTime.utc(2026, 10, 9),
    expiresAt: null,
  );
}
