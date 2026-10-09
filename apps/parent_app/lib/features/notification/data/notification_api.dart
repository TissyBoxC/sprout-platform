import '../../../core/network/api_client.dart';
import '../domain/notification.dart';

/// Guardian-authenticated HTTP boundary for the notification inbox and the
/// family-message composer.
class NotificationApi {
  const NotificationApi(this._apiClient);

  final ApiClient _apiClient;

  Future<GuardianNotificationPage> list({
    int limit = 20,
    String? cursor,
    String? category,
  }) async {
    final response = await _apiClient.get(
      '/api/v1/notifications',
      queryParameters: {
        'limit': limit.toString(),
        if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
        if (category != null && category.isNotEmpty) 'category': category,
      },
    );
    return GuardianNotificationPage.fromJson(_requiredMap(response['data'], 'data'));
  }

  Future<int> unreadCount() async {
    final response = await _apiClient.get('/api/v1/notifications/unread-count');
    final data = _requiredMap(response['data'], 'data');
    final value = data['unread_count'];
    if (value is num) {
      return value.toInt().clamp(0, 9999);
    }
    return 0;
  }

  Future<void> markRead(String notificationId) async {
    await _apiClient.post(
      '/api/v1/notifications/${Uri.encodeComponent(notificationId)}/read',
    );
  }

  Future<void> markAllRead() async {
    await _apiClient.post('/api/v1/notifications/read-all');
  }

  Future<void> sendFamilyMessage({
    required String deviceId,
    required String body,
    required bool sendToDevice,
    int displayDurationSeconds = 30,
  }) async {
    await _apiClient.post(
      '/api/v1/notifications/family-messages',
      body: {
        'device_id': deviceId,
        'body': body,
        'send_to_device': sendToDevice,
        'display_duration_seconds': displayDurationSeconds,
      },
    );
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}
