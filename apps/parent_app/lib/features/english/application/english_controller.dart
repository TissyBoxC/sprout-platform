import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/english_api.dart';
import '../domain/english.dart';

/// Combined English learning state: the lesson catalog, the family-scoped
/// practice progress, and the guardian's current age-tier selection.
class EnglishLearningState {
  const EnglishLearningState({
    required this.lessons,
    required this.progress,
    this.ageTier,
    this.childFilter,
  });

  const EnglishLearningState.initial()
    : lessons = const [],
      progress = const EnglishProgress.empty(),
      ageTier = null,
      childFilter = null;

  final List<EnglishLessonSummary> lessons;
  final EnglishProgress progress;

  /// When set, the catalog is narrowed to one age tier.
  final EnglishAgeTier? ageTier;

  /// When set, progress is narrowed to one child.
  final String? childFilter;

  bool get catalogEmpty => lessons.isEmpty;

  EnglishLessonProgress? progressFor(String lessonId) =>
      progress.forLesson(lessonId);
}

/// Owns the English lesson catalog and practice progress.
///
/// Catalog reads are public to any signed-in guardian; progress is always
/// family-scoped on the platform, so this controller only forwards an optional
/// child filter and never an owner id.
class EnglishController extends AsyncNotifier<EnglishLearningState> {
  late final EnglishApi _api;
  EnglishAgeTier? _ageTier;
  String? _childFilter;

  @override
  Future<EnglishLearningState> build() async {
    _api = ref.read(englishApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Narrows the catalog to one age tier, or clears it with `null`.
  Future<void> selectAgeTier(EnglishAgeTier? ageTier) async {
    if (ageTier == _ageTier) {
      return;
    }
    _ageTier = ageTier;
    await refresh();
  }

  /// Narrows progress to one child, or clears the filter with `null`.
  Future<void> filterByChild(String? childId) async {
    final normalized = (childId != null && childId.isNotEmpty) ? childId : null;
    if (normalized == _childFilter) {
      return;
    }
    _childFilter = normalized;
    await refresh();
  }

  /// Loads one lesson detail for the practice view.
  Future<EnglishLesson> loadLesson(String lessonId) {
    return _api.fetchLesson(lessonId);
  }

  /// Loads the latest practice attempts for one lesson.
  Future<List<EnglishPracticeAttempt>> loadAttempts(String lessonId) {
    return _api.fetchAttempts(lessonId: lessonId);
  }

  Future<EnglishLearningState> _load() async {
    final lessons = await _api.fetchLessons(ageTier: _ageTier?.apiValue);
    final progress = await _api.fetchProgress(childId: _childFilter);
    return EnglishLearningState(
      lessons: lessons,
      progress: progress,
      ageTier: _ageTier,
      childFilter: _childFilter,
    );
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final englishApiProvider = Provider<EnglishApi>((ref) {
  return HttpEnglishApi(ref.read(apiClientProvider));
});

final englishControllerProvider =
    AsyncNotifierProvider<EnglishController, EnglishLearningState>(
      EnglishController.new,
    );
