import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/memory/application/memory_controller.dart';
import 'package:parent_app/features/memory/data/memory_api.dart';
import 'package:parent_app/features/memory/domain/child_memory.dart';
import 'package:parent_app/features/memory/presentation/memory_page.dart';

void main() {
  group('MemoryPage', () {
    testWidgets('lists remembered facts with their source', (tester) async {
      await tester.pumpWidget(
        _memoryApp(
          memories: <ChildMemory>[
            _memory(memoryKey: 'favorite_color', memoryValue: '粉色'),
            _memory(
              memoryKey: 'pet_name',
              memoryValue: '豆豆',
              source: MemorySource.guardian,
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('favorite_color'), findsOneWidget);
      expect(find.text('粉色'), findsOneWidget);
      expect(find.text('pet_name'), findsOneWidget);
      expect(find.text('豆豆'), findsOneWidget);
      expect(find.text('家长设置'), findsOneWidget);
    });

    testWidgets('explains what to do when nothing is remembered yet', (
      tester,
    ) async {
      await tester.pumpWidget(_memoryApp(memories: const <ChildMemory>[]));
      await tester.pumpAndSettle();

      expect(find.text('还没有记忆'), findsOneWidget);
      expect(find.text('添加一条记忆'), findsOneWidget);
    });

    testWidgets('tells the guardian to add a child first', (tester) async {
      await tester.pumpWidget(
        _memoryApp(
          memories: const <ChildMemory>[],
          children: const <ChildProfile>[],
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('还没有儿童档案'), findsOneWidget);
    });

    testWidgets('shows a readable retry state instead of raw errors', (
      tester,
    ) async {
      await tester.pumpWidget(
        _memoryApp(
          error: const AppException(
            kind: AppErrorKind.serviceUnavailable,
            message: '暂时读不到记忆，请稍后重新加载。',
            retryable: true,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('暂时读不到记忆，请稍后重新加载。'), findsOneWidget);
      expect(find.text('重新加载'), findsOneWidget);
    });

    testWidgets('asks before deleting every memory', (tester) async {
      final api = _FakeMemoryApi(
        memories: <ChildMemory>[_memory(memoryKey: 'favorite_color')],
      );
      await tester.pumpWidget(_memoryApp(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('清空全部记忆'));
      await tester.pumpAndSettle();

      expect(find.text('清空全部记忆'), findsWidgets);
      expect(find.text('删除后无法恢复，机器人将忘记关于这个孩子的所有记忆。'), findsOneWidget);

      await tester.tap(find.text('清空记忆'));
      await tester.pumpAndSettle();

      expect(api.clearedChildren, contains('child-1'));
    });

    testWidgets('deletes one entry and keeps the rest', (tester) async {
      final api = _FakeMemoryApi(
        memories: <ChildMemory>[
          _memory(memoryKey: 'favorite_color'),
          _memory(memoryKey: 'pet_name'),
        ],
      );
      await tester.pumpWidget(_memoryApp(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('删除记忆').first);
      await tester.pumpAndSettle();
      await tester.tap(find.text('删除记忆').last);
      await tester.pumpAndSettle();

      expect(api.deletedKeys, contains('favorite_color'));
      expect(find.text('pet_name'), findsOneWidget);
    });
  });

  group('ChildMemory.fromJson', () {
    test('reads the wire contract and tolerates a missing timestamp', () {
      final memory = ChildMemory.fromJson(<String, dynamic>{
        'id': 'm-1',
        'child_id': 'c-1',
        'memory_key': 'favorite_color',
        'memory_value': '粉色',
        'confidence': 0.8,
        'source': 'device',
        'revision': 7,
        'updated_at': 'not-a-timestamp',
      });

      expect(memory.memoryKey, 'favorite_color');
      expect(memory.source, MemorySource.device);
      expect(memory.confidence, 0.8);
      expect(memory.revision, 7);
      expect(memory.updatedAt, isNull);
      expect(memory.isGuardianAuthored, isFalse);
    });

    test('falls back to the conversation source for an unknown value', () {
      final memory = ChildMemory.fromJson(<String, dynamic>{
        'memory_key': 'k',
        'memory_value': 'v',
        'source': 'something-new',
      });

      expect(memory.source, MemorySource.conversation);
      expect(memory.confidence, 0);
      expect(memory.revision, 0);
    });
  });
}

class _FakeMemoryApi implements MemoryApi {
  _FakeMemoryApi({List<ChildMemory> memories = const <ChildMemory>[]})
    : _memories = List<ChildMemory>.of(memories);

  final List<ChildMemory> _memories;
  final List<String> clearedChildren = <String>[];
  final List<String> deletedKeys = <String>[];

  @override
  Future<List<ChildMemory>> fetchMemories(String childId) async {
    return List<ChildMemory>.of(_memories);
  }

  @override
  Future<ChildMemory> saveMemory({
    required String childId,
    required String memoryKey,
    required String memoryValue,
  }) async {
    final saved = _memory(memoryKey: memoryKey, memoryValue: memoryValue);
    _memories
      ..removeWhere((memory) => memory.memoryKey == memoryKey)
      ..add(saved);
    return saved;
  }

  @override
  Future<void> deleteMemory({
    required String childId,
    required String memoryKey,
  }) async {
    deletedKeys.add(memoryKey);
    _memories.removeWhere((memory) => memory.memoryKey == memoryKey);
  }

  @override
  Future<void> clearMemories(String childId) async {
    clearedChildren.add(childId);
    _memories.clear();
  }
}

/// Overrides the memory controller with fixed data so the page renders without
/// a network path.
class _FixedMemoryController extends ChildMemoryController {
  _FixedMemoryController({
    required this.memories,
    required this.api,
    this.error,
  });

  final List<ChildMemory> memories;
  final MemoryApi api;
  final Object? error;

  ChildMemoryState _state(String childId) {
    return ChildMemoryState(childId: childId, memories: memories);
  }

  @override
  Future<ChildMemoryState> build() async {
    final failure = error;
    if (failure != null) {
      throw failure;
    }
    return _state('child-1');
  }

  @override
  Future<void> load(String childId) async {
    state = AsyncValue<ChildMemoryState>.data(_state(childId));
  }

  @override
  Future<void> remove({
    required String childId,
    required String memoryKey,
  }) async {
    await api.deleteMemory(childId: childId, memoryKey: memoryKey);
    await load(childId);
  }

  @override
  Future<void> clear(String childId) async {
    await api.clearMemories(childId);
    await load(childId);
  }
}

Widget _memoryApp({
  List<ChildMemory> memories = const <ChildMemory>[],
  List<ChildProfile>? children,
  Object? error,
  _FakeMemoryApi? api,
}) {
  final fakeApi = api ?? _FakeMemoryApi(memories: memories);
  final profiles = children ?? <ChildProfile>[_child()];
  return ProviderScope(
    overrides: [
      childProfileControllerProvider.overrideWith(
        () => _FixedChildController(profiles),
      ),
      childMemoryControllerProvider.overrideWith(
        () => _FixedMemoryController(
          memories: fakeApi._memories,
          api: fakeApi,
          error: error,
        ),
      ),
    ],
    child: MaterialApp(
      theme: AppTheme.light,
      home: const MemoryPage(),
    ),
  );
}

class _FixedChildController extends ChildProfileController {
  _FixedChildController(this._profiles);

  final List<ChildProfile> _profiles;

  @override
  Future<List<ChildProfile>> build() async {
    return _profiles;
  }
}

ChildProfile _child() {
  final now = DateTime.utc(2026);
  return ChildProfile(
    childId: 'child-1',
    familyId: 'family-1',
    nickname: '小芽',
    ageTier: ChildAgeTier.age3To4,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: now,
    updatedAt: now,
  );
}

ChildMemory _memory({
  String memoryKey = 'favorite_color',
  String memoryValue = '粉色',
  MemorySource source = MemorySource.conversation,
}) {
  return ChildMemory(
    id: 'm-$memoryKey',
    childId: 'child-1',
    memoryKey: memoryKey,
    memoryValue: memoryValue,
    confidence: 0.9,
    source: source,
    revision: 1,
    updatedAt: null,
  );
}
