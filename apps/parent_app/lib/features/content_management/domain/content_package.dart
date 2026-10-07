/// Category shown by the guardian content library.
enum ContentCategory {
  story('story', '故事'),
  nurseryRhyme('nursery_rhyme', '儿歌'),
  poetry('poetry', '古诗'),
  english('english', '英语'),
  encyclopedia('encyclopedia', '百科'),
  bedtime('bedtime', '睡前');

  const ContentCategory(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static ContentCategory? fromApiValue(String value) {
    for (final category in ContentCategory.values) {
      if (category.apiValue == value) {
        return category;
      }
    }
    return null;
  }
}

/// Age tier used to keep content suitable for the selected child.
enum ContentAgeTier {
  age3to4('age_3_4', '3～4 岁'),
  age5to6('age_5_6', '5～6 岁'),
  age7to8('age_7_8', '7～8 岁');

  const ContentAgeTier(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static ContentAgeTier? fromApiValue(String value) {
    for (final tier in ContentAgeTier.values) {
      if (tier.apiValue == value) {
        return tier;
      }
    }
    return null;
  }
}

/// Catalog item before local download state is merged in.
class ContentPackage {
  const ContentPackage({
    required this.packageId,
    required this.packageVersion,
    required this.title,
    required this.category,
    required this.ageTiers,
    required this.assetKey,
    required this.sha256,
    required this.sizeBytes,
    required this.publishedAt,
  });

  final String packageId;
  final int packageVersion;
  final String title;
  final ContentCategory category;
  final List<ContentAgeTier> ageTiers;
  final String assetKey;
  final String sha256;
  final int sizeBytes;
  final DateTime publishedAt;

  factory ContentPackage.fromJson(Map<String, Object?> json) {
    final category = ContentCategory.fromApiValue(
      _requiredString(json['category'], 'category'),
    );
    if (category == null) {
      throw const FormatException('unknown content category');
    }
    final ageTiers = _stringList(json['age_tiers'])
        .map(ContentAgeTier.fromApiValue)
        .whereType<ContentAgeTier>()
        .toList(growable: false);
    if (ageTiers.isEmpty) {
      throw const FormatException('missing content age tiers');
    }
    return ContentPackage(
      packageId: _requiredString(json['package_id'], 'package_id'),
      packageVersion: _requiredInt(
        json['package_version'],
        'package_version',
      ),
      title: _requiredString(json['title'], 'title'),
      category: category,
      ageTiers: ageTiers,
      assetKey: _requiredString(json['asset_key'], 'asset_key'),
      sha256: _requiredSha256(json['sha256']),
      sizeBytes: _requiredInt(json['size_bytes'], 'size_bytes'),
      publishedAt: _requiredDateTime(json['published_at'], 'published_at'),
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'package_id': packageId,
      'package_version': packageVersion,
      'title': title,
      'category': category.apiValue,
      'age_tiers': ageTiers
          .map((tier) => tier.apiValue)
          .toList(growable: false),
      'asset_key': assetKey,
      'sha256': sha256,
      'size_bytes': sizeBytes,
      'published_at': publishedAt.toUtc().toIso8601String(),
    };
  }
}

/// One catalog snapshot returned for an incremental sync request.
class ContentCatalog {
  const ContentCatalog({
    required this.catalogRevision,
    required this.packages,
    required this.withdrawnPackageIds,
  });

  final int catalogRevision;
  final List<ContentPackage> packages;
  final List<String> withdrawnPackageIds;

  factory ContentCatalog.fromJson(Map<String, Object?> json) {
    final packagesValue = json['packages'];
    if (packagesValue is! List) {
      throw const FormatException('missing content packages');
    }
    return ContentCatalog(
      catalogRevision: _requiredInt(
        json['catalog_revision'],
        'catalog_revision',
      ),
      packages: packagesValue
          .whereType<Map>()
          .map(
            (item) => ContentPackage.fromJson(Map<String, Object?>.from(item)),
          )
          .toList(growable: false),
      withdrawnPackageIds: _stringList(json['withdrawn_package_ids']),
    );
  }
}

/// Download address and integrity metadata fetched for one package.
class ContentDownloadSpec {
  const ContentDownloadSpec({
    required this.downloadUrl,
    required this.sha256,
    required this.sizeBytes,
  });

  final String downloadUrl;
  final String sha256;
  final int sizeBytes;

  factory ContentDownloadSpec.fromJson(Map<String, Object?> json) {
    return ContentDownloadSpec(
      downloadUrl: _requiredString(json['download_url'], 'download_url'),
      sha256: _requiredSha256(json['sha256']),
      sizeBytes: _requiredInt(json['size_bytes'], 'size_bytes'),
    );
  }
}

/// Local lifecycle of a package that has entered the guardian library.
enum ContentDownloadStatus {
  notDownloaded,
  downloading,
  downloaded,
  updateAvailable,
  withdrawn,
}

/// Catalog metadata combined with this device's local download state.
class ContentLibraryEntry {
  const ContentLibraryEntry({
    required this.package,
    required this.status,
    required this.downloadProgress,
    required this.errorMessage,
  });

