package portablelauncher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const bundleStagingMarker = ".staging-"

type bundleFileWriter func(string, []byte) error

// WriteBundle validates and atomically publishes one scrubbed conformance
// bundle. Raw launcher capture is intentionally not part of the bundle.
func WriteBundle(destination string, result Result, capture *Capture) error {
	return writeBundle(destination, result, capture, writePrivateBundleFile)
}

func writeBundle(destination string, result Result, capture *Capture, writeFile bundleFileWriter) error {
	if capture == nil {
		return fmt.Errorf("write conformance bundle: capture is nil")
	}
	if writeFile == nil {
		return fmt.Errorf("write conformance bundle: file writer is nil")
	}

	resultJSON, err := marshalBundleJSON(result)
	if err != nil {
		return fmt.Errorf("write conformance bundle: marshal result: %w", err)
	}
	index := capture.Index()
	indexJSON, err := marshalBundleJSON(index)
	if err != nil {
		return fmt.Errorf("write conformance bundle: marshal evidence index: %w", err)
	}
	blobs := make(map[string][]byte, len(index.Blobs))
	for _, entry := range index.Blobs {
		if blob, ok := capture.Blob(entry.Reference); ok {
			blobs[entry.Reference] = blob
		}
	}
	readBlob := func(reference string) ([]byte, error) {
		blob, ok := blobs[reference]
		if !ok {
			return nil, fmt.Errorf("blob %s not found", reference)
		}
		return append([]byte(nil), blob...), nil
	}
	if _, err := Validate(ValidationInput{ResultJSON: resultJSON, IndexJSON: indexJSON, ReadBlob: readBlob}); err != nil {
		return fmt.Errorf("write conformance bundle: validate: %w", err)
	}

	destination = filepath.Clean(destination)
	if destination == "." || destination == string(filepath.Separator) {
		return fmt.Errorf("write conformance bundle: destination must name a new directory")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("write conformance bundle: destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("write conformance bundle: inspect destination: %w", err)
	}
	parent := filepath.Dir(destination)
	if info, err := os.Stat(parent); err != nil {
		return fmt.Errorf("write conformance bundle: inspect destination parent: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("write conformance bundle: destination parent is not a directory")
	}

	stage, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+bundleStagingMarker)
	if err != nil {
		return fmt.Errorf("write conformance bundle: create staging directory: %w", err)
	}
	defer func() {
		if stage != "" {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.Chmod(stage, 0o700); err != nil {
		return fmt.Errorf("write conformance bundle: protect staging directory: %w", err)
	}
	blobDir := filepath.Join(stage, "blobs")
	if err := os.Mkdir(blobDir, 0o700); err != nil {
		return fmt.Errorf("write conformance bundle: create blob directory: %w", err)
	}

	files := []struct {
		path string
		data []byte
	}{
		{path: filepath.Join(stage, "result.json"), data: resultJSON},
		{path: filepath.Join(stage, "evidence-index.json"), data: indexJSON},
	}
	for _, entry := range index.Blobs {
		digest := strings.TrimPrefix(entry.Reference, "blob:sha256:")
		files = append(files, struct {
			path string
			data []byte
		}{path: filepath.Join(blobDir, digest+".json"), data: blobs[entry.Reference]})
	}
	for _, file := range files {
		if err := writeFile(file.path, file.data); err != nil {
			return fmt.Errorf("write conformance bundle: write %s: %w", filepath.Base(file.path), err)
		}
	}

	if err := publishBundle(stage, destination); err != nil {
		return fmt.Errorf("write conformance bundle: publish: %w", err)
	}
	stage = ""
	return nil
}

func marshalBundleJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writePrivateBundleFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	remaining := data
	for len(remaining) != 0 {
		written, writeErr := file.Write(remaining)
		if writeErr != nil {
			_ = file.Close()
			return writeErr
		}
		if written == 0 {
			_ = file.Close()
			return errors.New("short write")
		}
		remaining = remaining[written:]
	}
	return file.Close()
}
