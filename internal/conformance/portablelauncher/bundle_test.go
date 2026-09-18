package portablelauncher

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteBundleDeterministicLayoutAndRevalidation(t *testing.T) {
	result, capture := validBundleInput(t)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	if err := WriteBundle(first, result, capture); err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(second, result, capture); err != nil {
		t.Fatal(err)
	}

	index := capture.Index()
	wantFiles := map[string][]byte{
		"result.json":         mustBundleJSON(t, result),
		"evidence-index.json": mustBundleJSON(t, index),
	}
	for _, entry := range index.Blobs {
		blob, ok := capture.Blob(entry.Reference)
		if !ok {
			t.Fatalf("valid capture lost %s", entry.Reference)
		}
		digest := strings.TrimPrefix(entry.Reference, "blob:sha256:")
		wantFiles[filepath.Join("blobs", digest+".json")] = blob
	}

	for _, root := range []string{first, second} {
		assertBundleTree(t, root, wantFiles)
	}
	for relative := range wantFiles {
		one, err := os.ReadFile(filepath.Join(first, relative))
		if err != nil {
			t.Fatal(err)
		}
		two, err := os.ReadFile(filepath.Join(second, relative))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(one, two) {
			t.Errorf("%s differs between identical exports", relative)
		}
	}

	resultJSON, err := os.ReadFile(filepath.Join(first, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	indexJSON, err := os.ReadFile(filepath.Join(first, "evidence-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	readBlob := func(reference string) ([]byte, error) {
		digest := strings.TrimPrefix(reference, "blob:sha256:")
		return os.ReadFile(filepath.Join(first, "blobs", digest+".json"))
	}
	if _, err := Validate(ValidationInput{ResultJSON: resultJSON, IndexJSON: indexJSON, ReadBlob: readBlob}); err != nil {
		t.Fatalf("published bundle did not revalidate: %v", err)
	}
}

func TestWriteBundleAcceptsAuthoritativeCollectedStructuralFailure(t *testing.T) {
	result, capture := CollectResult(collectorTestInput(t))
	destination := filepath.Join(t.TempDir(), "collected")
	if err := WriteBundle(destination, result, capture); err != nil {
		t.Fatalf("WriteBundle collected result: %v", err)
	}

	resultJSON, err := os.ReadFile(filepath.Join(destination, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var published Result
	if err := decodeStrict(resultJSON, &published); err != nil {
		t.Fatal(err)
	}
	if published.Summary.Verdict != "fail" || published.Summary.FirstFailedCase == nil || *published.Summary.FirstFailedCase != "blocked" {
		t.Fatalf("published summary = %#v", published.Summary)
	}
	for _, caseName := range []string{"exited", "timeout"} {
		for i, step := range CanonicalScenario().Steps {
			if step.Case == caseName && published.Stages[i].Verdict != "pass" {
				t.Fatalf("published %s stage %d = %#v", caseName, i, published.Stages[i])
			}
		}
	}
}

func TestWriteBundleNeverOverwritesDestination(t *testing.T) {
	t.Run("already present", func(t *testing.T) {
		result, capture := validBundleInput(t)
		parent := t.TempDir()
		destination := filepath.Join(parent, "bundle")
		if err := os.Mkdir(destination, 0o711); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}

		err = WriteBundle(destination, result, capture)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("WriteBundle error = %v, want existing-destination refusal", err)
		}
		after, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) || after.Mode().Perm() != 0o711 {
			t.Fatalf("existing destination changed: before=%v after=%v", before.Mode(), after.Mode())
		}
		entries, err := os.ReadDir(destination)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("existing destination gained files: %v", entries)
		}
		assertNoBundleStaging(t, parent, filepath.Base(destination))
	})

	t.Run("created during publication", func(t *testing.T) {
		result, capture := validBundleInput(t)
		parent := t.TempDir()
		destination := filepath.Join(parent, "bundle")
		createdDestination := false
		err := writeBundle(destination, result, capture, func(path string, data []byte) error {
			if err := writePrivateBundleFile(path, data); err != nil {
				return err
			}
			if createdDestination {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) == "blobs" {
				if err := os.Mkdir(destination, 0o700); err != nil {
					return err
				}
				createdDestination = true
			}
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "publish") {
			t.Fatalf("writeBundle error = %v, want no-replace publication refusal", err)
		}
		if !createdDestination {
			t.Fatal("test did not create the competing destination")
		}
		entries, err := os.ReadDir(destination)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("competing destination was overwritten: %v", entries)
		}
		assertNoBundleStaging(t, parent, filepath.Base(destination))
	})
}

func TestWriteBundleRejectsInvalidResultAndCaptureBeforePublication(t *testing.T) {
	tests := []struct {
		name string
		want string
		edit func(*Result, *Capture)
	}{
		{
			name: "invalid result",
			want: "result scrub failed",
			edit: func(result *Result, _ *Capture) {
				result.Run.RunID = "Authorization: Bearer abcdefghijklmnop"
			},
		},
		{
			name: "missing blob",
			want: "missing blob",
			edit: func(_ *Result, capture *Capture) {
				delete(capture.blobs, capture.entries[0].Reference)
			},
		},
		{
			name: "unreferenced blob",
			want: "unreferenced blob",
			edit: func(_ *Result, capture *Capture) {
				if _, err := capture.AddJSON("common_collector", Evidence{Schema: EvidenceSchema, ScrubStatus: "pass", Observations: []Observation{}}); err != nil {
					t.Fatalf("add unreferenced evidence: %v", err)
				}
			},
		},
		{
			name: "mismatched blob",
			want: "digest mismatch",
			edit: func(_ *Result, capture *Capture) {
				ref := capture.entries[0].Reference
				capture.blobs[ref] = append(capture.blobs[ref], ' ')
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, capture := validBundleInput(t)
			test.edit(&result, capture)
			parent := t.TempDir()
			destination := filepath.Join(parent, "bundle")
			err := WriteBundle(destination, result, capture)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("WriteBundle error = %v, want %q", err, test.want)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid bundle published a final directory: %v", err)
			}
			assertNoBundleStaging(t, parent, filepath.Base(destination))
		})
	}
}

