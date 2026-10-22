import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/error/api_error_mapper.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/emotion/application/emotion_controller.dart';
import 'package:parent_app/features/emotion/data/emotion_api.dart';
import 'package:parent_app/features/emotion/domain/emotion.dart';
import 'package:parent_app/features/emotion/presentation/emotion_copy.dart';
import 'package:parent_app/features/emotion/presentation/emotion_page.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';

void main() {
  group('默认关闭', () {
    testWidgets('explains what is recorded before anything can be turned on', (
      tester,
    ) async {
      final api = _FakeEmotionApi(isEnabled: false);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.introTitle), findsOneWidget);
      expect(find.text(EmotionCopy.privacyTagOnly), findsOneWidget);
      expect(find.text(EmotionCopy.privacyNoMedia), findsOneWidget);
      expect(find.text(EmotionCopy.privacyGuardianOnly), findsOneWidget);
      expect(find.text(EmotionCopy.privacyNoMoments), findsOneWidget);
      expect(find.text(EmotionCopy.privacyRetention), findsOneWidget);
      expect(find.text(EmotionCopy.privacySwitchOff), findsOneWidget);
      // Off by default: no distribution is read and no switch is offered.
      expect(find.text(EmotionCopy.enableButton), findsOneWidget);
      expect(api.summaryCalls, isEmpty);
      expect(
        find.byKey(const ValueKey<String>('emotion-switch')),
        findsNothing,
      );
    });

    testWidgets('states the promises without pressure or marketing words', (
      tester,
    ) async {
      await tester.pumpWidget(_page(api: _FakeEmotionApi(isEnabled: false)));
      await tester.pumpAndSettle();

      final visible = _visibleText(tester).join('\n');
      for (final banned in <String>['一键', '专属', '限时', '立刻', '千万别', '隐患']) {
        expect(visible.contains(banned), isFalse, reason: banned);
      }
    });
  });

  group('开启', () {
    testWidgets('turning it on reads the distribution for the same window', (
      tester,
    ) async {
      final api = _FakeEmotionApi(isEnabled: false);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('emotion-enable')));
      await tester.pumpAndSettle();

      expect(api.enabledSaves, <bool>[true]);
      expect(api.summaryCalls, <String>['|7']);
      expect(find.text(EmotionCopy.summaryTitle), findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('emotion-switch')),
        findsOneWidget,
      );
    });

    testWidgets('a failed enable keeps the explanation and says so', (
      tester,
    ) async {
      final api = _FakeEmotionApi(isEnabled: false, failEnable: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('emotion-enable')));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey<String>('emotion-enable')),
        findsOneWidget,
      );
      expect(find.text(mapApiError(_offline()).message), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });
  });

  group('有数据', () {
    testWidgets('lists every recorded label with its count and share', (
      tester,
    ) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      _expand(tester);
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.summaryTitle), findsOneWidget);
      expect(
        find.text(
          '${EmotionCopy.sampleTotal(30)} · ${EmotionCopy.childFamilyWide}',
        ),
        findsOneWidget,
      );
      expect(
        find.text(EmotionCopy.labelDetail(_share(EmotionLabel.happy, 12, 40))),
        findsOneWidget,
      );
      expect(
        find.text(EmotionCopy.labelDetail(_share(EmotionLabel.calm, 10, 33))),
        findsOneWidget,
      );
      // A label with no samples is not listed at all.
      expect(find.textContaining('不安'), findsNothing);
      expect(
        find.text(EmotionCopy.dominantPhrase(7, EmotionLabel.happy)),
        findsOneWidget,
      );
    });

    testWidgets('never shows a single moment of the day', (tester) async {
      await tester.pumpWidget(_page(api: _FakeEmotionApi()));
      await tester.pumpAndSettle();
      _expand(tester);
      await tester.pumpAndSettle();

      final visible = _visibleText(tester).join('\n');
      // Neither a clock time nor a date belongs on this screen; the platform's
      // contract has no field that could produce one.
      for (final pattern in <RegExp>[
        RegExp(r'\d{1,2}:\d{2}'),
        RegExp(r'\d{1,2} 月 \d{1,2} 日'),
      ]) {
        expect(pattern.hasMatch(visible), isFalse, reason: pattern.pattern);
      }
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('trends are words, not a time series', (tester) async {
      await tester.pumpWidget(_page(api: _FakeEmotionApi()));
      await tester.pumpAndSettle();
      _expand(tester);
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.trendTitle), findsOneWidget);
      expect(find.textContaining('${EmotionCopy.trendUp}，'), findsOneWidget);
      expect(find.textContaining(EmotionCopy.trendFlat), findsOneWidget);
      // Shares are bars with a value, never a line chart.
      expect(find.byType(LinearProgressIndicator), findsNWidgets(3));
    });

    testWidgets('too few samples says so instead of inventing a trend', (
      tester,
    ) async {
      final api = _FakeEmotionApi(trendUnknown: true, samples: 6);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      _expand(tester);
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.trendNoComparison), findsOneWidget);
      expect(find.textContaining('比上期'), findsNothing);
    });
  });

  group('空状态', () {
    testWidgets('an empty window explains when the distribution appears', (
      tester,
    ) async {
      final api = _FakeEmotionApi(samples: 0);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      _expand(tester);
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.emptyTitle), findsOneWidget);
      expect(find.text(EmotionCopy.emptyBody), findsOneWidget);
      expect(find.text(EmotionCopy.summaryTitle), findsNothing);
    });
  });

  group('加载与错误', () {
    testWidgets('shows a loading state while the switch is read', (
      tester,
    ) async {
      final api = _FakeEmotionApi(isEnabled: false, pending: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pump();

      expect(find.text(EmotionCopy.loading), findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('emotion-enable')),
        findsNothing,
      );

      api.release();
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey<String>('emotion-enable')),
        findsOneWidget,
      );
    });

    testWidgets('a lapsed sign-in asks the guardian to sign in again', (
      tester,
    ) async {
      final api = _FakeEmotionApi(
        isEnabled: false,
        settingsFailure: _status(401),
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text('登录已过期，请重新登录'), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('an unreachable network offers a retry', (tester) async {
      final api = _FakeEmotionApi(
        isEnabled: false,
        settingsFailure: _offline(),
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
      expect(find.text(EmotionCopy.retryButton), findsOneWidget);

      api.settingsFailure = null;
      await tester.tap(find.text(EmotionCopy.retryButton));
      await tester.pumpAndSettle();
      expect(find.text(EmotionCopy.retryButton), findsNothing);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('a summary that never arrives is reported without raw text', (
      tester,
    ) async {
      final api = _FakeEmotionApi(summaryFailure: _offline());
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });
  });

  group('切换天数', () {
    testWidgets('choosing 30 days reads that window and shows it', (
      tester,
    ) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey<String>('emotion-window-30')));
      await tester.pumpAndSettle();

      expect(api.summaryCalls, <String>['|7', '|30']);
      expect(find.text(EmotionCopy.windowPhrase(30)), findsWidgets);
      // The card itself now covers the window that was asked for.
      _expand(tester);
      await tester.pumpAndSettle();
      expect(
        find.descendant(
          of: find.byKey(const ValueKey<String>('emotion-summary-shown')),
          matching: find.text(EmotionCopy.windowPhrase(30)),
        ),
        findsOneWidget,
      );
    });
  });

  group('切换孩子', () {
    testWidgets('choosing a child reads that child and names them', (
      tester,
    ) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api, children: _twoChildren));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      // The family-wide view is the default, which the platform accepts, and
      // the card names it so the numbers are not taken for one child's.
      expect(
        find.text(
          '${EmotionCopy.sampleTotal(30)} · ${EmotionCopy.childFamilyWide}',
        ),
        findsOneWidget,
      );

      await tester.tap(
        find.byKey(const ValueKey<String>('emotion-child-child-2')),
      );
      await tester.pumpAndSettle();

      expect(api.summaryCalls, <String>['|7', 'child-2|7']);
      expect(find.text('${EmotionCopy.sampleTotal(30)} · 豆豆'), findsOneWidget);
    });

    testWidgets('a family without children explains what to do first', (
      tester,
    ) async {
      await tester.pumpWidget(
        _page(api: _FakeEmotionApi(), children: const <ChildProfile>[]),
      );
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.childEmptyTitle), findsOneWidget);
      expect(find.text(EmotionCopy.childEmptyBody), findsOneWidget);
    });
  });

  group('保留期与关闭', () {
    testWidgets('a retention choice saves the same on state', (tester) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey<String>('emotion-retention-7')),
      );
      await tester.pumpAndSettle();

      expect(api.retentionSaves, <int?>[7]);
      // The summary is not re-read: only the retention changed.
      expect(api.summaryCalls, <String>['|7']);
      expect(find.text(EmotionCopy.retentionDays(7)), findsOneWidget);
    });

    testWidgets('never sends an empty retention, which the platform refuses', (
      tester,
    ) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      // 30 is already stored, so the chip that changes something is 7.
      await tester.tap(
        find.byKey(const ValueKey<String>('emotion-retention-7')),
      );
      await tester.pumpAndSettle();

      expect(api.retentionSaves, <int?>[7]);
      expect(api.retentionSaves.contains(0), isFalse);
      expect(api.retentionSaves.contains(null), isFalse);
    });

    testWidgets('closing asks first and cancelling keeps it on', (
      tester,
    ) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('emotion-switch')));
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.disableConfirmTitle), findsOneWidget);
      expect(find.text(EmotionCopy.disableConfirmBody), findsOneWidget);

      await tester.tap(
        find.byKey(const ValueKey<String>('emotion-disable-cancel')),
      );
      await tester.pumpAndSettle();

      expect(api.enabledSaves, isEmpty);
      expect(
        find.byKey(const ValueKey<String>('emotion-switch')),
        findsOneWidget,
      );
    });

    testWidgets('confirming stops recording and says what happened', (
      tester,
    ) async {
      final api = _FakeEmotionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      _expand(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('emotion-switch')));
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey<String>('emotion-disable-confirm')),
      );
      await tester.pumpAndSettle();

      expect(api.enabledSaves, <bool>[false]);
      expect(find.text(EmotionCopy.disabledNotice), findsOneWidget);
      expect(find.text(EmotionCopy.enableButton), findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('emotion-switch')),
        findsNothing,
      );
    });

    testWidgets('a family turned off elsewhere lands on the switch state', (
      tester,
    ) async {
      // The summary answers with the platform's own "not enabled" refusal,
      // which is the switch's state rather than a failed list.
      final api = _FakeEmotionApi(disabledRefusal: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text(EmotionCopy.enableButton), findsOneWidget);
      expect(find.text(EmotionCopy.retryButton), findsNothing);
    });
  });
}

