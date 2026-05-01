package dockerx

import "testing"

func TestImagePullProgressSummarizesLayers(t *testing.T) {
	progress := newImagePullProgress("example/image:latest")
	progress.layers["a"] = imagePullLayer{status: "Downloading", current: 50, total: 100}
	progress.layers["b"] = imagePullLayer{status: "Extracting", current: 25, total: 100}
	progress.layers["c"] = imagePullLayer{status: "Waiting"}
	progress.layers["d"] = imagePullLayer{status: "Pull complete", current: 10, total: 100}

	detail, current, total := progress.summary()
	if detail != "1 downloading, 1 extracting, 1 waiting, 1 complete" {
		t.Fatalf("unexpected detail: %q", detail)
	}
	if current != 175 || total != 300 {
		t.Fatalf("unexpected progress totals: current=%d total=%d", current, total)
	}
}

func TestPullStatusKind(t *testing.T) {
	tests := map[string]string{
		"Downloading":        "downloading",
		"Download complete":  "complete",
		"Extracting":         "extracting",
		"Waiting":            "waiting",
		"Pulling fs layer":   "waiting",
		"Already exists":     "complete",
		"Pull complete":      "complete",
		"Verifying Checksum": "other",
	}
	for status, want := range tests {
		if got := pullStatusKind(status); got != want {
			t.Fatalf("pullStatusKind(%q) = %q, want %q", status, got, want)
		}
	}
}
