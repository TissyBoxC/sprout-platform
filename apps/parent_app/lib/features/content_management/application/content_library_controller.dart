import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/content_management_api.dart';
import '../domain/content_package.dart';
import 'content_library_store.dart';

/// Catalog filters selected by the guardian.
class ContentLibraryFilter {
  const ContentLibraryFilter({this.category, this.ageTier});

  final ContentCategory? category;
  final ContentAgeTier? ageTier;

  ContentLibraryFilter copyWith({
    ContentCategory? category,
    ContentAgeTier? ageTier,
    bool clearCategory = false,
    bool clearAgeTier = false,
  }) {
    return ContentLibraryFilter(
      category: clearCategory ? null : (category ?? this.category),
      ageTier: clearAgeTier ? null : (ageTier ?? this.ageTier),
    );
  }
}

/// Complete state rendered by the content library page.
class ContentLibraryState {
  const ContentLibraryState({
    required this.entries,
    required this.catalogRevision,
    required this.filter,
    required this.hasLoadedOnce,
  });

  const ContentLibraryState.initial()
    : entries = const [],
      catalogRevision = 0,
      filter = const ContentLibraryFilter(),
      hasLoadedOnce = false;

  final List<ContentLibraryEntry> entries;
  final int catalogRevision;
  final ContentLibraryFilter filter;
  final bool hasLoadedOnce;

  bool get isEmpty => entries.isEmpty;

  ContentLibraryState copyWith({
    List<ContentLibraryEntry>? entries,
    int? catalogRevision,
    ContentLibraryFilter? filter,
    bool? hasLoadedOnce,
  }) {
    return ContentLibraryState(
      entries: entries ?? this.entries,
      catalogRevision: catalogRevision ?? this.catalogRevision,
      filter: filter ?? this.filter,
      hasLoadedOnce: hasLoadedOnce ?? this.hasLoadedOnce,
    );
  }
}

/// Owns incremental catalog sync, local index merge, and download actions.
class ContentLibraryController extends AsyncNotifier<ContentLibraryState> {
  late final ContentManagementApi _api;
  late final ContentLibraryStore _store;
  final Map<String, CancelToken> _activeDownloads = {};
  ContentLibraryIndex _index = const ContentLibraryIndex.empty();
  List<ContentPackage> _remoteCatalog = const [];

  @override
  Future<ContentLibraryState> build() async {
    _api = ref.read(contentManagementApiProvider);
    _store = ref.read(contentLibraryStoreProvider);
    _index = await _store.loadIndex();
    final state = await _syncCatalog(
      const ContentLibraryState.initial(),
      fullRefresh: true,
    );
    return state.copyWith(hasLoadedOnce: true);
  }

  Future<void> refresh() async {
    final current = state.value ?? const ContentLibraryState.initial();
    state = await AsyncValue.guard(() async {
      final next = await _syncCatalog(current, fullRefresh: false);
      return next.copyWith(hasLoadedOnce: true);
    });
  }

  Future<void> applyFilter(ContentLibraryFilter filter) async {
    final current = state.value ?? const ContentLibraryState.initial();
    final next = current.copyWith(filter: filter);
    state = AsyncData(next);
    state = await AsyncValue.guard(() async {
      final synced = await _syncCatalog(next, fullRefresh: true);
      return synced.copyWith(hasLoadedOnce: true);
    });
  }

  Future<void> download(String packageId) async {
    final current = state.value;
    final entry = _entryFor(current, packageId);
    if (current == null || entry == null || !entry.canDownload) {
      return;
    }
    final cancelToken = CancelToken();
    _activeDownloads[packageId] = cancelToken;
    _updateEntry(
      packageId,
      (item) => item.copyWith(
        status: ContentDownloadStatus.downloading,
        downloadProgress: 0,
        clearErrorMessage: true,
      ),
    );

    try {
      final spec = await _api.fetchDownloadSpec(packageId);
      final downloaded = await _store.downloadPackage(
        package: entry.package,
        spec: spec,
        cancelToken: cancelToken,
        onProgress: (progress) {
          _updateEntry(
            packageId,
            (item) => item.copyWith(downloadProgress: progress),
          );
        },
      );
      _index = ContentLibraryIndex(
        catalogRevision: _index.catalogRevision,
        packages: {..._index.packages, packageId: downloaded},
      );
      await _store.writeIndex(_index);
      _updateEntry(
        packageId,
        (item) => item.copyWith(
          status: ContentDownloadStatus.downloaded,
          downloadProgress: 1,
          clearErrorMessage: true,
        ),
      );
    } on Object catch (error) {
      if (error is DioException && CancelToken.isCancel(error)) {
        _updateEntry(
          packageId,
          (item) => item.copyWith(
            status: _statusFor(entry.package),
            downloadProgress: 0,
            clearErrorMessage: true,
          ),
        );
        return;
      }
      _updateEntry(
        packageId,
        (item) => item.copyWith(
          status: _statusFor(entry.package),
          downloadProgress: 0,
          errorMessage: _messageFor(error),
        ),
      );
    } finally {
      _activeDownloads.remove(packageId);
    }
  }

