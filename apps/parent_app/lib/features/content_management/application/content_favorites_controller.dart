import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../data/content_management_api.dart';
import '../domain/content_package.dart';
import 'content_library_controller.dart' show contentManagementApiProvider;

/// State rendered by the guardian favorites view.
class ContentFavoritesState {
  const ContentFavoritesState({
    required this.favorites,
    required this.pendingPackageIds,
    required this.hasLoadedOnce,
    this.errorMessage,
  });

  const ContentFavoritesState.initial()
    : favorites = const [],
      pendingPackageIds = const <String>{},
      hasLoadedOnce = false,
      errorMessage = null;

  final List<ContentFavorite> favorites;

  /// Package ids with an in-flight add/remove so the star can show a spinner
  /// and ignore a second tap.
  final Set<String> pendingPackageIds;
  final bool hasLoadedOnce;
  final String? errorMessage;

  bool get isEmpty => favorites.isEmpty;

  bool isFavorite(String packageId) =>
      favorites.any((favorite) => favorite.packageId == packageId);

  bool isPending(String packageId) => pendingPackageIds.contains(packageId);

  ContentFavoritesState copyWith({
    List<ContentFavorite>? favorites,
    Set<String>? pendingPackageIds,
    bool? hasLoadedOnce,
    String? errorMessage,
    bool clearErrorMessage = false,
  }) {
    return ContentFavoritesState(
      favorites: favorites ?? this.favorites,
      pendingPackageIds: pendingPackageIds ?? this.pendingPackageIds,
      hasLoadedOnce: hasLoadedOnce ?? this.hasLoadedOnce,
      errorMessage: clearErrorMessage
          ? null
          : (errorMessage ?? this.errorMessage),
    );
  }
}

/// Owns the guardian's bookmarks so a star toggle stays responsive and never
/// leaves the two views out of sync.
class ContentFavoritesController extends AsyncNotifier<ContentFavoritesState> {
  late final ContentManagementApi _api;

  @override
  Future<ContentFavoritesState> build() async {
    _api = ref.read(contentManagementApiProvider);
    return _load(const ContentFavoritesState.initial());
  }

  Future<void> refresh() async {
    final current = state.value ?? const ContentFavoritesState.initial();
    state = await AsyncValue.guard(() => _load(current));
  }

  /// Adds or removes the bookmark depending on the current state. The star is
  /// optimistic so tapping feels immediate; a failure rolls the change back and
  /// surfaces the reason.
  Future<void> toggle(String packageId) async {
    final current = state.value;
    if (current == null || current.isPending(packageId)) {
      return;
    }
    final wasFavorite = current.isFavorite(packageId);
    final pending = {...current.pendingPackageIds, packageId};
    state = AsyncData(
      current.copyWith(
        pendingPackageIds: pending,
        clearErrorMessage: true,
      ),
    );
    try {
      if (wasFavorite) {
        await _api.removeFavorite(packageId);
      } else {
        await _api.addFavorite(packageId);
      }
      final next = state.value ?? current;
      final updated = wasFavorite
          ? next.favorites
                .where((favorite) => favorite.packageId != packageId)
                .toList(growable: false)
          : next.favorites;
      state = AsyncData(
        next.copyWith(
          favorites: updated,
          pendingPackageIds: {...next.pendingPackageIds}..remove(packageId),
          clearErrorMessage: true,
        ),
      );
      // Reconcile with the platform so an optimistic add picks up the exact
      // title, version, and availability the server recorded.
      await _reconcile();
    } on Object catch (error) {
      final latest = state.value ?? current;
      state = AsyncData(
        latest.copyWith(
          pendingPackageIds: {...latest.pendingPackageIds}..remove(packageId),
          errorMessage: _messageFor(error),
        ),
      );
    }
  }

  Future<ContentFavoritesState> _load(ContentFavoritesState current) async {
    final favorites = await _api.fetchFavorites();
    return current.copyWith(
      favorites: favorites,
      hasLoadedOnce: true,
      clearErrorMessage: true,
    );
  }

  Future<void> _reconcile() async {
    final latest = state.value;
    if (latest == null) {
      return;
    }
    try {
      final favorites = await _api.fetchFavorites();
      state = AsyncData(latest.copyWith(favorites: favorites));
    } on Object {
      // A failed reconcile keeps the optimistic result; the next refresh or
      // page open will resync. Only the mutation itself must report errors.
    }
  }

  String _messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final contentFavoritesControllerProvider =
    AsyncNotifierProvider<ContentFavoritesController, ContentFavoritesState>(
      ContentFavoritesController.new,
    );