func TestWriteBundleFailureRemovesOnlyItsStagingDirectory(t *testing.T) {
	result, capture := validBundleInput(t)
	parent := t.TempDir()
	destination := filepath.Join(parent, "bundle")
	unrelated := filepath.Join(parent, ".bundle"+bundleStagingMarker+"keep")
	if err := os.Mkdir(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(unrelated, "marker")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected write failure")
	err := writeBundle(destination, result, capture, func(path string, data []byte) error {
		if filepath.Base(path) == "evidence-index.json" {
			return injected
		}
		return writePrivateBundleFile(path, data)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("writeBundle error = %v, want injected failure", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed write left a final directory: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep\n" {
		t.Fatalf("failure cleanup touched unrelated staging directory: data=%q err=%v", data, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(unrelated) {
		t.Fatalf("write failure left unexpected paths: %v", entries)
	}
}

func validBundleInput(t *testing.T) (Result, *Capture) {
	t.Helper()
	built := buildContractFixture(t)
	capture := &Capture{
		entries: append([]BlobEntry(nil), built.Index.Blobs...),
		blobs:   map[string][]byte{built.BlobRef: append([]byte(nil), built.Blob...)},
	}
	return built.Result, capture
}

func mustBundleJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := marshalBundleJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertBundleTree(t *testing.T, root string, wantFiles map[string][]byte) {
	t.Helper()
	wantPaths := []string{"blobs", "evidence-index.json", "result.json"}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var gotPaths []string
	for _, entry := range entries {
		gotPaths = append(gotPaths, entry.Name())
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("bundle root entries = %v, want %v", gotPaths, wantPaths)
	}
	for _, directory := range []string{root, filepath.Join(root, "blobs")} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("directory %s mode = %v, want 0700", directory, info.Mode())
		}
	}
	for relative, want := range wantFiles {
		path := filepath.Join(root, relative)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s content differs from deterministic bytes", relative)
		}
		if !bytes.HasSuffix(got, []byte("\n")) || bytes.HasSuffix(got, []byte("\n\n")) {
			t.Errorf("%s does not have exactly one final newline", relative)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Errorf("file %s mode = %v, want 0600", relative, info.Mode())
		}
	}
	blobs, err := os.ReadDir(filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != len(wantFiles)-2 {
		t.Errorf("blob count = %d, want %d", len(blobs), len(wantFiles)-2)
	}
}

func assertNoBundleStaging(t *testing.T, parent, destinationBase string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "." + destinationBase + bundleStagingMarker
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			t.Errorf("staging directory remains after failure: %s", entry.Name())
		}
	}
}