  final ContentPackage package;
  final ContentDownloadStatus status;
  final double downloadProgress;
  final String? errorMessage;

  bool get canDownload =>
      status == ContentDownloadStatus.notDownloaded ||
      status == ContentDownloadStatus.updateAvailable;

  bool get canDelete =>
      status == ContentDownloadStatus.downloaded ||
      status == ContentDownloadStatus.updateAvailable;

  ContentLibraryEntry copyWith({
    ContentPackage? package,
    ContentDownloadStatus? status,
    double? downloadProgress,
    String? errorMessage,
    bool clearErrorMessage = false,
  }) {
    return ContentLibraryEntry(
      package: package ?? this.package,
      status: status ?? this.status,
      downloadProgress: downloadProgress ?? this.downloadProgress,
      errorMessage: clearErrorMessage
          ? null
          : (errorMessage ?? this.errorMessage),
    );
  }
}

/// Persisted index for one downloaded package.
class DownloadedContent {
  const DownloadedContent({
    required this.packageId,
    required this.packageVersion,
    required this.sha256,
    required this.relativePath,
    required this.downloadedAt,
  });

  final String packageId;
  final int packageVersion;
  final String sha256;
  final String relativePath;
  final DateTime downloadedAt;

  factory DownloadedContent.fromJson(Map<String, Object?> json) {
    return DownloadedContent(
      packageId: _requiredString(json['package_id'], 'package_id'),
      packageVersion: _requiredInt(
        json['package_version'],
        'package_version',
      ),
      sha256: _requiredSha256(json['sha256']),
      relativePath: _requiredString(json['relative_path'], 'relative_path'),
      downloadedAt: _requiredDateTime(json['downloaded_at'], 'downloaded_at'),
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'package_id': packageId,
      'package_version': packageVersion,
      'sha256': sha256,
      'relative_path': relativePath,
      'downloaded_at': downloadedAt.toUtc().toIso8601String(),
    };
  }
}

/// Complete local index with the last successful catalog revision.
class ContentLibraryIndex {
  const ContentLibraryIndex({
    required this.catalogRevision,
    required this.packages,
  });

  const ContentLibraryIndex.empty() : catalogRevision = 0, packages = const {};

  final int catalogRevision;
  final Map<String, DownloadedContent> packages;

  factory ContentLibraryIndex.fromJson(Map<String, Object?> json) {
    final packagesValue = json['packages'];
    final packages = <String, DownloadedContent>{};
    if (packagesValue is Map) {
      for (final entry in packagesValue.entries) {
        final key = entry.key;
        final value = entry.value;
        if (key is String && value is Map) {
          packages[key] = DownloadedContent.fromJson(
            Map<String, Object?>.from(value),
          );
        }
      }
    }
    return ContentLibraryIndex(
      catalogRevision: _asInt(json['catalog_revision']),
      packages: packages,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'catalog_revision': catalogRevision,
      'packages': <String, Object?>{
        for (final entry in packages.entries) entry.key: entry.value.toJson(),
      },
    };
  }
}

/// Human-readable file size without exposing storage details.
String formatContentSize(int sizeBytes) {
  if (sizeBytes < 1024) {
    return '$sizeBytes B';
  }
  final kilobytes = sizeBytes / 1024;
  if (kilobytes < 1024) {
    return '${kilobytes.toStringAsFixed(kilobytes >= 100 ? 0 : 1)} KB';
  }
  final megabytes = kilobytes / 1024;
  return '${megabytes.toStringAsFixed(megabytes >= 100 ? 0 : 1)} MB';
}

String _requiredString(Object? value, String field) {
  if (value is String && value.isNotEmpty) {
    return value;
  }
  throw FormatException('missing $field');
}

String _requiredSha256(Object? value) {
  final sha256 = _requiredString(value, 'sha256').toLowerCase();
  if (!RegExp(r'^[0-9a-f]{64}$').hasMatch(sha256)) {
    throw const FormatException('invalid sha256');
  }
  return sha256;
}

int _requiredInt(Object? value, String field) {
  if (value is num && value >= 0) {
    return value.toInt();
  }
  throw FormatException('missing $field');
}

DateTime _requiredDateTime(Object? value, String field) {
  if (value is String) {
    final parsed = DateTime.tryParse(value);
    if (parsed != null) {
      return parsed.toUtc();
    }
  }
  throw FormatException('missing $field');
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value.whereType<String>().toList(growable: false);
}

int _asInt(Object? value) {
  return value is num ? value.toInt() : 0;
}
