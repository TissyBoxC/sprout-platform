import '../../../core/network/api_client.dart';
import '../domain/content_package.dart';

/// Catalog and download contracts owned by the content library.
abstract interface class ContentManagementApi {
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  });

  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId);

  /// Lists the guardian's bookmarked packages, newest bookmark first.
  Future<List<ContentFavorite>> fetchFavorites();

  /// Bookmarks one package. A repeat call is idempotent on the platform.
  Future<void> addFavorite(String packageId);

  /// Removes one bookmark. A missing bookmark is treated as success.
  Future<void> removeFavorite(String packageId);

  /// Lists playback history across the guardian's devices, newest first.
  ///
  /// Passing [deviceId] narrows the result to one owned device.
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId});

  /// Lists the published theme packs the guardian can browse.
  Future<List<ContentThemePack>> fetchThemePacks();

  /// Lists platform-ranked content suggestions for the guardian's family.
  ///
  /// Passing [ageTier] narrows candidates to one child's age band; the
  /// platform already excludes what the family favourited or played.
  Future<List<ContentRecommendation>> fetchRecommendations({
    ContentAgeTier? ageTier,
    int limit = 8,
    List<String> excludePackageIds = const <String>[],
  });
}

/// Production implementation backed by the authenticated API client.
class HttpContentManagementApi implements ContentManagementApi {
  const HttpContentManagementApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  }) async {
    final query = <String, String>{
      'since_revision': sinceRevision.toString(),
      if (ageTier != null) 'age_tier': ageTier.apiValue,
      if (category != null) 'category': category.apiValue,
    };
    final response = await _apiClient.get(
      '/api/v1/content/catalog',
      queryParameters: query,
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing catalog data');
    }
    return ContentCatalog.fromJson(Map<String, Object?>.from(data));
  }

  @override
  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId) async {
    final response = await _apiClient.get(
      '/api/v1/content/packages/$packageId/download',
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing download data');
    }
    final envelope = Map<String, Object?>.from(data);
    final download = envelope['download'];
    if (download is! Map) {
      throw const FormatException('missing download data');
    }
    return ContentDownloadSpec.fromJson(Map<String, Object?>.from(download));
  }

  @override
  Future<List<ContentFavorite>> fetchFavorites() async {
    final response = await _apiClient.get('/api/v1/content/favorites');
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing favorites data');
    }
    final favorites = data['favorites'];
    if (favorites is! List) {
      return const <ContentFavorite>[];
    }
    return favorites
        .whereType<Map>()
        .map(
          (item) => ContentFavorite.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<void> addFavorite(String packageId) async {
    await _apiClient.put('/api/v1/content/favorites/$packageId');
  }

  @override
  Future<void> removeFavorite(String packageId) async {
    await _apiClient.delete('/api/v1/content/favorites/$packageId');
  }

  @override
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId}) async {
    final response = await _apiClient.get(
      '/api/v1/content/play-history',
      queryParameters: <String, String>{
        if (deviceId != null && deviceId.isNotEmpty) 'device_id': deviceId,
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing play history data');
    }
    final history = data['history'];
    if (history is! List) {
      return const <ContentPlayRecord>[];
    }
    return history
        .whereType<Map>()
        .map(
          (item) => ContentPlayRecord.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async {
    final response = await _apiClient.get('/api/v1/content/theme-packs');
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing theme pack data');
    }
    final packs = data['theme_packs'];
    if (packs is! List) {
      return const <ContentThemePack>[];
    }
    return packs
        .whereType<Map>()
        .map(
          (item) => ContentThemePack.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<List<ContentRecommendation>> fetchRecommendations({
    ContentAgeTier? ageTier,
    int limit = 8,
    List<String> excludePackageIds = const <String>[],
  }) async {
    final response = await _apiClient.get(
      '/api/v1/content/recommendations',
      queryParameters: <String, String>{
        if (ageTier != null) 'age_tier': ageTier.apiValue,
        'limit': limit.toString(),
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing recommendation data');
    }
    final recommendations = data['recommendations'];
    if (recommendations is! List) {
      return const <ContentRecommendation>[];
    }
    final excluded = excludePackageIds.toSet();
    return recommendations
        .whereType<Map>()
        .map(
          (item) =>
              ContentRecommendation.fromJson(Map<String, Object?>.from(item)),
        )
        .where((item) => !excluded.contains(item.packageId))
        .toList(growable: false);
  }
}
