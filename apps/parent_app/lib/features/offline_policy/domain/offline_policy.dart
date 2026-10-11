/// Platform-wide device offline download policy.
///
/// One policy applies to every bound device. The guardian reads it to see how
/// much content a device may cache for offline use, and the revision tells the
/// client which version is currently published.
class OfflinePolicy {
  const OfflinePolicy({
    required this.offlineEnabled,
    required this.maxCachedPackages,
    required this.maxCacheBytes,
    required this.cacheTtlHours,
    required this.downloadOverWifiOnly,
    required this.revision,
  });

  final bool offlineEnabled;
  final int maxCachedPackages;
  final int maxCacheBytes;
  final int cacheTtlHours;
  final bool downloadOverWifiOnly;
  final int revision;

  /// Cache cap expressed in whole megabytes for display.
  int get maxCacheMegabytes => maxCacheBytes ~/ (1024 * 1024);

  factory OfflinePolicy.fromJson(Map<String, Object?> json) {
    return OfflinePolicy(
      offlineEnabled: json['offline_enabled'] != false,
      maxCachedPackages: _asInt(json['max_cached_packages'], 20),
      maxCacheBytes: _asInt(json['max_cache_bytes'], 512 * 1024 * 1024),
      cacheTtlHours: _asInt(json['cache_ttl_hours'], 720),
      downloadOverWifiOnly: json['download_over_wifi_only'] != false,
      revision: _asInt(json['revision'], 0),
    );
  }
}

int _asInt(Object? value, int fallback) =>
    value is num ? value.toInt() : fallback;
