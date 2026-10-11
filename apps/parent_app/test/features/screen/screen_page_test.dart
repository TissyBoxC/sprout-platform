import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/screen/application/screen_controller.dart';
import 'package:parent_app/features/screen/data/screen_api.dart';
import 'package:parent_app/features/screen/domain/screen.dart';
import 'package:parent_app/features/screen/presentation/screen_page.dart';

void main() {
  testWidgets('shows the screen policy, catalog and point-read dictionary', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [screenApiProvider.overrideWithValue(_FakeScreenApi())],
        child: const MaterialApp(home: ScreenPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('屏幕'), findsOneWidget);
    expect(find.text('默认表情'), findsOneWidget);
    expect(find.text('表情动画'), findsOneWidget);
    expect(find.text('点读'), findsOneWidget);
    expect(find.text('视频播放'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('点读词条'), 200);
    expect(find.text('点读词条'), findsOneWidget);
    expect(find.text('apple'), findsOneWidget);
    expect(find.text('苹果'), findsOneWidget);
  });

  testWidgets('toggles animation and saves the policy', (tester) async {
    final api = _FakeScreenApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [screenApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: ScreenPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('表情动画'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('保存屏幕设置'), 200);
    await tester.tap(find.text('保存屏幕设置'));
    await tester.pumpAndSettle();

    expect(api.savedPolicy?.animationEnabled, isFalse);
  });

  testWidgets('adds a point-read entry through the controller', (tester) async {
    final api = _FakeScreenApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [screenApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: ScreenPage()),
      ),
    );
    await tester.pumpAndSettle();

    final addButton = find.byTooltip('添加词条');
    await tester.scrollUntilVisible(addButton, 200);
    await tester.ensureVisible(addButton);
    await tester.pumpAndSettle();
    await tester.tap(addButton);
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, 'banana');
    await tester.enterText(find.byType(TextField).last, '香蕉');
    await tester.tap(find.widgetWithText(FilledButton, '保存'));
    await tester.pumpAndSettle();

    expect(api.putTerm, 'banana');
    expect(find.text('香蕉'), findsOneWidget);
  });
}

class _FakeScreenApi implements ScreenApi {
  bool animationEnabled = true;
  ScreenPolicy? savedPolicy;
  String? putTerm;
  final List<PointReadEntry> _entries = <PointReadEntry>[
    const PointReadEntry(id: 'entry-1', term: 'apple', text: '苹果'),
  ];

  @override
  Future<(ScreenPolicy, ScreenCatalog)> fetchPolicy() async {
    return (
      ScreenPolicy(
        defaultExpression: 'happy',
        animationEnabled: animationEnabled,
        pointReadEnabled: true,
        videoEnabled: true,
        videoMaxFps: 24,
        videoMaxBitrateKbps: 2000,
        animationFrameMs: 200,
        revision: 1,
      ),
      const ScreenCatalog(
        expressions: <String>[
          'neutral',
          'happy',
          'sad',
          'curious',
          'sleepy',
          'surprised',
          'excited',
        ],
        animations: <String>['idle', 'blink', 'wave', 'nod', 'dance'],
      ),
    );
  }

  @override
  Future<ScreenPolicy> updatePolicy(ScreenPolicy policy) async {
    savedPolicy = policy;
    animationEnabled = policy.animationEnabled;
    return ScreenPolicy(
      defaultExpression: policy.defaultExpression,
      animationEnabled: policy.animationEnabled,
      pointReadEnabled: policy.pointReadEnabled,
      videoEnabled: policy.videoEnabled,
      videoMaxFps: policy.videoMaxFps,
      videoMaxBitrateKbps: policy.videoMaxBitrateKbps,
      animationFrameMs: policy.animationFrameMs,
      revision: policy.revision + 1,
    );
  }

  @override
  Future<List<PointReadEntry>> fetchDictionary() async =>
      List<PointReadEntry>.unmodifiable(_entries);

  @override
  Future<PointReadEntry> putDictionaryEntry({
    required String term,
    required String text,
  }) async {
    putTerm = term;
    final entry = PointReadEntry(
      id: 'entry-${_entries.length + 1}',
      term: term,
      text: text,
    );
    _entries.removeWhere((existing) => existing.term == term);
    _entries.add(entry);
    return entry;
  }

  @override
  Future<void> deleteDictionaryEntry(String entryId) async {
    _entries.removeWhere((entry) => entry.id == entryId);
  }

  @override
  Future<VideoPlaybackPlan> fetchPlayback(String deviceId) async {
    return const VideoPlaybackPlan(bitrateKbps: 2000, fps: 24);
  }

  @override
  Future<VideoPlaybackPlan> resolvePlayback({
    required String deviceId,
    required int bitrateKbps,
    required int fps,
  }) async {
    return VideoPlaybackPlan(bitrateKbps: bitrateKbps, fps: fps);
  }
}
