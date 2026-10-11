import '../../../core/network/api_client.dart';
import '../domain/english.dart';

/// Guardian-facing English learning contracts: the read-only lesson catalog
/// plus the family-scoped practice progress. The platform scopes progress to
/// the authenticated family, so the client never sends an owner id.
abstract interface class EnglishApi {
  /// Lists published lessons, optionally narrowed to one age tier.
  Future<List<EnglishLessonSummary>> fetchLessons({String? ageTier});

  /// Loads one lesson with its ordered words and sentence patterns.
  Future<EnglishLesson> fetchLesson(String lessonId);

  /// Loads the family-scoped practice progress rollup.
  Future<EnglishProgress> fetchProgress({String? childId, String? lessonId});

  /// Lists the most recent practice attempts, newest first.
  Future<List<EnglishPracticeAttempt>> fetchAttempts({
    String? childId,
    String? lessonId,
  });
}

/// Production implementation backed by the authenticated API client.
class HttpEnglishApi implements EnglishApi {
  const HttpEnglishApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<List<EnglishLessonSummary>> fetchLessons({String? ageTier}) async {
    final response = await _apiClient.get(
      '/api/v1/english/lessons',
      queryParameters: <String, String>{
        if (ageTier != null && ageTier.isNotEmpty) 'age_tier': ageTier,
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing english lesson data');
    }
    final lessons = data['lessons'];
    if (lessons is! List) {
      return const <EnglishLessonSummary>[];
    }
    return lessons
        .whereType<Map>()
        .map(
          (item) => EnglishLessonSummary.fromJson(
            Map<String, Object?>.from(item),
          ),
        )
        .toList(growable: false);
  }

  @override
  Future<EnglishLesson> fetchLesson(String lessonId) async {
    final response = await _apiClient.get(
      '/api/v1/english/lessons/${Uri.encodeComponent(lessonId)}',
    );
    return _requiredLesson(response['data']);
  }

  @override
  Future<EnglishProgress> fetchProgress({
    String? childId,
    String? lessonId,
  }) async {
    final response = await _apiClient.get(
      '/api/v1/english/progress',
      queryParameters: <String, String>{
        if (childId != null && childId.isNotEmpty) 'child_id': childId,
        if (lessonId != null && lessonId.isNotEmpty) 'lesson_id': lessonId,
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing english progress data');
    }
    final envelope = Map<String, Object?>.from(data);
    return EnglishProgress(
      lessons: _decodeList(envelope['lesson_progress'], EnglishLessonProgress.fromJson),
      items: _decodeList(envelope['item_progress'], EnglishItemProgress.fromJson),
    );
  }

  @override
  Future<List<EnglishPracticeAttempt>> fetchAttempts({
    String? childId,
    String? lessonId,
  }) async {
    final response = await _apiClient.get(
      '/api/v1/english/attempts',
      queryParameters: <String, String>{
        if (childId != null && childId.isNotEmpty) 'child_id': childId,
        if (lessonId != null && lessonId.isNotEmpty) 'lesson_id': lessonId,
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing english attempt data');
    }
    final attempts = Map<String, Object?>.from(data)['attempts'];
    return _decodeList(attempts, EnglishPracticeAttempt.fromJson);
  }
}

EnglishLesson _requiredLesson(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing english lesson');
  }
  final envelope = Map<String, Object?>.from(data);
  final lesson = envelope['lesson'] ?? envelope;
  if (lesson is! Map) {
    throw const FormatException('missing english lesson');
  }
  return EnglishLesson.fromJson(Map<String, Object?>.from(lesson));
}

List<T> _decodeList<T>(
  Object? value,
  T Function(Map<String, Object?>) convert,
) {
  if (value is! List) {
    return const [];
  }
  return value
      .whereType<Map>()
      .map((item) => convert(Map<String, Object?>.from(item)))
      .toList(growable: false);
}
