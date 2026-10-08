import '../../child_profile/domain/child_profile.dart';

/// One device-local day of usage counters shown to a guardian.
class UsageReportDay {
  const UsageReportDay({
    required this.schemaVersion,
    required this.reportDate,
    required this.timezoneOffsetMinutes,
    required this.activeMinutes,
    required this.conversationCount,
    required this.conversationMinutes,
    required this.contentPlayCount,
    required this.contentMinutes,
    required this.dailyLimitMinutes,
    required this.remainingMinutes,
    required this.limitReached,
    required this.categories,
    required this.blocked,
    required this.devices,
    required this.updatedAt,
  });

  final String schemaVersion;
  final DateTime reportDate;
  final int timezoneOffsetMinutes;
  final int activeMinutes;
  final int conversationCount;
  final int conversationMinutes;
  final int contentPlayCount;
  final int contentMinutes;
  final int dailyLimitMinutes;
  final int remainingMinutes;
  final bool limitReached;
  final List<UsageReportCategory> categories;
  final UsageReportBlocked blocked;
  final List<UsageReportDevice> devices;
  final DateTime updatedAt;

  bool get hasUsage =>
      activeMinutes > 0 ||
      conversationCount > 0 ||
      contentPlayCount > 0 ||
      categories.isNotEmpty ||
      devices.isNotEmpty;

  bool get isUnlimited => dailyLimitMinutes == 0;

  int get usagePercent {
    if (dailyLimitMinutes <= 0) {
      return 0;
    }
    return ((activeMinutes * 100) / dailyLimitMinutes).round().clamp(0, 100);
  }

  factory UsageReportDay.fromJson(Map<String, Object?> json) {
    return UsageReportDay(
      schemaVersion: _requiredString(json['schema_version'], 'schema_version'),
      reportDate: _requiredDate(json['report_date'], 'report_date'),
      timezoneOffsetMinutes: _requiredInt(
        json['timezone_offset_minutes'],
        'timezone_offset_minutes',
      ),
      activeMinutes: _requiredInt(json['active_minutes'], 'active_minutes'),
      conversationCount: _requiredInt(
        json['conversation_count'],
        'conversation_count',
      ),
      conversationMinutes: _requiredInt(
        json['conversation_minutes'],
        'conversation_minutes',
      ),
      contentPlayCount: _requiredInt(
        json['content_play_count'],
        'content_play_count',
      ),
      contentMinutes: _requiredInt(json['content_minutes'], 'content_minutes'),
      dailyLimitMinutes: _requiredInt(
        json['daily_limit_minutes'],
        'daily_limit_minutes',
      ),
      remainingMinutes: _requiredInt(
        json['remaining_minutes'],
        'remaining_minutes',
      ),
      limitReached: json['limit_reached'] == true,
      categories: _mapList(
        json['categories'],
      ).map(UsageReportCategory.fromJson).toList(growable: false),
      blocked: UsageReportBlocked.fromJson(
        _requiredMap(json['blocked'], 'blocked'),
      ),
      devices: _mapList(
        json['devices'],
      ).map(UsageReportDevice.fromJson).toList(growable: false),
      updatedAt: _requiredDateTime(json['updated_at'], 'updated_at'),
    );
  }
}

/// Usage for one approved content category on a report day.
class UsageReportCategory {
  const UsageReportCategory({
    required this.category,
    required this.playCount,
    required this.minutes,
  });

  final ChildContentCategory category;
  final int playCount;
  final int minutes;

  factory UsageReportCategory.fromJson(Map<String, Object?> json) {
    return UsageReportCategory(
      category: ChildContentCategoryMetadata.fromWireValue(
        _requiredString(json['category'], 'category'),
      ),
      playCount: _requiredInt(json['play_count'], 'play_count'),
      minutes: _requiredInt(json['minutes'], 'minutes'),
    );
  }
}

/// Counts of safety limits that prevented device use on a report day.
class UsageReportBlocked {
  const UsageReportBlocked({
    required this.disabledPeriod,
    required this.dailyLimit,
    required this.categoryDenied,
    required this.timeUntrusted,
  });

