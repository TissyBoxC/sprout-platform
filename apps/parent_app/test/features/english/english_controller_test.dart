import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/english/application/english_controller.dart';
import 'package:parent_app/features/english/data/english_api.dart';
import 'package:parent_app/features/english/domain/english.dart';

void main() {
  test('loads the catalog and progress on first build', () async {
    final api = _FakeEnglishApi(
      lessons: [
        _lesson('lesson_fruit', EnglishAgeTier.age3to4),
        _lesson('lesson_school', EnglishAgeTier.age5to6),
      ],
      progress: _progress('lesson_fruit'),
    );
    final container = ProviderContainer(
      overrides: [englishApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(englishControllerProvider.future);

    expect(state.lessons, hasLength(2));
    expect(state.ageTier, isNull);
    expect(state.childFilter, isNull);
    expect(state.progressFor('lesson_fruit')?.bestScore, 80);
    expect(api.lastAgeTier, isNull);
  });

  test('narrows the catalog by age tier and clears it again', () async {
    final api = _FakeEnglishApi(
      lessons: [
        _lesson('lesson_fruit', EnglishAgeTier.age3to4),
        _lesson('lesson_school', EnglishAgeTier.age5to6),
      ],
    );
    final container = ProviderContainer(
      overrides: [englishApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(englishControllerProvider.future);
    final controller = container.read(englishControllerProvider.notifier);

    await controller.selectAgeTier(EnglishAgeTier.age3to4);
    expect(api.lastAgeTier, 'age_3_4');
    var state = container.read(englishControllerProvider).value;
    expect(state?.lessons.map((lesson) => lesson.lessonId), ['lesson_fruit']);

    // Selecting the same tier is a no-op: no extra request is sent.
    final before = api.catalogCalls;
    await controller.selectAgeTier(EnglishAgeTier.age3to4);
    expect(api.catalogCalls, before);

    await controller.selectAgeTier(null);
    expect(api.lastAgeTier, isNull);
    state = container.read(englishControllerProvider).value;
    expect(state?.lessons, hasLength(2));
  });

  test('filters the progress by child', () async {
    final api = _FakeEnglishApi(lessons: const []);
    final container = ProviderContainer(
      overrides: [englishApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(englishControllerProvider.future);
    final controller = container.read(englishControllerProvider.notifier);

    await controller.filterByChild('child-1');
    expect(api.lastChildFilter, 'child-1');
    expect(
      container.read(englishControllerProvider).value?.childFilter,
      'child-1',
    );

    await controller.filterByChild(null);
    expect(api.lastChildFilter, isNull);
  });

  test('loads one lesson detail for the practice view', () async {
    final api = _FakeEnglishApi(lessons: const []);
    final container = ProviderContainer(
      overrides: [englishApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(englishControllerProvider.future);
    final controller = container.read(englishControllerProvider.notifier);

    final lesson = await controller.loadLesson('lesson_fruit');

    expect(lesson.lessonId, 'lesson_fruit');
    expect(lesson.words.single.term, 'apple');
    expect(api.lastLessonId, 'lesson_fruit');
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeEnglishApi(lessons: const [], failLoad: true);
    final container = ProviderContainer(
      overrides: [englishApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      englishControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(englishControllerProvider).hasError, isTrue);
  });
}

EnglishLessonSummary _lesson(String id, EnglishAgeTier tier) {
  return EnglishLessonSummary(
    lessonId: id,
    ageTier: tier.apiValue,
    title: '课程 $id',
    summary: '',
    sequence: 1,
    wordCount: 2,
    sentenceCount: 1,
  );
}

EnglishProgress _progress(String lessonId) {
  return EnglishProgress(
    lessons: [
      EnglishLessonProgress(
        lessonId: lessonId,
        attempts: 2,
        passedItems: 1,
        totalItems: 2,
        bestScore: 80,
        averageScore: 65,
        lastPracticed: DateTime.utc(2026, 10, 11, 9),
      ),
    ],
    items: [
      EnglishItemProgress(
        lessonId: lessonId,
        itemId: 'word-apple',
        itemType: EnglishItemType.word,
        targetText: 'apple',
        bestScore: 80,
        attempts: 2,
        passed: true,
        lastAt: DateTime.utc(2026, 10, 11, 9),
      ),
    ],
  );
}

class _FakeEnglishApi implements EnglishApi {
  _FakeEnglishApi({
    required List<EnglishLessonSummary> lessons,
    EnglishProgress? progress,
    this.failLoad = false,
  }) : _lessons = [...lessons],
       _progress = progress ?? const EnglishProgress.empty();

  final List<EnglishLessonSummary> _lessons;
  final EnglishProgress _progress;
  final bool failLoad;
  int catalogCalls = 0;
  String? lastAgeTier;
  String? lastChildFilter;
  String? lastLessonId;

  @override
  Future<List<EnglishLessonSummary>> fetchLessons({String? ageTier}) async {
    catalogCalls += 1;
    lastAgeTier = ageTier;
    if (failLoad) {
      throw Exception('network down');
    }
    if (ageTier == null || ageTier.isEmpty) {
      return List<EnglishLessonSummary>.unmodifiable(_lessons);
    }
    return List<EnglishLessonSummary>.unmodifiable(
      _lessons.where((lesson) => lesson.ageTier == ageTier),
    );
  }

  @override
  Future<EnglishLesson> fetchLesson(String lessonId) async {
    lastLessonId = lessonId;
    if (failLoad) {
      throw Exception('network down');
    }
    return EnglishLesson(
      lessonId: lessonId,
      ageTier: EnglishAgeTier.age3to4.apiValue,
      title: '水果',
      summary: '',
      sequence: 1,
      words: const [
        EnglishWord(
          id: 'word-apple',
          position: 1,
          term: 'apple',
          phonetic: 'ˈæp.əl',
          translation: '苹果',
          example: 'I like apples.',
        ),
      ],
      sentences: const [],
    );
  }

  @override
  Future<EnglishProgress> fetchProgress({String? childId, String? lessonId}) async {
    lastChildFilter = childId;
    if (failLoad) {
      throw Exception('network down');
    }
    if (lessonId != null && lessonId.isNotEmpty) {
      final match = _progress.forLesson(lessonId);
      return EnglishProgress(
        lessons: match == null ? const [] : [match],
        items: _progress.itemsForLesson(lessonId),
      );
    }
    return _progress;
  }

  @override
  Future<List<EnglishPracticeAttempt>> fetchAttempts({
    String? childId,
    String? lessonId,
  }) async {
    return const [];
  }
}