/// The widget tree every case starts from: the real page, the real notifier,
/// and a fake only at the data boundary.
Widget _page({required _FakeEmotionApi api, List<ChildProfile>? children}) {
  final profiles = children ?? _twoChildren;
  final router = GoRouter(
    initialLocation: '/me/emotion',
    routes: <RouteBase>[
      GoRoute(
        path: '/me',
        builder: (context, state) => const Scaffold(body: Text('我的占位')),
      ),
      GoRoute(
        path: '/me/emotion',
        builder: (context, state) => const EmotionPage(),
      ),
    ],
  );
  return ProviderScope(
    overrides: [
      // Every provider the page reaches is replaced: a future provider left
      // alone would never settle and no screen would ever appear.
      emotionFamilyIdProvider.overrideWith((ref) async => 'family-1'),
      emotionApiProvider.overrideWithValue(api),
      emotionChildrenProvider.overrideWith((ref) async => profiles),
      familyControllerProvider.overrideWith(_SeededFamilyController.new),
      childProfileControllerProvider.overrideWith(_SeededChildController.new),
    ],
    child: MaterialApp.router(theme: AppTheme.light, routerConfig: router),
  );
}

final _twoChildren = <ChildProfile>[
  ChildProfile(
    childId: 'child-1',
    familyId: 'family-1',
    nickname: '朵朵',
    ageTier: ChildAgeTier.age5To6,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: _fixedNow,
    updatedAt: _fixedNow,
  ),
  ChildProfile(
    childId: 'child-2',
    familyId: 'family-1',
    nickname: '豆豆',
    ageTier: ChildAgeTier.age3To4,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: _fixedNow,
    updatedAt: _fixedNow,
  ),
];