  final int disabledPeriod;
  final int dailyLimit;
  final int categoryDenied;
  final int timeUntrusted;

  int get total => disabledPeriod + dailyLimit + categoryDenied + timeUntrusted;

  factory UsageReportBlocked.fromJson(Map<String, Object?> json) {
    return UsageReportBlocked(
      disabledPeriod: _requiredInt(json['disabled_period'], 'disabled_period'),
      dailyLimit: _requiredInt(json['daily_limit'], 'daily_limit'),
      categoryDenied: _requiredInt(json['category_denied'], 'category_denied'),
      timeUntrusted: _requiredInt(json['time_untrusted'], 'time_untrusted'),
    );
  }
}

/// Usage contributed by one family-bound device.
class UsageReportDevice {
  const UsageReportDevice({
    required this.deviceId,
    required this.deviceName,
    required this.activeMinutes,
    required this.conversationCount,
    required this.contentPlayCount,
  });

  final String deviceId;
  final String deviceName;
  final int activeMinutes;
  final int conversationCount;
  final int contentPlayCount;

  factory UsageReportDevice.fromJson(Map<String, Object?> json) {
    return UsageReportDevice(
      deviceId: _requiredString(json['device_id'], 'device_id'),
      deviceName: _requiredString(json['device_name'], 'device_name'),
      activeMinutes: _requiredInt(json['active_minutes'], 'active_minutes'),
      conversationCount: _requiredInt(
        json['conversation_count'],
        'conversation_count',
      ),
      contentPlayCount: _requiredInt(
        json['content_play_count'],
        'content_play_count',
      ),
    );
  }
}

/// One bounded usage-report snapshot with the newest day first.
class UsageReport {
  const UsageReport({required this.days});

  final List<UsageReportDay> days;

  bool get isEmpty => days.isEmpty;

  UsageReportDay? get today => days.isEmpty ? null : days.first;

  UsageReportDay? get latest => days.isEmpty ? null : days.first;

  /// Days in calendar order for a left-to-right trend chart.
  List<UsageReportDay> get chronologicalDays =>
      days.reversed.toList(growable: false);

  int get rangeActiveMinutes =>
      days.fold(0, (total, day) => total + day.activeMinutes);

  int get rangeConversationCount =>
      days.fold(0, (total, day) => total + day.conversationCount);

  int get rangeContentPlayCount =>
      days.fold(0, (total, day) => total + day.contentPlayCount);

  int get rangeBlockedCount =>
      days.fold(0, (total, day) => total + day.blocked.total);

  factory UsageReport.fromJson(Map<String, Object?> json) {
    return UsageReport(
      days: _mapList(
        json['reports'],
      ).map(UsageReportDay.fromJson).toList(growable: false),
    );
  }
}

String usageReportDateLabel(DateTime date) {
  return '${date.month} 月 ${date.day} 日';
}

String _requiredString(Object? value, String field) {
  if (value is String && value.trim().isNotEmpty) {
    return value.trim();
  }
  throw FormatException('missing $field');
}

int _requiredInt(Object? value, String field) {
  if (value is num) {
    return value.toInt();
  }
  throw FormatException('missing $field');
}

DateTime _requiredDate(Object? value, String field) {
  if (value is String) {
    final datePart = value.contains('T') ? value.split('T').first : value;
    final parts = datePart.split('-');
    if (parts.length == 3) {
      final year = int.tryParse(parts[0]);
      final month = int.tryParse(parts[1]);
      final day = int.tryParse(parts[2]);
      if (year != null && month != null && day != null) {
        return DateTime(year, month, day);
      }
    }
  }
  throw FormatException('missing $field');
}

DateTime _requiredDateTime(Object? value, String field) {
  if (value is String) {
    final parsed = DateTime.tryParse(value);
    if (parsed != null) {
      return parsed;
    }
  }
  throw FormatException('missing $field');
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
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
