import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/vision/application/vision_controller.dart';
import 'package:parent_app/features/vision/data/vision_api.dart';
import 'package:parent_app/features/vision/domain/vision_task.dart';
import 'package:parent_app/features/vision/presentation/vision_page.dart';

void main() {
  testWidgets('shows an authorization prompt before image consent', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          visionApiProvider.overrideWithValue(
            _FakeVisionApi(
              capability: const VisionCapability(
                supported: true,
                consentGranted: false,
                reconsentPath: '/me/privacy',
                retentionHours: 24,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: VisionPage(deviceId: 'sprout_device_001')),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('尚未授权使用照片'), findsOneWidget);
    expect(find.text('去授权'), findsOneWidget);
    expect(find.text('还没有发起过任务。'), findsOneWidget);
    final objectButton = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '识物'),
    );
    expect(objectButton.onPressed, isNull);
  });

  testWidgets('shows recognized results and enables task actions', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          visionApiProvider.overrideWithValue(
            _FakeVisionApi(
              capability: const VisionCapability(
                supported: true,
                consentGranted: true,
                reconsentPath: '/me/privacy',
                retentionHours: 24,
              ),
              tasks: const [
                VisionTask(
                  taskId: 'task-1',
                  deviceId: 'sprout_device_001',
                  taskType: 'object_recognition',
                  status: 'pending',
                  prompt: '',
                  failureCode: '',
                  createdAt: null,
                  expiresAt: null,
                ),
              ],
              results: const [
                VisionResult(
                  taskId: 'task-1',
                  deviceId: 'sprout_device_001',
                  taskType: 'object_recognition',
                  labels: [
                    VisionLabel(label: 'teddy bear', confidence: 9000),
                  ],
                  summary: '一只玩具熊',
                  createdAt: null,
                  expiresAt: null,
                ),
              ],
            ),
          ),
        ],
        child: const MaterialApp(home: VisionPage(deviceId: 'sprout_device_001')),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('拍照识别已授权'), findsOneWidget);
    expect(find.text('等待设备'), findsOneWidget);
    expect(find.text('一只玩具熊'), findsOneWidget);
    final objectButton = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '识物'),
    );
    expect(objectButton.onPressed, isNotNull);
  });
}

class _FakeVisionApi implements VisionApi {
  _FakeVisionApi({
    required this.capability,
    this.tasks = const <VisionTask>[],
    this.results = const <VisionResult>[],
  });

  final VisionCapability capability;
  final List<VisionTask> tasks;
  final List<VisionResult> results;

  @override
  Future<VisionCapability> fetchCapability(String deviceId) async =>
      capability;

  @override
  Future<List<VisionTask>> listTasks(String deviceId) async => tasks;

  @override
  Future<List<VisionResult>> listResults(String deviceId) async => results;

  @override
  Future<VisionTask> createTask(
    String deviceId, {
    required String taskType,
    String prompt = '',
  }) async {
    throw UnimplementedError();
  }

  @override
  Future<void> cancelTask(String deviceId, String taskId) async {}
}
