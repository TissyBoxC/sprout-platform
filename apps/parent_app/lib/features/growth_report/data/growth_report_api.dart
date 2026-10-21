import '../../../core/network/api_client.dart';
import '../domain/growth_report.dart';

/// Guardian-facing access to a child's growth reports.
///
/// Every call is scoped to the signed-in family, so the client never sends an
/// owner id. A report contains aggregates only, which is why the whole list can
/// be fetched at once instead of paged like raw records.
abstract interface class GrowthReportApi {
  /// Lists a child's reports newest first, optionally narrowed to one period.
  Future<List<GrowthReport>> fetchReports(
    String childId, {
    GrowthReportPeriod? period,
  });

  /// Reads one report by id.
  Future<GrowthReport> fetchReport({
    required String childId,
    required String reportId,
  });
}

/// Production implementation backed by the authenticated API client.
///
/// The platform checks that [familyId] belongs to the signed-in guardian, so the
/// client passes the id the session reported rather than inventing one.
class HttpGrowthReportApi implements GrowthReportApi {
  const HttpGrowthReportApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String _basePath(String childId) {
    return '/api/v1/families/${Uri.encodeComponent(_familyId)}'
        '/children/${Uri.encodeComponent(childId)}/growth-reports';
  }

  @override
  Future<List<GrowthReport>> fetchReports(
    String childId, {
    GrowthReportPeriod? period,
  }) async {
    final response = await _apiClient.get(
      _basePath(childId),
      queryParameters: period == null
          ? null
          : <String, String>{'period_type': period.apiValue},
    );
    final data = response['data'];
    final reports = data is Map ? data['reports'] : null;
    if (reports is! List) {
      return const <GrowthReport>[];
    }
    return reports
        .whereType<Map>()
        .map((report) => GrowthReport.fromJson(report.cast<String, dynamic>()))
        .toList(growable: false);
  }

  @override
  Future<GrowthReport> fetchReport({
    required String childId,
    required String reportId,
  }) async {
    final response = await _apiClient.get(
      '${_basePath(childId)}/${Uri.encodeComponent(reportId)}',
    );
    final data = response['data'];
    final report = data is Map ? data['report'] : null;
    if (report is! Map) {
      throw const FormatException('growth report missing from response');
    }
    return GrowthReport.fromJson(report.cast<String, dynamic>());
  }
}
