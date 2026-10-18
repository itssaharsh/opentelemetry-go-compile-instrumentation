// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/data"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	unzippedPkgDir  = "pkg"
	unzippedInstDir = "instrumentation"

	pkgTempDir  = "pkg_temp"
	instTempDir = "instrumentation_temp"
)

func normalizePath(name string) string {
	cleanName := filepath.ToSlash(filepath.Clean(name))

	replacements := map[string]string{
		pkgTempDir:  unzippedPkgDir,
		instTempDir: unzippedInstDir,
	}

	for from, to := range replacements {
		if cleanName == from {
			return to
		}
		if strings.HasPrefix(cleanName, from+"/") {
			return to + strings.TrimPrefix(cleanName, from)
		}
	}

	return cleanName
}

func extract(tarReader *tar.Reader, header *tar.Header, targetPath string) error {
	fileInfo := header.FileInfo()
	switch header.Typeflag {
	case tar.TypeDir:
		err := os.MkdirAll(targetPath, fileInfo.Mode())
		if err != nil {
			return ex.Wrap(err)
		}

	case tar.TypeReg:
		file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR,
			fileInfo.Mode())
		if err != nil {
			return ex.Wrap(err)
		}

		_, err = io.CopyN(file, tarReader, header.Size)
		if err != nil {
			_ = file.Close()
			return ex.Wrap(err)
		}
		err = file.Close()
		if err != nil {
			return ex.Wrap(err)
		}
	default:
		return ex.Newf("unsupported file type: %c in %s",
			header.Typeflag, header.Name)
	}
	return nil
}

func extractGZip(bundleReader io.Reader, targetDir string) (retErr error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return ex.Wrap(err)
	}

	gzReader, gzErr := gzip.NewReader(bundleReader)
	if gzErr != nil {
		return ex.Wrap(gzErr)
	}
	defer func() {
		if closeErr := gzReader.Close(); closeErr != nil {
			retErr = errors.Join(retErr, ex.Wrap(closeErr))
		}
	}()

	// The build temp directory is kept between builds, so these directories
	// may still hold the bundle of an earlier otelc. Extracting over them
	// would leave behind any file that bundle had and this one does not, and
	// the leftover would be compiled alongside the new files. Clear them only
	// once the bundle has been opened, so that a bundle that cannot be read
	// does not take a working extraction with it. The rest of the directory,
	// such as the state snapshots and the build cache, is left alone.
	for _, dir := range []string{unzippedPkgDir, unzippedInstDir} {
		if err := os.RemoveAll(filepath.Join(targetDir, dir)); err != nil {
			return ex.Wrapf(err, "removing %s extracted by a previous build", dir)
		}
	}

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ex.Wrap(err)
		}

		// Skip AppleDouble files (._filename) and other hidden files
		if strings.HasPrefix(filepath.Base(header.Name), ".") {
			continue
		}

		// Normalize path to Unix style for consistent string operations
		cleanName := normalizePath(header.Name)

		// Sanitize the file path to prevent Zip Slip vulnerability
		if cleanName == "." || cleanName == ".." ||
			strings.HasPrefix(cleanName, "..") {
			continue
		}

		// Ensure the resolved path is within the target directory
		targetPath := filepath.Join(targetDir, cleanName)
		resolvedPath, err := filepath.EvalSymlinks(targetPath)
		if err != nil {
			// If symlink evaluation fails, use the original path
			resolvedPath = targetPath
		}

		// Check if the resolved path is within the target directory
		relPath, err := filepath.Rel(targetDir, resolvedPath)
		if err != nil || strings.HasPrefix(relPath, "..") ||
			filepath.IsAbs(relPath) {
			continue // Skip files that would be extracted outside target dir
		}
		err = extract(tarReader, header, filepath.Join(targetDir, relPath))
		if err != nil {
			return err
		}
	}

	return nil
}

func extractOtelcBundle() error {
	// Extract the instrumentation code to the build temp directory
	// for future instrumentation phase
	return extractGZip(data.GetBundleReader(), util.GetBuildTempDir())
}
