import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../data/content_management_api.dart';
import '../domain/content_package.dart';
import 'content_library_controller.dart'
    show contentManagementApiProvider, contentLibraryControllerProvider;

/// Owns the platform-ranked content suggestions shown on the library page.
///
/// The controller reads the currently selected catalog filter so the section
/// follows the same age tier the guardian picked, and asks the platform to
/// exclude packages already visible in the catalog so the same card is never
/// rendered twice.
class ContentRecommendationController
    extends AsyncNotifier<List<ContentRecommendation>> {
  static const int _limit = 8;

  late ContentManagementApi _api;

  @override
  Future<List<ContentRecommendation>> build() async {
    _api = ref.read(contentManagementApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  Future<List<ContentRecommendation>> _load() async {
    // Wait for the library so the age tier and the exclusion list reflect the
    // catalog the guardian is currently looking at, even when the recommender
    // is the first of the two providers to resolve.
    final library = await ref.read(contentLibraryControllerProvider.future);
    final ageTier = library.filter.ageTier;
    final exclude = <String>[
      for (final entry in library.entries) entry.package.packageId,
    ];
    return _api.fetchRecommendations(
      ageTier: ageTier,
      limit: _limit,
      excludePackageIds: exclude,
    );
  }

  /// Maps a load failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final contentRecommendationControllerProvider =
    AsyncNotifierProvider<
      ContentRecommendationController,
      List<ContentRecommendation>
    >(ContentRecommendationController.new);
