import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/learning_visualization/application/learning_visualization_controller.dart';
import 'package:parent_app/features/learning_visualization/data/learning_visualization_api.dart';
import 'package:parent_app/features/learning_visualization/domain/learning_visualization.dart';

void main() {
  test('loads the policy and devices on first build', () async {
    final api = _FakeLearningVisualizationApi(
      policy: _policy(enabled: false),
      devices: [
        _device('device-1', 'lesson', upToDate: true),
        _device('device-2', 'idle', upToDate: false),
      ],
    );
    final container = ProviderContainer(
      overrides: [learningVisualizationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(
      learningVisualizationControllerProvider.future,
    );

    expect(state.policy.enabled, isFalse);
    expect(state.devices, hasLength(2));
    expect(state.isEmpty, isFalse);
    expect(state.runningCount, 1);
    expect(state.outdatedCount, 1);
  });

  test('publishes a policy then reloads', () async {
    final api = _FakeLearningVisualizationApi(policy: _policy(enabled: false));
    final container = ProviderContainer(
      overrides: [learningVisualizationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(learningVisualizationControllerProvider.future);
    final controller = container.read(
      learningVisualizationControllerProvider.notifier,
    );
    final published = await controller.publishPolicy(
      const LearningVisualizationDraft(
        enabled: true,
        feedbackStyle: LearningFeedbackStyle.playful,
        animationLevel: LearningAnimationLevel.full,
        touchFeedbackEnabled: true,
        rewardEffectsEnabled: true,
        lessonLinkageEnabled: true,
        maxSessionMinutes: 45,
      ),
    );

    expect(published.enabled, isTrue);
    expect(published.feedbackStyle, LearningFeedbackStyle.playful);
    final state = container.read(learningVisualizationControllerProvider).value;
    expect(state?.policy.enabled, isTrue);
    expect(api.publishCalls, 1);
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeLearningVisualizationApi(
      policy: _policy(enabled: false),
      failLoad: true,
    );
    final container = ProviderContainer(
      overrides: [learningVisualizationApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      learningVisualizationControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(
      container.read(learningVisualizationControllerProvider).hasError,
      isTrue,
    );
  });
}

LearningVisualizationPolicy _policy({required bool enabled}) {
  return LearningVisualizationPolicy(
    enabled: enabled,
    feedbackStyle: LearningFeedbackStyle.neutral,
    animationLevel: enabled
        ? LearningAnimationLevel.subtle
        : LearningAnimationLevel.off,
    touchFeedbackEnabled: true,
    rewardEffectsEnabled: true,
    lessonLinkageEnabled: true,
    maxSessionMinutes: 20,
    revision: enabled ? 1 : 0,
  );
}

LearningVisualizationDevice _device(
  String id,
  String visualState, {
  required bool upToDate,
}) {
  return LearningVisualizationDevice(
    deviceId: id,
    supported: true,
    touchSupported: true,
    linkedLessonId: visualState == 'lesson' ? 'colors_basic' : '',
    visualState: visualState,
    feedbackStyle: 'neutral',
    animationLevel: visualState == 'lesson' ? 'subtle' : 'off',
    feedbackEvents: visualState == 'lesson' ? 4 : 0,
    sessionMinutes: visualState == 'lesson' ? 6 : 0,
    upToDate: upToDate,
  );
}

class _FakeLearningVisualizationApi implements LearningVisualizationApi {
  _FakeLearningVisualizationApi({
    required LearningVisualizationPolicy policy,
    List<LearningVisualizationDevice> devices = const [],
    this.failLoad = false,
  }) : _policy = policy,
       _devices = [...devices];

  LearningVisualizationPolicy _policy;
  final List<LearningVisualizationDevice> _devices;
  final bool failLoad;
  int publishCalls = 0;

  @override
  Future<LearningVisualizationPolicy> fetchPolicy() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return _policy;
  }

  @override
  Future<List<LearningVisualizationDevice>> fetchDevices() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return List<LearningVisualizationDevice>.unmodifiable(_devices);
  }

  @override
  Future<LearningVisualizationPolicy> publishPolicy(
    LearningVisualizationDraft draft,
  ) async {
    publishCalls += 1;
    _policy = LearningVisualizationPolicy(
      enabled: draft.enabled,
      feedbackStyle: draft.feedbackStyle,
      animationLevel: draft.animationLevel,
      touchFeedbackEnabled: draft.touchFeedbackEnabled,
      rewardEffectsEnabled: draft.rewardEffectsEnabled,
      lessonLinkageEnabled: draft.lessonLinkageEnabled,
      maxSessionMinutes: draft.maxSessionMinutes,
      revision: _policy.revision + 1,
    );
    return _policy;
  }

  @override
  Future<Map<String, Object?>> fetchAdminStats({int windowDays = 7}) async {
    return <String, Object?>{'window_days': windowDays};
  }
}
