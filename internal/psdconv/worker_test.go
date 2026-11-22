package psdconv

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"testing"
)

func TestPreviewWorker(t *testing.T) {
	if !WorkerSupported() {
		t.Skip("requires worker memory isolation")
	}
	f, err := os.CreateTemp(t.TempDir(), "input-*.psd")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(layeredFixture()); err != nil {
		t.Fatal(err)
	}

	worker := func(ctx context.Context, mode string, body string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreviewWorkerProcess$", "--", mode, body)
		cmd.Env = []string{"BDRIVE_PSD_WORKER_TEST=1"}
		return cmd
	}
	out, err := runPreviewWorker(context.Background(), worker, f, "render", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runPreviewWorker(ctx, worker, f, "render", nil); err == nil {
		t.Fatal("cancelled worker succeeded")
	}
}

func TestPreviewWorkerProcess(t *testing.T) {
	if os.Getenv("BDRIVE_PSD_WORKER_TEST") != "1" {
		return
	}
	args := os.Args[len(os.Args)-2:]
	os.Exit(PreviewWorkerMain(args, os.Stdin, os.Stdout))
}