final _fixedNow = DateTime.utc(2026, 10, 20);

EmotionLabelShare _share(EmotionLabel label, int count, int percentage) {
  return EmotionLabelShare(
    label: label,
    count: count,
    percentage: percentage,
    trend: EmotionTrend.flat,
  );
}

/// A distribution of 30 samples across three labels.
EmotionSummary _summary({
  int days = 7,
  bool trendUnknown = false,
  int samples = 30,
}) {
  final unknown = trendUnknown ? EmotionTrend.unknown : null;
  final now = DateTime.utc(2026, 10, 22, 12);
  return EmotionSummary(
    days: days,
    periodStart: now.subtract(Duration(days: days)),
    periodEnd: now,
    totalSamples: samples,
    previousTotalSamples: samples,
    averageConfidence: 78,
    isEnabled: true,
    retentionDays: 30,
    shares: <EmotionLabelShare>[
      if (samples > 0) ...<EmotionLabelShare>[
        EmotionLabelShare(
          label: EmotionLabel.calm,
          count: 10,
          percentage: 33,
          trend: unknown ?? EmotionTrend.flat,
        ),
        EmotionLabelShare(
          label: EmotionLabel.happy,
          count: 12,
          percentage: 40,
          trend: unknown ?? EmotionTrend.up,
        ),
        EmotionLabelShare(
          label: EmotionLabel.tired,
          count: 8,
          percentage: 27,
          trend: unknown ?? EmotionTrend.down,
        ),
      ],
      EmotionLabelShare(
        label: EmotionLabel.upset,
        count: 0,
        percentage: 0,
        trend: unknown ?? EmotionTrend.unknown,
      ),
    ],
  );
}

