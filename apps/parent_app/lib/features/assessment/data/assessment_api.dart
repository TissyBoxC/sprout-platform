import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../domain/assessment.dart';
import '../presentation/assessment_copy.dart';

/// The lesson content one assessment item is drawn from.
///
/// The platform only returns an item id and its text, so this mirrors the
/// English lesson the guardian picked. Ordering follows the platform: words
/// first, then sentence patterns, each in lesson order.
class AssessmentLessonItem {
  const AssessmentLessonItem({
    required this.itemId,
    required this.itemType,
    required this.text,
  });

  final String itemId;

  /// `word` or `sentence`.
  final String itemType;
  final String text;

  bool get isWord => itemType == 'word';
}

/// Guardian-facing assessment operations.
///
/// Every call is addressed through the signed-in family, so the platform can
/// check the guardian's role and keep another family's run out of reach. The
/// parent app scores each item because no device recognition happens here:
/// nothing in this layer invents a score.
abstract interface class AssessmentApi {
  /// The start reply carries the first item beside the run, so the guardian can
  /// begin reading straight away.
  ///
  /// Throws an [AssessmentRefused] when the platform refuses.
  Future<AssessmentSession> start({
    required String lessonId,
    String childId = '',
  });

  /// Reads one run back, for a refresh or a resume after leaving the page.
  Future<AssessmentSession> fetch(String sessionId);

  /// Records one item's score, between 0 and 100.
  Future<AssessmentAnswer> answer({
    required String sessionId,
    required AssessmentItem item,
    required int score,
  });

  /// Ends a run explicitly. This is the only way a run stops on purpose.
  Future<AssessmentSession> finish(String sessionId);

  /// Lists the family's runs, newest first.
  Future<List<AssessmentSession>> history({int limit});
}

/// Production implementation backed by the authenticated API client.
class HttpAssessmentApi implements AssessmentApi {
  const HttpAssessmentApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String get _base => '/api/v1/families/${Uri.encodeComponent(_familyId)}';

  String _sessionPath(String sessionId) =>
      '$_base/assessment-sessions/${Uri.encodeComponent(sessionId)}';

  @override
  Future<AssessmentSession> start({
    required String lessonId,
    String childId = '',
  }) {
    return _guarded(() async {
      final response = await _apiClient.post(
        '$_base/assessment-sessions',
        body: <String, Object?>{
          'lesson_id': lessonId,
          if (childId.isNotEmpty) 'child_id': childId,
        },
      );
      return _sessionFrom(response)..firstItem = _itemFrom(response);
    });
  }

  @override
  Future<AssessmentSession> fetch(String sessionId) {
    return _guarded(() async {
      final response = await _apiClient.get(_sessionPath(sessionId));
      // A detail read also carries the item to show next, which is what lets a
      // guardian return to a run without re-reading an answered question.
      return _sessionFrom(response)..firstItem = _itemFrom(response);
    });
  }

  @override
  Future<AssessmentAnswer> answer({
    required String sessionId,
    required AssessmentItem item,
    required int score,
  }) {
    return _guarded(() async {
      final response = await _apiClient.post(
        '${_sessionPath(sessionId)}/answers',
        body: <String, Object?>{
          'item_id': item.itemId,
          'item_type': item.itemType,
          'target_text': item.text,
          'score': score.clamp(0, 100),
        },
      );
      final session = _sessionFrom(response);
      return AssessmentAnswer(
        session: session,
        answeredCount: session.answeredCount,
        correctCount: session.correctCount,
        difficulty: session.difficulty,
        nextItem: _itemFrom(response),
      );
    });
  }

  @override
  Future<AssessmentSession> finish(String sessionId) {
    return _guarded(() async {
      final response = await _apiClient.post('${_sessionPath(sessionId)}/finish');
      return _sessionFrom(response);
    });
  }

  @override
  Future<List<AssessmentSession>> history({int limit = 20}) async {
    final response = await _apiClient.get(
      '$_base/assessment-sessions',
      queryParameters: <String, String>{'limit': '$limit'},
    );
    final data = response['data'];
    final sessions = data is Map ? data['sessions'] : null;
    if (sessions is! List) {
      return const <AssessmentSession>[];
    }
    return sessions
        .whereType<Map>()
        .map(
          (entry) =>
              AssessmentSession.fromJson(Map<String, Object?>.from(entry)),
        )
        .toList(growable: false);
  }

  /// Runs one call and replaces a coded refusal with a guardian-facing one.
  ///
  /// The platform's refusal code is the only durable part of the failure, so it
  /// is read and translated at this boundary. Nothing from the response body
  /// leaves this method.
  Future<T> _guarded<T>(Future<T> Function() call) async {
    try {
      return await call();
    } on AppException catch (error) {
      final refusal = _refusalFrom(error);
      if (refusal == null) {
        rethrow;
      }
      throw AssessmentRefused(refusal);
    }
  }

  AssessmentSession _sessionFrom(Map<String, Object?> response) {
    final data = response['data'];
    return AssessmentSession.fromJson(
      data is Map ? Map<String, Object?>.from(data) : <String, Object?>{},
    );
  }

  AssessmentItem _itemFrom(Map<String, Object?> response) {
    final data = response['data'];
    final item = data is Map ? data['next_item'] : null;
    if (item is! Map) {
      return AssessmentItem.empty;
    }
    return AssessmentItem.fromJson(Map<String, Object?>.from(item));
  }
}

AssessmentRefusal? _refusalFrom(AppException error) {
  final code = readApiErrorCode(_bodyOf(error.cause));
  return switch (code) {
    'assessment_already_active' => AssessmentRefusal.alreadyActive,
    'assessment_rate_limited' => AssessmentRefusal.rateLimited,
    'assessment_expired' => AssessmentRefusal.expired,
    'assessment_state_changed' => AssessmentRefusal.stateChanged,
    'assessment_lesson_not_found' => AssessmentRefusal.lessonNotFound,
    'assessment_child_not_found' => AssessmentRefusal.childNotFound,
    'assessment_not_found' => AssessmentRefusal.sessionNotFound,
    // The remaining codes describe a broken payload or an internal failure.
    // None of them is the guardian's to fix, so they fall through to the
    // generic sentence rather than being translated into a misleading hint.
    _ => null,
  };
}

/// Extracts the error body the transport attached to a mapped failure.
Object? _bodyOf(Object? cause) {
  if (cause == null) {
    return null;
  }
  final dynamic transportError = cause;
  try {
    return transportError.response?.data;
  } on Object {
    return null;
  }
}
