import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/english/data/english_api.dart';
import 'package:parent_app/features/english/domain/english.dart';

void main() {
  test('parses lesson summaries and forwards the age tier filter', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'lessons': <Object?>[
            <String, Object?>{
              'lesson_id': 'lesson_fruit',
              'age_tier': 'age_3_4',
              'title': '水果',
              'summary': '认识常见水果',
              'sequence': 1,
              'word_count': 3,
              'sentence_count': 1,
            },
          ],
        },
      },
    );
    final api = HttpEnglishApi(client);

    final lessons = await api.fetchLessons(ageTier: 'age_3_4');

    expect(lessons, hasLength(1));
    expect(lessons.single.lessonId, 'lesson_fruit');
    expect(lessons.single.wordCount, 3);
    expect(client.lastQueryParameters, <String, String>{
      'age_tier': 'age_3_4',
    });
  });

  test('omits the age tier filter when listing the whole catalog', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'lessons': <Object?>[]},
      },
    );
    final api = HttpEnglishApi(client);

    final lessons = await api.fetchLessons();

    expect(lessons, isEmpty);
    expect(client.lastQueryParameters, isEmpty);
  });

  test('parses a lesson detail with words and sentences', () async {
    final api = HttpEnglishApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'lesson': <String, Object?>{
              'lesson_id': 'lesson_fruit',
              'age_tier': 'age_3_4',
              'title': '水果',
              'summary': '',
              'sequence': 1,
              'words': <Object?>[
                <String, Object?>{
                  'id': 'word-apple',
                  'position': 1,
                  'term': 'apple',
                  'phonetic': 'ˈæp.əl',
                  'translation': '苹果',
                  'example': 'I like apples.',
                },
              ],
              'sentences': <Object?>[
                <String, Object?>{
                  'id': 'sent-like',
                  'position': 1,
                  'pattern': 'I like ___.',
                  'prompt': '说一句你喜欢的水果',
                  'model_answer': 'I like apples.',
                },
              ],
            },
          },
        },
      ),
    );

    final lesson = await api.fetchLesson('lesson_fruit');

    expect(lesson.words.single.term, 'apple');
    expect(lesson.words.single.translation, '苹果');
    expect(lesson.sentences.single.pattern, 'I like ___.');
  });

  test('encodes the lesson id in the detail path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'lesson': <String, Object?>{'lesson_id': 'x'}},
      },
    );
    final api = HttpEnglishApi(client);

    await api.fetchLesson('lesson fruit');

    expect(client.lastGetPath, '/api/v1/english/lessons/lesson%20fruit');
  });

  test('parses the progress envelope with child and lesson filters', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'lesson_progress': <Object?>[
            <String, Object?>{
              'lesson_id': 'lesson_fruit',
              'attempts': 2,
              'passed_items': 1,
              'total_items': 3,
              'best_score': 80,
              'average_score': 60,
              'last_practiced': '2026-10-11T09:00:00Z',
            },
          ],
          'item_progress': <Object?>[
            <String, Object?>{
              'lesson_id': 'lesson_fruit',
              'item_id': 'word-apple',
              'item_type': 'word',
              'target_text': 'apple',
              'best_score': 80,
              'attempts': 2,
              'passed': true,
              'last_at': '2026-10-11T09:00:00Z',
            },
          ],
        },
      },
    );
    final api = HttpEnglishApi(client);

    final progress = await api.fetchProgress(
      childId: 'child-1',
      lessonId: 'lesson_fruit',
    );

    expect(progress.lessons, hasLength(1));
    expect(progress.lessons.single.passedItems, 1);
    expect(progress.lessons.single.completion, closeTo(1 / 3, 0.0001));
    expect(progress.items.single.passed, isTrue);
    expect(progress.items.single.itemType, EnglishItemType.word);
    expect(progress.forLesson('lesson_fruit'), isNotNull);
    expect(progress.itemsForLesson('lesson_fruit'), hasLength(1));
    expect(client.lastQueryParameters, <String, String>{
      'child_id': 'child-1',
      'lesson_id': 'lesson_fruit',
    });
  });

  test('parses the attempts list', () async {
    final api = HttpEnglishApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'attempts': <Object?>[
              <String, Object?>{
                'id': 'attempt-1',
                'child_id': 'child-1',
                'lesson_id': 'lesson_fruit',
                'item_id': 'word-apple',
                'item_type': 'sentence',
                'target_text': 'I like apples.',
                'recognized_text': 'I like apple',
                'score': 72,
                'passed': true,
                'practiced_at': '2026-10-11T09:00:00Z',
              },
            ],
          },
        },
      ),
    );

    final attempts = await api.fetchAttempts();

    expect(attempts, hasLength(1));
    expect(attempts.single.itemType, EnglishItemType.sentence);
    expect(attempts.single.score, 72);
    expect(attempts.single.passed, isTrue);
  });

  test('rejects an unknown item type in an attempt', () {
    expect(
      () => EnglishPracticeAttempt.fromJson(<String, Object?>{
        'item_type': 'phonics',
      }),
      throwsA(isA<FormatException>()),
    );
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  Map<String, String>? lastQueryParameters;
  String? lastGetPath;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastGetPath = path;
    lastQueryParameters = queryParameters;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> delete(String path) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
