import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/notification/data/notification_api.dart';
import 'package:parent_app/features/notification/domain/notification.dart';

void main() {
  test('parses one inbox page with unread count and cursor', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'items': <Object?>[_notificationJson()],
          'unread_count': 3,
          'next_cursor': 'cursor-2',
          'has_more': true,
        },
      },
    );
    final api = NotificationApi(client);

    final page = await api.list(limit: 20, category: 'family_message');

    expect(page.items, hasLength(1));
    expect(page.unreadCount, 3);
    expect(page.nextCursor, 'cursor-2');
    expect(page.hasMore, isTrue);
    expect(page.items.single.hasAction, isTrue);
    expect(page.items.single.channels, <String>['in_app']);
    expect(client.lastGetPath, '/api/v1/notifications');
    expect(client.lastQueryParameters, <String, String>{
      'limit': '20',
      'category': 'family_message',
    });
  });

  test('omits an empty cursor from the query', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'items': <Object?>[],
          'unread_count': 0,
          'next_cursor': '',
          'has_more': false,
        },
      },
    );
    final api = NotificationApi(client);

    await api.list(cursor: '');

    expect(client.lastQueryParameters, <String, String>{'limit': '20'});
  });

  test('clamps the unread badge and tolerates a missing value', () async {
    final big = NotificationApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{'unread_count': 100000},
        },
      ),
    );
    expect(await big.unreadCount(), 9999);

    final missing = NotificationApi(
      _FakeApiClient(response: <String, Object?>{'data': <String, Object?>{}}),
    );
    expect(await missing.unreadCount(), 0);
  });

  test('marks one notification read on its encoded path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = NotificationApi(client);

    await api.markRead('notice/1');

    expect(client.lastPostPath, '/api/v1/notifications/notice%2F1/read');
  });

  test('marks all notifications read', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = NotificationApi(client);

    await api.markAllRead();

    expect(client.lastPostPath, '/api/v1/notifications/read-all');
  });

  test('sends a family message with the device and display options', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = NotificationApi(client);

    await api.sendFamilyMessage(
      deviceId: 'device-1',
      body: '早点休息',
      sendToDevice: true,
      displayDurationSeconds: 45,
    );

    expect(client.lastPostPath, '/api/v1/notifications/family-messages');
    expect(client.lastPostBody, <String, Object?>{
      'device_id': 'device-1',
      'body': '早点休息',
      'send_to_device': true,
      'display_duration_seconds': 45,
    });
  });

  test('falls back to a system label for an unknown category', () {
    expect(notificationCategoryLabel('brand_new_kind'), '系统通知');
    expect(notificationCategoryLabel('family_message'), '家人留言');
  });

  test('defaults missing optional copy without throwing', () {
    final notification = GuardianNotification.fromJson(<String, Object?>{
      'id': 'notice-1',
    });

    expect(notification.category, 'system_announcement');
    expect(notification.severity, 'info');
    expect(notification.title, '');
    expect(notification.isRead, isFalse);
    expect(notification.publishAt, isNull);
    expect(notification.hasAction, isFalse);
  });
}

Map<String, Object?> _notificationJson() {
  return <String, Object?>{
    'id': 'notice-1',
    'category': 'family_message',
    'severity': 'info',
    'title': '家人留言',
    'body': '早点休息',
    'action_path': '/me/notifications',
    'action_label': '查看',
    'device_id': 'device-1',
    'channels': <Object?>['in_app'],
    'read': false,
    'publish_at': '2026-10-09T00:00:00Z',
    'expires_at': '2026-10-20T00:00:00Z',
  };
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  String? lastGetPath;
  Map<String, String>? lastQueryParameters;
  String? lastPostPath;
  Object? lastPostBody;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastGetPath = path;
    lastQueryParameters = queryParameters;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    lastPostPath = path;
    lastPostBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    return response;
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