class _FakeEmotionApi implements EmotionApi {
  _FakeEmotionApi({
    this.isEnabled = true,
    this.samples = 30,
    this.trendUnknown = false,
    this.pending = false,
    this.failEnable = false,
    this.disabledRefusal = false,
    this.settingsFailure,
    this.summaryFailure,
  });

  bool isEnabled;
  final int samples;
  final bool trendUnknown;
  final bool pending;
  final bool failEnable;
  final bool disabledRefusal;
  Object? settingsFailure;
  Object? summaryFailure;

  final List<bool> enabledSaves = <bool>[];
  final List<int?> retentionSaves = <int?>[];
  final List<String> summaryCalls = <String>[];

  int retentionDays = 30;
  Completer<void>? _gate;

  /// Holds the first read open until the test releases it.
  void release() {
    _gate?.complete();
    _gate = null;
  }

  Future<void> _awaitGate() async {
    if (!pending) {
      return;
    }
    _gate ??= Completer<void>();
    await _gate!.future;
  }

  @override
  Future<EmotionSettings> fetchSettings() async {
    await _awaitGate();
    final failure = settingsFailure;
    if (failure != null) {
      throw mapApiError(failure);
    }
    return EmotionSettings(
      isEnabled: isEnabled,
      retentionDays: retentionDays,
      summaryDefaultDays: 7,
      maxRetentionDays: 90,
    );
  }

