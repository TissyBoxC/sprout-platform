import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/usage_report/domain/usage_report.dart';

void main() {
  test('parses one usage report day and its breakdowns', () {
    final report = UsageReport.fromJson({
      'reports': [_reportJson()],
    });

    expect(report.days, hasLength(1));
    final day = report.days.single;
    expect(day.reportDate, DateTime(2026, 10, 7));
    expect(day.activeMinutes, 42);
    expect(day.conversationCount, 18);
    expect(day.contentPlayCount, 6);
    expect(day.dailyLimitMinutes, 60);
    expect(day.remainingMinutes, 18);
    expect(day.limitReached, isFalse);
    expect(day.usagePercent, 70);
    expect(day.categories.single.category, ChildContentCategory.story);
    expect(day.blocked.total, 3);
    expect(day.devices.single.deviceName, '初芽');
  });

  test('keeps the newest returned day first and exposes range totals', () {
    final newer = _reportJson(
      reportDate: '2026-10-08',
      activeMinutes: 10,
      conversationCount: 2,
      contentPlayCount: 1,
      blocked: {
        'disabled_period': 0,
        'daily_limit': 1,
        'category_denied': 0,
        'time_untrusted': 0,
      },
    );
    final report = UsageReport.fromJson({
      'reports': [newer, _reportJson()],
    });

    expect(report.today?.reportDate, DateTime(2026, 10, 8));
    expect(report.chronologicalDays.first.reportDate, DateTime(2026, 10, 7));
    expect(report.rangeActiveMinutes, 52);
    expect(report.rangeConversationCount, 20);
    expect(report.rangeContentPlayCount, 7);
    expect(report.rangeBlockedCount, 4);
  });

  test('treats zero daily limit as unlimited', () {
    final day = UsageReportDay.fromJson(
      _reportJson(dailyLimitMinutes: 0, remainingMinutes: 0),
    );

    expect(day.isUnlimited, isTrue);
    expect(day.usagePercent, 0);
  });
}

Map<String, Object?> _reportJson({
  String reportDate = '2026-10-07',
  int activeMinutes = 42,
  int conversationCount = 18,
  int contentPlayCount = 6,
  int dailyLimitMinutes = 60,
  int remainingMinutes = 18,
  Map<String, Object?>? blocked,
}) {
  return {
    'schema_version': '1.0.0',
    'report_date': reportDate,
    'timezone_offset_minutes': 480,
    'active_minutes': activeMinutes,
    'conversation_count': conversationCount,
    'conversation_minutes': 24,
    'content_play_count': contentPlayCount,
    'content_minutes': 18,
    'daily_limit_minutes': dailyLimitMinutes,
    'remaining_minutes': remainingMinutes,
    'limit_reached': false,
    'categories': [
      {'category': 'story', 'play_count': 3, 'minutes': 9},
    ],
    'blocked':
        blocked ??
        {
          'disabled_period': 1,
          'daily_limit': 0,
          'category_denied': 2,
          'time_untrusted': 0,
        },
    'devices': [
      {
        'device_id': 'device_demo_001',
        'device_name': '初芽',
        'active_minutes': activeMinutes,
        'conversation_count': conversationCount,
        'content_play_count': contentPlayCount,
      },
    ],
    'updated_at': '2026-10-07T12:00:00Z',
  };
}
