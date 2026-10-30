package kubectl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"toolbox/internal/config"
	"toolbox/internal/runner"
	"toolbox/internal/sanitize"
)

type executor interface {
	Run(context.Context, []string) (runner.Result, error)
}
type Service struct {
	settings  config.Kubectl
	runner    executor
	sanitizer *sanitize.Sanitizer
}
type Result struct {
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
	credentials, literals, err := credentialSnapshot(c.Kubectl)
	if err != nil {
		return nil, err
	}
	s, err := sanitize.New(c.Redaction, literals)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(c.Kubectl.TimeoutSeconds) * time.Second
	setup := func(dir string) ([]string, []string, error) {
		path := filepath.Join(dir, "kubeconfig.json")
		if err := os.WriteFile(path, credentials, 0600); err != nil {
			return nil, nil, err
		}
		return []string{"--kubeconfig=" + path, "--context=toolbox", "--cache-dir=" + filepath.Join(dir, "cache"), "--request-timeout=" + timeout.String()}, []string{"KUBECTL_KUBERC=false", "KUBERC=off"}, nil
	}
	r, err := runner.New(c.Kubectl.Binary, c.Kubectl.ExecutableSHA256, setup, timeout, c.Kubectl.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	return &Service{c.Kubectl, r, s}, nil
}

func (s *Service) Resources() []Resource { return Catalog(s.settings) }
func (s *Service) Namespaces() []string  { return append([]string{}, s.settings.Namespaces...) }
func (s *Service) Get(ctx context.Context, in GetInput) (Result, error) {
	args, err := getArgs(s.settings, in)
	if err != nil {
		return Result{}, err
	}
	return s.execute(ctx, args, true)
}
func (s *Service) Logs(ctx context.Context, in LogsInput) (Result, error) {
	args, err := logsArgs(s.settings, in)
	if err != nil {
		return Result{}, err
	}
	return s.execute(ctx, args, false)
}

func (s *Service) execute(ctx context.Context, args []string, structured bool) (Result, error) {
	r, runErr := s.runner.Run(ctx, args)
	result := Result{Untrusted: true}
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
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > s.settings.MaxOutputBytes {
		return Result{}, errors.New("sanitized output exceeded the limit. Response discarded. Narrow the query")
	}
	return result, nil
}
