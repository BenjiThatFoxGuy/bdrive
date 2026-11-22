package psdconv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const WorkerArg = "--internal-psd-preview-worker"
const maxWorkerOutput = 20 << 20

func RunPreviewWorker(ctx context.Context, source *os.File, mode string, overrides map[int]bool) ([]byte, error) {
	if !WorkerSupported() {
		return nil, ErrUnsupportedPreview
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return runPreviewWorker(ctx, func(ctx context.Context, mode, body string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, exe, WorkerArg, mode, body)
		cmd.Env = []string{"GOMAXPROCS=2", "GOMEMLIMIT=768MiB"}
		return cmd
	}, source, mode, overrides)
}

func runPreviewWorker(ctx context.Context, command func(context.Context, string, string) *exec.Cmd, source *os.File, mode string, overrides map[int]bool) ([]byte, error) {
	if len(overrides) > previewMaxLayers || (mode != "inspect" && mode != "render") {
		return nil, ErrUnsupportedPreview
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	body, err := json.Marshal(overrides)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := command(ctx, mode, string(body))
	cmd.Stdin = source
	result := &limitedWorkerOutput{}
	cmd.Stdout = result
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if exit := new(exec.ExitError); errors.As(err, &exit) && exit.ExitCode() == 2 {
			return nil, ErrUnsupportedPreview
		}
		return nil, fmt.Errorf("PSD worker: %w", err)
	}
	return result.buf.Bytes(), nil
}

type limitedWorkerOutput struct{ buf bytes.Buffer }

func (o *limitedWorkerOutput) Write(p []byte) (int, error) {
	if len(p) > maxWorkerOutput-o.buf.Len() {
		return 0, errors.New("PSD worker output exceeded limit")
	}
	return o.buf.Write(p)
}

// PreviewWorkerMain runs only in a child process. Do not call it in the server.
func PreviewWorkerMain(args []string, input io.ReadSeeker, output io.Writer) int {
	if len(args) != 2 || len(args[1]) > 32<<10 || setWorkerLimit() != nil {
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "inspect":
		meta, err := InspectLayers(ctx, input)
		if err != nil {
			return 2
		}
		if err = json.NewEncoder(output).Encode(meta); err != nil {
			return 1
		}
	case "render":
		var overrides map[int]bool
		if err := json.Unmarshal([]byte(args[1]), &overrides); err != nil || len(overrides) > previewMaxLayers {
			return 2
		}
		data, err := RenderLayers(ctx, input, overrides)
		if err != nil {
			return 2
		}
		if _, err = output.Write(data); err != nil {
			return 1
		}
	default:
		return 2
	}
	return 0
}
