import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import '../../../core/error/app_exception.dart';
import '../domain/content_package.dart';

/// Persists downloaded content under application support storage.
///
/// The index is written through a temporary file and atomic rename so an
/// interrupted upgrade cannot leave the guardian with a half-written catalog.
class ContentLibraryStore {
  ContentLibraryStore({
    Future<Directory> Function()? supportDirectory,
    Future<Directory> Function()? temporaryDirectory,
    Dio? downloadClient,
  }) : _supportDirectory = supportDirectory ?? getApplicationSupportDirectory,
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory,
       _downloadClient =
           downloadClient ??
           Dio(
             BaseOptions(
               connectTimeout: const Duration(seconds: 20),
               receiveTimeout: const Duration(minutes: 10),
               sendTimeout: const Duration(seconds: 20),
             ),
           );

  static const _rootName = 'content_library';
  static const _indexFileName = 'index.json';
  final Future<Directory> Function() _supportDirectory;
  final Future<Directory> Function() _temporaryDirectory;
  final Dio _downloadClient;

  /// Loads the local index, returning an empty catalog when none exists yet.
  Future<ContentLibraryIndex> loadIndex() async {
    final indexFile = await _indexFile();
    if (!await indexFile.exists()) {
      return const ContentLibraryIndex.empty();
    }
    try {
      final decoded = jsonDecode(await indexFile.readAsString());
      if (decoded is! Map) {
        return const ContentLibraryIndex.empty();
      }
      return ContentLibraryIndex.fromJson(Map<String, Object?>.from(decoded));
    } on Object {
      // A corrupt index is recoverable: the catalog can be fetched again and
      // the content directory can be rebuilt from verified downloads.
      return const ContentLibraryIndex.empty();
    }
  }

  /// Writes the index atomically after every successful mutation.
  Future<void> writeIndex(
    ContentLibraryIndex index, {
    Directory? supportDirectory,
  }) async {
    final root = supportDirectory ?? await _contentRoot();
    await root.create(recursive: true);
    final target = File(path.join(root.path, _indexFileName));
    final staging = File(
      path.join(root.path, '._index-${DateTime.now().microsecondsSinceEpoch}'),
    );
    await staging.writeAsString(jsonEncode(index.toJson()), flush: true);
    try {
      await staging.rename(target.path);
    } on FileSystemException {
      // Android does not replace an existing target on every filesystem.
      if (await target.exists()) {
        await target.delete();
      }
      await staging.rename(target.path);
    }
  }

  /// Downloads, verifies, and activates a package.
  ///
  /// The temporary file is deleted on every failure path, including a SHA-256
  /// mismatch, so a failed download cannot be mistaken for a usable package.
  Future<DownloadedContent> downloadPackage({
    required ContentPackage package,
    required ContentDownloadSpec spec,
    required void Function(double progress) onProgress,
    CancelToken? cancelToken,
  }) async {
    _validateDownloadSpec(spec);
    final root = await _contentRoot();
    final packageDirectory = Directory(path.join(root.path, 'packages'));
    await packageDirectory.create(recursive: true);

    final temporaryFile = File(
      path.join(
        (await _temporaryDirectory()).path,
        'sprout-content-${package.packageId}-'
        '${DateTime.now().microsecondsSinceEpoch}.part',
      ),
    );
    final relativePath = path.join(
      'packages',
      '${_safeSegment(package.packageId)}-${package.packageVersion}.bin',
    );
    final destination = File(path.join(root.path, relativePath));

    try {
      await _downloadClient.download(
        spec.downloadUrl,
        temporaryFile.path,
        deleteOnError: true,
        cancelToken: cancelToken,
        options: Options(
          responseType: ResponseType.stream,
          headers: const {'Accept': 'application/octet-stream'},
        ),
        onReceiveProgress: (receivedBytes, totalBytes) {
          final expectedBytes = totalBytes > 0 ? totalBytes : spec.sizeBytes;
          final progress = expectedBytes <= 0
              ? 0.0
              : (receivedBytes / expectedBytes).clamp(0.0, 1.0);
          onProgress(progress);
        },
      );
      _throwIfCancelled(cancelToken);
      await verifySha256(temporaryFile, spec.sha256);
      _throwIfCancelled(cancelToken);
      if (await destination.exists()) {
        await destination.delete();
      }
      await temporaryFile.rename(destination.path);
      final downloaded = DownloadedContent(
        packageId: package.packageId,
        packageVersion: package.packageVersion,
        sha256: spec.sha256,
        relativePath: relativePath,
        downloadedAt: DateTime.now().toUtc(),
      );
      await _deleteOtherVersions(root, package.packageId, keep: relativePath);
      onProgress(1);
      return downloaded;
    } on Object catch (error) {
      if (await temporaryFile.exists()) {
        await temporaryFile.delete();
      }
      if (error is DioException) {
        throw AppException(
          kind: AppErrorKind.network,
          message: '内容下载没有完成，请检查网络后重试',
          retryable: true,
          cause: error,
        );
      }
      rethrow;
    }
  }

