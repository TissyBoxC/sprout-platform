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
  ContentFavoritesState _latest = const ContentFavoritesState.initial();

  @override
  Future<ContentFavoritesState> build() async {
    _api = ref.read(contentManagementApiProvider);
    _latest = await _load(const ContentFavoritesState.initial());
    return _latest;
  }

  Future<void> refresh() async {
    final current = _latest;
    final result = await AsyncValue.guard(() => _load(current));
    if (result case AsyncData(:final value)) {
      _latest = value;
    }
    state = result;
  }

  /// Adds or removes the bookmark depending on the current state. The star is
  /// optimistic so tapping feels immediate; a failure rolls the change back and
  /// surfaces the reason.
  Future<void> toggle(String packageId) async {
    final current = _latest;
    if (current.isPending(packageId)) {
      return;
    }
    final wasFavorite = current.isFavorite(packageId);
    final pending = {...current.pendingPackageIds, packageId};
    _setState(
      current.copyWith(pendingPackageIds: pending, clearErrorMessage: true),
    );
    try {
      if (wasFavorite) {
        await _api.removeFavorite(packageId);
      } else {
        await _api.addFavorite(packageId);
      }
      final next = _latest;
      final updated = wasFavorite
          ? next.favorites
                .where((favorite) => favorite.packageId != packageId)
                .toList(growable: false)
          : next.favorites;
      _setState(
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
      final latest = _latest;
      final restoredFavorites = wasFavorite
          ? latest.favorites
          : latest.favorites
                .where((favorite) => favorite.packageId != packageId)
                .toList(growable: false);
      _setState(
        latest.copyWith(
          favorites: restoredFavorites,
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
    final latest = _latest;
    try {
      final favorites = await _api.fetchFavorites();
      // A mutation started after this reconcile request began must remain
      // authoritative. Only apply the snapshot once no toggle is in flight.
      if (_latest.pendingPackageIds.isEmpty) {
        _setState(latest.copyWith(favorites: favorites));
      }
    } on Object {
      // A failed reconcile keeps the optimistic result; the next refresh or
      // page open will resync. Only the mutation itself must report errors.
    }
  }

  void _setState(ContentFavoritesState next) {
    _latest = next;
    state = AsyncData(next);
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
