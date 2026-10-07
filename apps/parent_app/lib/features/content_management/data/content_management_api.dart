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
}