  @override
  Future<EmotionSettings> updateSettings({
    required bool isEnabled,
    int? retentionDays,
  }) async {
    if (!isEnabled && failEnable) {
      throw mapApiError(_offline());
    }
    if (isEnabled && failEnable) {
      throw mapApiError(_offline());
    }
    if (retentionDays != null) {
      if (retentionDays <= 0) {
        // The platform refuses zero with a 422, so the fake must not accept it.
        throw const EmotionRefused(EmotionRefusal.invalidRetention);
      }
      retentionSaves.add(retentionDays);
      this.retentionDays = retentionDays;
    } else {
      enabledSaves.add(isEnabled);
    }
    this.isEnabled = isEnabled;
    return EmotionSettings(
      isEnabled: isEnabled,
      retentionDays: this.retentionDays,
      summaryDefaultDays: 7,
      maxRetentionDays: 90,
    );
  }

  @override
  Future<EmotionSummary> fetchSummary({
    String childId = '',
    int days = 7,
  }) async {
    summaryCalls.add('$childId|$days');
    final failure = summaryFailure;
    if (failure != null) {
      throw mapApiError(failure);
    }
    if (disabledRefusal) {
      throw const EmotionRefused(EmotionRefusal.disabled);
    }
    return _summary(days: days, trendUnknown: trendUnknown, samples: samples);
  }
}

class _SeededFamilyController extends FamilyController {
  @override
  Future<FamilyMembershipState> build() async {
    return const FamilyMembershipState(
      capability: FamilyCapability(
        familyId: 'family-1',
        ownerUserId: 'user-1',
        memberCount: 1,
        pendingInviteCount: 0,
        schemaVersion: '1',
      ),
      members: <FamilyMember>[],
      invitations: <FamilyInvitation>[],
    );
  }
}

class _SeededChildController extends ChildProfileController {
  @override
  Future<List<ChildProfile>> build() async => _twoChildren;
}

/// A connection that never reaches the service, as the transport reports it.
DioException _offline() {
  return DioException(
    requestOptions: RequestOptions(path: '/api/v1/families/family-1'),
    type: DioExceptionType.connectionError,
    message: 'SocketException: connection refused (127.0.0.1:8081)',
  );
}

/// A response with a status and a code, exactly as the transport reports it.
DioException _status(int status, {String code = 'unauthenticated'}) {
  final options = RequestOptions(path: '/api/v1/families/family-1');
  return DioException(
    requestOptions: options,
    type: DioExceptionType.badResponse,
    response: Response<Object?>(
      requestOptions: options,
      statusCode: status,
      data: <String, Object?>{
        'error': <String, Object?>{
          'code': code,
          'message': 'raw backend message',
          'retryable': false,
        },
      },
    ),
  );
}

/// Gives the page a tall viewport before a case reaches below the fold.
///
/// The entry card is deliberately long, because its whole point is the
/// explanation, so on a phone its own switch is only reached after a short
/// scroll. Growing the window keeps every case reading the real widget tree
/// while removing scroll noise from cases that are about something else. The
/// view is reset by replacing the test viewport after each case, so no widget
/// is rebuilt through a width change.
void _expand(WidgetTester tester) {
  tester.view.physicalSize = const Size(1200, 3600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

List<String> _visibleText(WidgetTester tester) {
  return tester
      .widgetList<Text>(find.byType(Text))
      .map((text) => text.data ?? text.textSpan?.toPlainText() ?? '')
      .toList(growable: false);
}

String? _findLeakedTerms(WidgetTester tester) {
  const banned = <String>[
    'API',
    'token',
    'Token',
    'session',
    'Session',
    'status',
    'emotion_',
    '127.0.0.1',
    '[object Object]',
    'undefined',
    'null',
    'NaN',
    'Exception',
    '检查网络配置',
  ];
  for (final text in _visibleText(tester)) {
    for (final word in banned) {
      if (text.contains(word)) {
        return word;
      }
    }
  }
  return null;
}