  void cancelDownload(String packageId) {
    _activeDownloads[packageId]?.cancel('guardian canceled content download');
  }

  Future<void> delete(String packageId) async {
    final current = state.value;
    if (current == null ||
        !current.entries.any((item) => item.package.packageId == packageId)) {
      return;
    }
    try {
      await _store.deletePackage(_index, packageId);
      _index = ContentLibraryIndex(
        catalogRevision: _index.catalogRevision,
        packages: {..._index.packages}..remove(packageId),
      );
      await _store.writeIndex(_index);
      _updateEntry(
        packageId,
        (item) => item.copyWith(
          status: ContentDownloadStatus.notDownloaded,
          downloadProgress: 0,
          clearErrorMessage: true,
        ),
      );
    } on Object catch (error) {
      _updateEntry(
        packageId,
        (item) => item.copyWith(errorMessage: _messageFor(error)),
      );
    }
  }

  Future<ContentLibraryState> _syncCatalog(
    ContentLibraryState current, {
    required bool fullRefresh,
  }) async {
    final sinceRevision = fullRefresh ? 0 : current.catalogRevision;
    final catalog = await _api.fetchCatalog(
      sinceRevision: sinceRevision,
      ageTier: current.filter.ageTier,
      category: current.filter.category,
    );

    final fetched = <String, ContentPackage>{
      for (final package in _remoteCatalog) package.packageId: package,
      for (final package in catalog.packages) package.packageId: package,
    };
    _remoteCatalog = fetched.values.toList(growable: false);

    final withdrawn = <String>{...catalog.withdrawnPackageIds};
    await _store.deleteWithdrawnFiles(_index, withdrawn);
    final packages = <String, DownloadedContent>{
      for (final entry in _index.packages.entries)
        if (!withdrawn.contains(entry.key)) entry.key: entry.value,
    };
    _index = ContentLibraryIndex(
      catalogRevision: catalog.catalogRevision,
      packages: packages,
    );
    await _store.writeIndex(_index);

    final visible = _remoteCatalog
        .where((package) {
          final filter = current.filter;
          return (filter.category == null ||
                  package.category == filter.category) &&
              (filter.ageTier == null ||
                  package.ageTiers.contains(filter.ageTier));
        })
        .map(
          (package) => ContentLibraryEntry(
            package: package,
            status: _statusFor(package),
            downloadProgress: _progressFor(package.packageId),
            errorMessage: null,
          ),
        )
        .toList(growable: false);
    visible.sort((left, right) {
      final category = left.package.category.index.compareTo(
        right.package.category.index,
      );
      if (category != 0) {
        return category;
      }
      return right.package.publishedAt.compareTo(left.package.publishedAt);
    });
    return current.copyWith(
      entries: visible,
      catalogRevision: catalog.catalogRevision,
    );
  }

  ContentDownloadStatus _statusFor(ContentPackage package) {
    final downloaded = _index.packages[package.packageId];
    if (downloaded == null) {
      return ContentDownloadStatus.notDownloaded;
    }
    if (downloaded.sha256 != package.sha256 ||
        downloaded.packageVersion != package.packageVersion) {
      return ContentDownloadStatus.updateAvailable;
    }
    return ContentDownloadStatus.downloaded;
  }

  double _progressFor(String packageId) {
    final entry = state.value?.entries
        .where((item) => item.package.packageId == packageId)
        .firstOrNull;
    return entry?.downloadProgress ?? 0;
  }

  ContentLibraryEntry? _entryFor(
    ContentLibraryState? current,
    String packageId,
  ) {
    return current?.entries
        .where((item) => item.package.packageId == packageId)
        .firstOrNull;
  }

  void _updateEntry(
    String packageId,
    ContentLibraryEntry Function(ContentLibraryEntry entry) update,
  ) {
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncData(
      current.copyWith(
        entries: current.entries
            .map(
              (entry) =>
                  entry.package.packageId == packageId ? update(entry) : entry,
            )
            .toList(growable: false),
      ),
    );
  }

  String _messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final contentManagementApiProvider = Provider<ContentManagementApi>((ref) {
  return HttpContentManagementApi(ref.read(apiClientProvider));
});

final contentLibraryStoreProvider = Provider<ContentLibraryStore>((ref) {
  return ContentLibraryStore();
});

final contentLibraryControllerProvider =
    AsyncNotifierProvider<ContentLibraryController, ContentLibraryState>(
      ContentLibraryController.new,
    );
