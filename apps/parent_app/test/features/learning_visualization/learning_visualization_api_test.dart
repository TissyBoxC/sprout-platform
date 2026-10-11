import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/learning_visualization/data/learning_visualization_api.dart';
import 'package:parent_app/features/learning_visualization/domain/learning_visualization.dart';

void main() {
  test('parses the policy envelope with a conservative default', () async {
    final api = HttpLearningVisualizationApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'policy': <String, Object?>{
              'enabled': false,
              'feedback_style': 'neutral',
              'animation_level': 'off',
              'touch_feedback_enabled': true,
              'reward_effects_enabled': true,
              'lesson_linkage_enabled': true,
              'max_session_minutes': 20,
              'revision': 0,
            },
          },
        },
      ),
    );

    final policy = await api.fetchPolicy();

    expect(policy.enabled, isFalse);
    expect(policy.animationLevel, LearningAnimationLevel.off);
    expect(policy.feedbackStyle, LearningFeedbackStyle.neutral);
    expect(policy.maxSessionMinutes, 20);
    expect(policy.revision, 0);
  });

  test('publishes a policy with the draft JSON body', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'policy': <String, Object?>{
            'enabled': true,
            'feedback_style': 'celebration',
            'animation_level': 'full',
            'touch_feedback_enabled': true,
            'reward_effects_enabled': false,
            'lesson_linkage_enabled': true,
            'max_session_minutes': 30,
            'revision': 2,
          },
        },
      },
    );
    final api = HttpLearningVisualizationApi(client);

    final policy = await api.publishPolicy(
      const LearningVisualizationDraft(
        enabled: true,
        feedbackStyle: LearningFeedbackStyle.celebration,
        animationLevel: LearningAnimationLevel.full,
        touchFeedbackEnabled: true,
        rewardEffectsEnabled: false,
        lessonLinkageEnabled: true,
        maxSessionMinutes: 30,
      ),
    );

    expect(policy.feedbackStyle, LearningFeedbackStyle.celebration);
    expect(policy.revision, 2);
    expect(client.lastPutPath, '/api/v1/learning-visualization/policy');
    expect(client.lastPutBody, <String, Object?>{
      'enabled': true,
      'feedback_style': 'celebration',
      'animation_level': 'full',
      'touch_feedback_enabled': true,
      'reward_effects_enabled': false,
      'lesson_linkage_enabled': true,
      'max_session_minutes': 30,
    });
  });

  test('parses device status rows', () async {
    final api = HttpLearningVisualizationApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'devices': <Object?>[
              <String, Object?>{
                'device_id': 'device-1',
                'visualization_supported': true,
                'touch_supported': true,
                'linked_lesson_id': 'colors_basic',
                'visual_state': 'lesson',
                'feedback_style': 'encouraging',
                'animation_level': 'subtle',
                'feedback_events': 6,
                'session_minutes': 8,
                'up_to_date': true,
              },
            ],
          },
        },
      ),
    );

    final devices = await api.fetchDevices();

    expect(devices, hasLength(1));
    expect(devices.single.running, isTrue);
    expect(devices.single.linkedLessonId, 'colors_basic');
    expect(devices.single.feedbackEvents, 6);
    expect(devices.single.upToDate, isTrue);
    expect(visualStateLabel('lesson'), '学习中');
    expect(animationLevelLabel('subtle'), '轻微');
  });

  test('forwards the admin stats window', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'window_days': 30, 'device_count': 5},
      },
    );
    final api = HttpLearningVisualizationApi(client);

    final stats = await api.fetchAdminStats(windowDays: 30);

    expect(stats['device_count'], 5);
    expect(client.lastQueryParameters, <String, String>{'window_days': '30'});
  });

  test('defaults unknown style and state labels safely', () {
    expect(
      LearningFeedbackStyle.fromApiValue('flashy'),
      LearningFeedbackStyle.neutral,
    );
    expect(
      LearningAnimationLevel.fromApiValue('extreme'),
      LearningAnimationLevel.off,
    );
    expect(visualStateLabel('exploded'), '未知');
    expect(animationLevelLabel('extreme'), '未知');
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  Map<String, String>? lastQueryParameters;
  String? lastPutPath;
  Object? lastPutBody;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastQueryParameters = queryParameters;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async =>
      response;

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    lastPutPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async => response;

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
