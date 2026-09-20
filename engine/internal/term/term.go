// Package term runs an interactive shell in a container and bridges it to
// the UI socket. Bytes go out as they arrive; input and resizes come in as
// frames. This is a mutation of nothing in the cluster, but it is recorded in
// the audit log because a shell can do anything.
package term

import (
	"context"
	"io"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
)

type Sink interface {
	Output(data []byte)
	Closed(err error)
}

type Session struct {
	stdin  *io.PipeWriter
	sizes  chan remotecommand.TerminalSize
	cancel context.CancelFunc
	once   sync.Once
}

func (s *Session) Input(b []byte) { _, _ = s.stdin.Write(b) }
func (s *Session) Resize(cols, rows uint16) {
	select {
	case s.sizes <- remotecommand.TerminalSize{Width: cols, Height: rows}:
	default:
	}
}
func (s *Session) Close() {
	s.once.Do(func() {
		s.cancel()
		_ = s.stdin.Close()
	})
}

type sizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	s, ok := <-q.ch
	if !ok {
		return nil
	}
	return &s
}

type writer struct{ sink Sink }

func (w writer) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	w.sink.Output(b)
	return len(p), nil
}

// Start opens a TTY shell in the container. Prefers bash, falls back to sh.
func Start(parent context.Context, sess *cluster.Session, namespace, pod, container string, cols, rows uint16, sink Sink) (*Session, error) {
	ctx, cancel := context.WithCancel(parent)
	req := sess.Client.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"},
			Stdin:     true, Stdout: true, Stderr: true, TTY: true,
		}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(sess.Rest, "POST", req.URL())
	if err != nil {
		cancel()
		return nil, err
	}
	pr, pw := io.Pipe()
	s := &Session{stdin: pw, sizes: make(chan remotecommand.TerminalSize, 4), cancel: cancel}
	s.sizes <- remotecommand.TerminalSize{Width: cols, Height: rows}
	go func() {
		err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin: pr, Stdout: writer{sink}, Stderr: writer{sink}, Tty: true, TerminalSizeQueue: sizeQueue{s.sizes},
		})
		// Once the stream is gone nothing reads this pipe. Closing the read
		// end makes any later keystroke fail immediately instead of blocking
		// forever on a pipe with no reader, which would wedge the whole
		// socket: exec.input is handled on the same goroutine that would
		// have to process the exec.stop that could free it.
		_ = pr.CloseWithError(io.ErrClosedPipe)
		sink.Closed(err)
	}()
	return s, nil
}
