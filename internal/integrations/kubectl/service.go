package kubectl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/globlit/toolbox/internal/config"
	"github.com/globlit/toolbox/internal/runner"
	"github.com/globlit/toolbox/internal/sanitize"
)

type executor interface {
	Run(context.Context, []string) (runner.Result, error)
}
type Service struct {
	settings      config.Kubectl
	contexts      map[string]*Service
	runner        executor
	sanitizer     *sanitize.Sanitizer
	forwardRunner *runner.Runner
	mu            sync.Mutex
	forwards      map[string]*forwardSession
	closed        bool
}
type Result struct {
	Context       string          `json:"context"`
	Namespaces    []string        `json:"namespaces,omitempty"`
	Data          json.RawMessage `json:"data,omitempty"`
	Text          string          `json:"text,omitempty"`
	Diagnostics   string          `json:"diagnostics,omitempty"`
	WithheldLines int             `json:"withheld_lines,omitempty"`
	Redactions    int             `json:"redactions"`
	Error         string          `json:"error,omitempty"`
	Untrusted     bool            `json:"untrusted"`
}

func New(c config.Config) (*Service, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	root := &Service{settings: c.Kubectl, contexts: make(map[string]*Service), forwards: make(map[string]*forwardSession)}
	pool := runner.NewPool()
	var allLiterals []string
	for _, k := range c.Kubectl.Scopes() {
		credentials, literals, err := credentialSnapshot(k)
		if err != nil {
			return nil, err
		}
		allLiterals = append(allLiterals, literals...)
		timeout := time.Duration(k.TimeoutSeconds) * time.Second
		setup := func(dir string) ([]string, []string, error) {
			path := filepath.Join(dir, "kubeconfig.json")
			if err := os.WriteFile(path, credentials, 0600); err != nil {
				return nil, nil, err
			}
			return []string{"--kubeconfig=" + path, "--context=toolbox", "--cache-dir=" + filepath.Join(dir, "cache"), "--request-timeout=" + timeout.String()}, []string{"KUBECTL_KUBERC=false", "KUBERC=off"}, nil
		}
		r, err := pool.NewRunner(k.Binary, k.ExecutableSHA256, setup, timeout, k.MaxOutputBytes)
		if err != nil {
			return nil, err
		}
		root.contexts[k.Context] = &Service{settings: k, runner: r, forwardRunner: r}
	}
	san, err := sanitize.New(c.Redaction, allLiterals)
	if err != nil {
		return nil, err
	}
	for _, scoped := range root.contexts {
		scoped.sanitizer = san
	}
	return root, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	var processes []*runner.Process
	for _, session := range s.forwards {
		processes = append(processes, session.process)
	}
	s.mu.Unlock()
	for _, process := range processes {
		process.Stop()
	}
}

func (s *Service) Get(ctx context.Context, in GetInput) (Result, error) {
	scoped, err := s.selectContext(in.Context)
	if err != nil {
		return Result{}, err
	}
	if in.AllNamespaces {
		return scoped.getAll(ctx, in)
	}
	args, err := getArgs(scoped.settings, in)
	if err != nil {
		return Result{}, err
	}
	return scoped.execute(ctx, args, true)
}
func (s *Service) Raw(ctx context.Context, in RawInput) (Result, error) {
	scoped, err := s.selectContext(in.Context)
	if err != nil {
		return Result{}, err
	}
	args, err := rawArgs(scoped.settings, in)
	if err != nil {
		return Result{}, err
	}
	return scoped.execute(ctx, args, true)
}
func (s *Service) Logs(ctx context.Context, in LogsInput) (Result, error) {
	scoped, err := s.selectContext(in.Context)
	if err != nil {
		return Result{}, err
	}
	args, err := logsArgs(scoped.settings, in)
	if err != nil {
		return Result{}, err
	}
	return scoped.execute(ctx, args, false)
}

func (s *Service) execute(ctx context.Context, args []string, structured bool) (Result, error) {
	r, runErr := s.runner.Run(ctx, args)
	result := Result{Context: s.settings.Context, Untrusted: true}
	var report sanitize.Report
	result.Diagnostics, report = s.sanitizer.Text(r.Stderr)
	result.Redactions += report.Redactions
	result.WithheldLines += report.WithheldLines
	if runErr != nil {
		// Failed stdout may contain malformed or partial resources. Never fall back
		// from structured parsing to raw output, even on a kubectl error.
		result.Error = runErr.Error()
	} else if structured {
		var err error
		result.Data, report, err = s.sanitizer.JSON(r.Stdout)
		if err != nil {
			return Result{}, err
		}
		if len(result.Data) == 0 || result.Data[0] != '{' {
			return Result{}, errors.New("expected a JSON resource object. Response discarded")
		}
		result.Redactions += report.Redactions
		result.WithheldLines += report.WithheldLines
	} else {
		result.Text, report = s.sanitizer.Text(r.Stdout)
		result.Redactions += report.Redactions
		result.WithheldLines += report.WithheldLines
	}
	return s.checkResult(result)
}

func (s *Service) checkResult(result Result) (Result, error) {
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > s.settings.MaxOutputBytes {
		return Result{}, errors.New("sanitized output exceeded the limit. Response discarded. Narrow the query")
	}
	return result, nil
}
