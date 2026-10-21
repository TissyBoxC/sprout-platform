import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/device/application/device_child_controller.dart';
import 'package:parent_app/features/device/data/device_child_api.dart';
import 'package:parent_app/features/device/presentation/device_child_panel.dart';

void main() {
  group('DeviceChildPanel', () {
    testWidgets('marks the assigned child and offers switching', (
      tester,
    ) async {
      await tester.pumpWidget(
        _panelApp(
          status: const DeviceChildStatus(
            state: DeviceChildState.assigned,
            childId: 'child-1',
            childCount: 2,
          ),
        ),
      );
      await tester.pumpAndSettle();

      // The sentence tells the guardian which child the robot is talking to.
      expect(find.textContaining('现在和 小芽 对话'), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, '小苗'), findsOneWidget);
      expect(find.text('取消这台设备的儿童绑定'), findsOneWidget);
    });

    testWidgets('asks for a choice when children exist but none is selected', (
      tester,
    ) async {
      await tester.pumpWidget(
        _panelApp(
          status: const DeviceChildStatus(
            state: DeviceChildState.unassigned,
            childId: '',
            childCount: 2,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.textContaining('还没有选择儿童'), findsOneWidget);
      // No unbind action while nothing is bound.
      expect(find.text('取消这台设备的儿童绑定'), findsNothing);
    });

    testWidgets('points to adding a child when the family has none', (
      tester,
    ) async {
      await tester.pumpWidget(
        _panelApp(
          status: const DeviceChildStatus(
            state: DeviceChildState.noChild,
            childId: '',
            childCount: 0,
          ),
          children: const <ChildProfile>[],
        ),
      );
      await tester.pumpAndSettle();

      expect(find.textContaining('还没有儿童档案'), findsOneWidget);
      expect(find.byType(ChoiceChip), findsNothing);
    });

    testWidgets('switching shows a confirmation naming the child', (
      tester,
    ) async {
      final controller = _RecordingController(
        const DeviceChildStatus(
          state: DeviceChildState.assigned,
          childId: 'child-1',
          childCount: 2,
        ),
      );
      await tester.pumpWidget(_panelApp(controller: controller));
      await tester.pumpAndSettle();

      await tester.tap(find.widgetWithText(ChoiceChip, '小苗'));
      await tester.pumpAndSettle();

      expect(controller.activated, 'child-2');
      expect(find.text('已切换到小苗'), findsOneWidget);
    });

    testWidgets('clearing asks first and explains what is kept', (
      tester,
    ) async {
      final controller = _RecordingController(
        const DeviceChildStatus(
          state: DeviceChildState.assigned,
          childId: 'child-1',
          childCount: 2,
        ),
      );
      await tester.pumpWidget(_panelApp(controller: controller));
      await tester.pumpAndSettle();

      await tester.tap(find.text('取消这台设备的儿童绑定'));
      await tester.pumpAndSettle();

      expect(find.textContaining('对话记忆不会被删除'), findsOneWidget);
      await tester.tap(find.text('取消绑定'));
      await tester.pumpAndSettle();

      expect(controller.cleared, isTrue);
    });

    testWidgets('keeping the binding sends no clear request', (tester) async {
      final controller = _RecordingController(
        const DeviceChildStatus(
          state: DeviceChildState.assigned,
          childId: 'child-1',
          childCount: 2,
        ),
      );
      await tester.pumpWidget(_panelApp(controller: controller));
      await tester.pumpAndSettle();

      await tester.tap(find.text('取消这台设备的儿童绑定'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('保留绑定'));
      await tester.pumpAndSettle();

      expect(controller.cleared, isFalse);
    });
  });
}

class _FixedChildController extends ChildProfileController {
  _FixedChildController(this._profiles);

  final List<ChildProfile> _profiles;

  @override
  Future<List<ChildProfile>> build() async => _profiles;
}

class _RecordingController extends DeviceChildController {
  _RecordingController(this._initial) : super('device-1');

  final DeviceChildStatus _initial;
  String? activated;
  bool cleared = false;

  @override
  Future<DeviceChildStatus> build() async => _initial;

  @override
  Future<void> activate(String childId) async {
    activated = childId;
  }

  @override
  Future<void> clear() async {
    cleared = true;
  }

  @override
  Future<void> refresh() async {}
}

Widget _panelApp({
  DeviceChildStatus? status,
  List<ChildProfile>? children,
  _RecordingController? controller,
}) {
  final profiles = children ?? _children();
  final initial =
      status ??
      const DeviceChildStatus(
        state: DeviceChildState.assigned,
        childId: 'child-1',
        childCount: 2,
      );
  return ProviderScope(
    overrides: [
      childProfileControllerProvider.overrideWith(
        () => _FixedChildController(profiles),
      ),
      deviceChildControllerProvider(
        'device-1',
      ).overrideWith(() => controller ?? _RecordingController(initial)),
    ],
    child: MaterialApp(
      theme: AppTheme.light,
      home: const Scaffold(
        body: Padding(
          padding: EdgeInsets.all(16),
          child: DeviceChildPanel(deviceId: 'device-1'),
        ),
      ),
    ),
  );
}

List<ChildProfile> _children() {
  return <ChildProfile>[_child('child-1', '小芽'), _child('child-2', '小苗')];
}

ChildProfile _child(String childId, String nickname) {
  final now = DateTime.utc(2026);
  return ChildProfile(
    childId: childId,
    familyId: 'family-1',
    nickname: nickname,
    ageTier: ChildAgeTier.age3To4,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: now,
    updatedAt: now,
  );
}
