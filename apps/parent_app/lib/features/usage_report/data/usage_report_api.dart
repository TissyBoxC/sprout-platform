import '../../../core/network/api_client.dart';
import '../domain/usage_report.dart';

/// HTTP data source for the signed-in guardian usage report.
class UsageReportApi {
  const UsageReportApi(this._apiClient);

  final ApiClient _apiClient;

  Future<UsageReport> list({required int days}) async {
    final response = await _apiClient.get(
      '/api/v1/usage-reports',
      queryParameters: {'days': days.toString()},
    );
    final data = _requiredMap(response['data'], 'data');
    return UsageReport.fromJson(data);
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}
