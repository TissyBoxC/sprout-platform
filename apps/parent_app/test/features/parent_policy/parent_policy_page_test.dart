import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/parent_policy/application/parent_policy_controller.dart';
import 'package:parent_app/features/parent_policy/domain/parent_policy.dart';
import 'package:parent_app/features/parent_policy/presentation/parent_policy_page.dart';

void main() {
  testWidgets('asks before leaving with unsaved policy changes', (
    tester,
  ) async {
    await tester.pumpWidget(_policyApp(_ControlledPolicyController(_policy())));
    await tester.pumpAndSettle();

    await tester.drag(find.byType(Slider).first, const Offset(80, 0));
    await tester.pump();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();

    expect(find.text('放弃未保存的修改？'), findsOneWidget);
    expect(find.text('离开后这次修改不会保存。'), findsOneWidget);

    await tester.tap(find.text('继续编辑'));
    await tester.pumpAndSettle();
    expect(find.text('每日使用时长'), findsOneWidget);
  });

  testWidgets('shows a reload path when another page changed the policy', (
    tester,
  ) async {
    final controller = _ControlledPolicyController(
      _policy(),
      saveError: const AppException(
        kind: AppErrorKind.conflict,
        message: '设置已在其他页面更新，请重新加载后再修改',
        retryable: true,
      ),
    );
    await tester.pumpWidget(_policyApp(controller));
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(find.text('保存时间与内容设置'), 250);
    await tester.tap(find.text('保存时间与内容设置'));
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(find.text('设置已被更新'), -250);
    expect(find.text('设置已被更新'), findsOneWidget);
    expect(find.textContaining('以免覆盖其他入口的调整'), findsOneWidget);
    expect(find.text('重新加载设置'), findsOneWidget);
  });

  testWidgets('marks cross-midnight periods in the visible summary', (
    tester,
  ) async {
    await tester.pumpWidget(
      _policyApp(
        _ControlledPolicyController(
          _policy(
            periods: const [
              DisabledPeriod(startTime: '21:00', endTime: '07:00'),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(find.text('21:00 至 07:00（跨天）'), 200);
    await tester.pumpAndSettle();
    expect(find.text('21:00 至 07:00（跨天）'), findsOneWidget);
  });
}

Widget _policyApp(_ControlledPolicyController controller) {
  return ProviderScope(
    overrides: [
      parentPolicyControllerProvider(
        'child_demo',
      ).overrideWith(() => controller),
      childProfileControllerProvider.overrideWith(
        () => _FakeChildProfileController(),
      ),
    ],
    child: const MaterialApp(home: ParentPolicyPage(childId: 'child_demo')),
  );
}

ParentPolicy _policy({List<DisabledPeriod> periods = const []}) {
  return ParentPolicy(
    policyId: 'policy_demo',
    familyId: 'family_demo',
    childId: 'child_demo',
    policyVersion: 3,
    dailyLimitMinutes: 60,
    allowedCategories: ChildContentCategory.values,
    disabledPeriods: periods,
    maxVolumePercent: 70,
    voiceConversation: const VoiceConversationPolicy(
      continuousConversationEnabled: true,
      idleWindowSeconds: 8,
      bargeInEnabled: true,
      farFieldEnabled: true,
    ),
    updatedAt: DateTime.utc(2026, 10, 8),
  );
}

class _ControlledPolicyController extends ParentPolicyController {
  _ControlledPolicyController(this.policy, {this.saveError})
    : super('child_demo');

  final ParentPolicy policy;
  final Object? saveError;

  @override
  Future<ParentPolicy> build() async => policy;

  @override
  Future<ParentPolicy> savePolicy(ParentPolicyDraft draft) async {
    if (saveError != null) {
      throw saveError!;
    }
    return policy;
  }
}

class _FakeChildProfileController extends ChildProfileController {
  @override
  Future<List<ChildProfile>> build() async {
    return [
      ChildProfile(
        childId: 'child_demo',
        familyId: 'family_demo',
        nickname: '小芽',
        ageTier: ChildAgeTier.age3To4,
        interests: const [],
        contentCategories: ChildContentCategory.values,
        guardianConsent: true,
        createdAt: DateTime.utc(2026, 10, 1),
        updatedAt: DateTime.utc(2026, 10, 8),
      ),
    ];
  }
}
