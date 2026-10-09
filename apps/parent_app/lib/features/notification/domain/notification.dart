/// One guardian-facing notification shown in the in-app inbox.
///
/// Notification copy never carries another family's data, child conversation
/// text, media, tokens, or raw errors.
class GuardianNotification {
  const GuardianNotification({
    required this.id,
    required this.category,
    required this.severity,
    required this.title,
    required this.body,
    required this.actionPath,
    required this.actionLabel,
    required this.deviceId,
    required this.channels,
    required this.isRead,
    required this.publishAt,
    required this.expiresAt,
  });

  final String id;
  final String category;
  final String severity;
  final String title;
  final String body;
  final String actionPath;
  final String actionLabel;
  final String deviceId;
  final List<String> channels;
  final bool isRead;
  final DateTime? publishAt;
  final DateTime? expiresAt;

  bool get hasAction => actionPath.trim().isNotEmpty;

  factory GuardianNotification.fromJson(Map<String, Object?> json) {
    return GuardianNotification(
      id: _requiredString(json['id'], 'id'),
      category: _stringOr(json['category'], 'system_announcement'),
      severity: _stringOr(json['severity'], 'info'),
      title: _stringOr(json['title'], ''),
      body: _stringOr(json['body'], ''),
      actionPath: _stringOr(json['action_path'], ''),
      actionLabel: _stringOr(json['action_label'], ''),
      deviceId: _stringOr(json['device_id'], ''),
      channels: _stringList(json['channels']),
      isRead: json['read'] == true,
      publishAt: _optionalDateTime(json['publish_at']),
      expiresAt: _optionalDateTime(json['expires_at']),
    );
  }
}

/// One bounded page of the guardian inbox plus its unread badge.
class GuardianNotificationPage {
  const GuardianNotificationPage({
    required this.items,
    required this.unreadCount,
    required this.nextCursor,
    required this.hasMore,
  });

  final List<GuardianNotification> items;
  final int unreadCount;
  final String nextCursor;
  final bool hasMore;

  factory GuardianNotificationPage.fromJson(Map<String, Object?> json) {
    return GuardianNotificationPage(
      items: _mapList(json['items'])
          .map(GuardianNotification.fromJson)
          .toList(growable: false),
      unreadCount: _intOr(json['unread_count'], 0),
      nextCursor: _stringOr(json['next_cursor'], ''),
      hasMore: json['has_more'] == true,
    );
  }
}

String notificationCategoryLabel(String category) {
  return const {
        'account_security': '账号安全',
        'device_status': '设备状态',
        'content_release': '内容更新',
        'service_update': '服务更新',
        'usage_report': '使用记录',
        'family_message': '家人留言',
        'system_announcement': '系统通知',
      }[category] ??
      '系统通知';
}

String _requiredString(Object? value, String field) {
  if (value is String && value.trim().isNotEmpty) {
    return value.trim();
  }
  throw FormatException('missing $field');
}

String _stringOr(Object? value, String fallback) {
  if (value is String) {
    final trimmed = value.trim();
    return trimmed.isEmpty ? fallback : trimmed;
  }
  return fallback;
}

int _intOr(Object? value, int fallback) {
  if (value is num) {
    return value.toInt();
  }
  return fallback;
}

DateTime? _optionalDateTime(Object? value) {
  if (value is String && value.trim().isNotEmpty) {
    return DateTime.tryParse(value.trim());
  }
  return null;
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value
      .whereType<String>()
      .map((item) => item.trim())
      .where((item) => item.isNotEmpty)
      .toList(growable: false);
}

List<Map<String, Object?>> _mapList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value
      .whereType<Map>()
      .map((item) => Map<String, Object?>.from(item))
      .toList(growable: false);
}