  /// Deletes one downloaded package and its local file.
  Future<void> deletePackage(
    ContentLibraryIndex index,
    String packageId,
  ) async {
    final downloaded = index.packages[packageId];
    if (downloaded == null) {
      return;
    }
    final root = await _contentRoot();
    final file = File(path.join(root.path, downloaded.relativePath));
    if (await file.exists()) {
      await file.delete();
    }
  }

  /// Removes files for packages withdrawn by the catalog.
  Future<void> deleteWithdrawnFiles(
    ContentLibraryIndex index,
    Iterable<String> packageIds,
  ) async {
    for (final packageId in packageIds) {
      await deletePackage(index, packageId);
    }
  }

  @visibleForTesting
  static Future<void> verifySha256(File file, String expectedSha256) async {
    final digest = await sha256.bind(file.openRead()).first;
    final actual = digest.toString().toLowerCase();
    if (actual != expectedSha256.toLowerCase()) {
      if (await file.exists()) {
        await file.delete();
      }
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '内容校验没有通过，请稍后重试',
        retryable: true,
      );
    }
  }

  Future<File> _indexFile() async {
    final root = await _contentRoot();
    return File(path.join(root.path, _indexFileName));
  }

  Future<Directory> _contentRoot() async {
    return Directory(path.join((await _supportDirectory()).path, _rootName));
  }

  Future<void> _deleteOtherVersions(
    Directory root,
    String packageId, {
    required String keep,
  }) async {
    final packagesDirectory = Directory(path.join(root.path, 'packages'));
    if (!await packagesDirectory.exists()) {
      return;
    }
    final prefix = '${_safeSegment(packageId)}-';
    await for (final entity in packagesDirectory.list(followLinks: false)) {
      if (entity is! File) {
        continue;
      }
      final relative = path.relative(entity.path, from: root.path);
      if (relative == keep) {
        continue;
      }
      if (path.basename(entity.path).startsWith(prefix)) {
        try {
          await entity.delete();
        } on FileSystemException {
          // A later catalog sync will retry cleanup.
        }
      }
    }
  }

  void _validateDownloadSpec(ContentDownloadSpec spec) {
    final uri = Uri.tryParse(spec.downloadUrl);
    if (uri == null || uri.scheme != 'https' || uri.host.isEmpty) {
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '内容地址暂时不可用，请稍后重试',
        retryable: true,
      );
    }
    if (!RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(spec.sha256)) {
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '内容校验信息不完整，请稍后重试',
        retryable: true,
      );
    }
  }

  void _throwIfCancelled(CancelToken? cancelToken) {
    final cancelError = cancelToken?.cancelError;
    if (cancelError != null) {
      throw cancelError;
    }
  }

  String _safeSegment(String value) {
    final sanitized = value.replaceAll(RegExp(r'[^0-9A-Za-z._-]'), '_');
    return sanitized.isEmpty ? 'unknown' : sanitized;
  }
}
