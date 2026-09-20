package watch

import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
)

// LogSink receives batched lines from one log stream.
type LogSink interface {
	Lines(lines []string, eof bool)
	Error(err error)
}

// LogOptions selects what to stream.
type LogOptions struct {
	Namespace, Name, Container string
	Previous                   bool
	Tail                       int64
}

// maxTail bounds how much history a client may ask for. The value arrives
// off the socket, and "give me a hundred million lines" should not be a
// request the engine forwards.
const maxTail = 10000

// maxTailBytes bounds the one-shot read, which has no streaming backpressure.
const maxTailBytes = 1 << 20

// Logs follows a container's log until ctx is cancelled or the stream ends.
// Lines are batched every FlushInterval or every 500 lines, whichever first,
// so a chatty pod cannot flood the socket with one frame per line.
func Logs(ctx context.Context, sess *cluster.Session, o LogOptions, sink LogSink) {
	go func() {
		tail := o.Tail
		if tail <= 0 {
			tail = 200
		}
		if tail > maxTail {
			tail = maxTail
		}
		req := sess.Client.CoreV1().Pods(o.Namespace).GetLogs(o.Name, &corev1.PodLogOptions{
			Container:  o.Container,
			Follow:     true,
			Previous:   o.Previous,
			TailLines:  &tail,
			Timestamps: true,
		})
		stream, err := req.Stream(ctx)
		if err != nil {
			sink.Error(err)
			return
		}
		defer stream.Close()

		lines := make(chan string, 1024)
		go func() {
			defer close(lines)
			sc := bufio.NewScanner(stream)
			sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
			for sc.Scan() {
				select {
				case lines <- sc.Text():
				case <-ctx.Done():
					return
				}
			}
			if err := sc.Err(); err != nil && err != io.EOF && ctx.Err() == nil {
				sink.Error(err)
			}
		}()

		batch := make([]string, 0, 500)
		t := time.NewTicker(FlushInterval)
		defer t.Stop()
		flush := func(eof bool) {
			if len(batch) > 0 || eof {
				sink.Lines(batch, eof)
				batch = make([]string, 0, 500)
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case l, ok := <-lines:
				if !ok {
					flush(true)
					return
				}
				batch = append(batch, l)
				if len(batch) >= 500 {
					flush(false)
				}
			case <-t.C:
				flush(false)
			}
		}
	}()
}

// TailLogs fetches the last n lines of each container of a pod without
// following, for one-shot evidence gathering.
func TailLogs(ctx context.Context, sess *cluster.Session, namespace, pod string, n int64) []string {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p, err := sess.Client.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	var out []string
	if n > maxTail {
		n = maxTail
	}
	for _, c := range p.Spec.Containers {
		req := sess.Client.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: c.Name, TailLines: &n, Timestamps: true})
		// Streamed under a limit rather than DoRaw, which reads the whole
		// response into memory: a line count is not a byte count, and a
		// container writing very long lines can return hundreds of MB.
		rc, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(rc, maxTailBytes))
		_ = rc.Close()
		if err != nil || len(body) == 0 {
			continue
		}
		if len(p.Spec.Containers) > 1 {
			out = append(out, "# container "+c.Name)
		}
		for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}
