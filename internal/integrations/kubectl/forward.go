package kubectl

import (
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"toolbox/internal/config"
	"toolbox/internal/runner"
)

type ForwardInput struct {
	Context         string   `json:"context,omitempty" jsonschema:"Approved context name. Required when multiple contexts are configured."`
	Target          string   `json:"target" jsonschema:"Pod name or pod/name, service/name, deployment/name."`
	Ports           []string `json:"ports" jsonschema:"One to eight numeric [LOCAL:]REMOTE mappings. Use :REMOTE for a random local port. Local ports must be 1024 or higher."`
	Namespace       string   `json:"namespace,omitempty" jsonschema:"Approved namespace. Defaults to the configured namespace."`
	DurationSeconds int      `json:"duration_seconds,omitempty" jsonschema:"Tunnel lifetime in seconds. Defaults to the operator limit, at most 3600."`
}
type ForwardID struct {
	ID string `json:"id" jsonschema:"Tunnel identifier returned by kubectl_port_forward."`
}
type ForwardPort struct {
	Local  int `json:"local"`
	Remote int `json:"remote"`
}
type ForwardResult struct {
	Context          string        `json:"context"`
	ID               string        `json:"id"`
	Target           string        `json:"target"`
	Namespace        string        `json:"namespace"`
	Address          string        `json:"address"`
	Ports            []ForwardPort `json:"ports"`
	ExpiresAt        time.Time     `json:"expires_at"`
	Status           string        `json:"status"`
	TrafficSanitized bool          `json:"traffic_sanitized"`
	ReadOnly         bool          `json:"read_only"`
}
type forwardSession struct {
	process *runner.Process
	result  ForwardResult
	stopped bool
}

func forwardArgs(k config.Kubectl, in ForwardInput) ([]string, string, time.Duration, error) {
	if !k.AllowPortForward {
		return nil, "", 0, errors.New("port forwarding is disabled in the operator configuration")
	}
	ns, err := namespace(k, in.Namespace)
	if err != nil {
		return nil, "", 0, err
	}
	parts := strings.Split(in.Target, "/")
	if len(parts) == 1 {
		parts = []string{"pod", parts[0]}
	}
	if len(parts) != 2 || !validName(parts[1]) {
		return nil, "", 0, errors.New("invalid port-forward target")
	}
	kinds := map[string]string{"pod": "pods", "pods": "pods", "service": "services", "services": "services", "svc": "services", "deployment": "deployments", "deployments": "deployments", "deploy": "deployments"}
	kind, ok := kinds[parts[0]]
	if !ok {
		return nil, "", 0, errors.New("port forwarding supports pods, services, and deployments")
	}
	if len(in.Ports) < 1 || len(in.Ports) > 8 {
		return nil, "", 0, errors.New("require 1..8 port mappings")
	}
	args := []string{"port-forward", "--namespace=" + ns, "--address=127.0.0.1", "--pod-running-timeout=10s", kind + "/" + parts[1]}
	locals := map[int]bool{}
	for _, mapping := range in.Ports {
		local, remote, pair := strings.Cut(mapping, ":")
		if !pair {
			remote = local
		}
		r, err := strconv.Atoi(remote)
		if err != nil || r < 1 || r > 65535 || strconv.Itoa(r) != remote {
			return nil, "", 0, errors.New("remote port must be from 1 to 65535")
		}
		l := 0
		if local != "" {
			l, err = strconv.Atoi(local)
			if err != nil || l < 0 || l > 65535 || (l != 0 && l < 1024) || strconv.Itoa(l) != local {
				return nil, "", 0, errors.New("local port must be 0 or from 1024 to 65535")
			}
		}
		if l != 0 && locals[l] {
			return nil, "", 0, errors.New("duplicate local port")
		}
		locals[l] = true
		args = append(args, strconv.Itoa(l)+":"+strconv.Itoa(r))
	}
	duration := in.DurationSeconds
	if duration == 0 {
		duration = k.PortForwardMaxSeconds
	}
	if duration < 1 || duration > k.PortForwardMaxSeconds {
		return nil, "", 0, errors.New("tunnel lifetime exceeds the operator limit")
	}
	return args, ns, time.Duration(duration) * time.Second, nil
}

var forwardingLine = regexp.MustCompile(`(?m)^Forwarding from 127\.0\.0\.1:([0-9]+) -> ([0-9]+)\r?$`)

func (s *Service) StartForward(ctx context.Context, in ForwardInput) (ForwardResult, error) {
	scoped, err := s.selectContext(in.Context)
	if err != nil {
		return ForwardResult{}, err
	}
	args, ns, duration, err := forwardArgs(scoped.settings, in)
	if err != nil {
		return ForwardResult{}, err
	}
	s.mu.Lock()
	if s.closed || scoped.forwardRunner == nil {
		s.mu.Unlock()
		return ForwardResult{}, errors.New("port-forward service is unavailable")
	}
	for id, session := range s.forwards {
		select {
		case <-session.process.Done():
			delete(s.forwards, id)
		default:
		}
	}
	process, err := scoped.forwardRunner.Start(context.Background(), args, duration)
	if err != nil {
		s.mu.Unlock()
		return ForwardResult{}, err
	}
	id := rand.Text()
	session := &forwardSession{process: process, result: ForwardResult{Context: scoped.settings.Context, ID: id, Target: args[4], Namespace: ns, Address: "127.0.0.1", ExpiresAt: process.Deadline(), Status: "starting", Ports: []ForwardPort{}}}
	s.forwards[id] = session
	s.mu.Unlock()
	ready := false
	defer func() {
		if !ready {
			process.Stop()
			s.mu.Lock()
			delete(s.forwards, id)
			s.mu.Unlock()
		}
	}()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ForwardResult{}, errors.New("port-forward startup cancelled")
		case <-process.Done():
			return ForwardResult{}, errors.New("port-forward startup failed. Check the target, permissions, and available ports")
		case <-timeout.C:
			return ForwardResult{}, errors.New("port-forward startup timed out")
		case <-ticker.C:
			matches := forwardingLine.FindAllSubmatch(process.Snapshot().Stdout, -1)
			if len(matches) != len(in.Ports) {
				continue
			}
			ports := make([]ForwardPort, 0, len(matches))
			for _, match := range matches {
				local, _ := strconv.Atoi(string(match[1]))
				remote, _ := strconv.Atoi(string(match[2]))
				if local < 1024 || local > 65535 || remote < 1 || remote > 65535 {
					return ForwardResult{}, errors.New("invalid port-forward startup response")
				}
				ports = append(ports, ForwardPort{local, remote})
			}
			s.mu.Lock()
			if s.closed || session.stopped {
				s.mu.Unlock()
				return ForwardResult{}, errors.New("port-forward startup cancelled")
			}
			session.result.Ports, session.result.Status = ports, "active"
			result := session.result
			s.mu.Unlock()
			ready = true
			return result, nil
		}
	}
}

func (s *Service) ForwardStatus(id string) (ForwardResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.forwards[id]
	if !ok {
		return ForwardResult{}, errors.New("tunnel is unknown or no longer tracked")
	}
	result := session.result
	select {
	case <-session.process.Done():
		switch {
		case session.stopped:
			result.Status = "stopped"
		case !time.Now().Before(result.ExpiresAt):
			result.Status = "expired"
		default:
			result.Status = "closed"
		}
	default:
	}
	result.Ports = append([]ForwardPort{}, result.Ports...)
	return result, nil
}

func (s *Service) StopForward(id string) (ForwardResult, error) {
	s.mu.Lock()
	session, ok := s.forwards[id]
	if !ok {
		s.mu.Unlock()
		return ForwardResult{}, errors.New("tunnel is unknown or no longer tracked")
	}
	session.stopped = true
	s.mu.Unlock()
	session.process.Stop()
	return s.ForwardStatus(id)
}

func (s *Service) WaitForward(ctx context.Context, id string) (ForwardResult, error) {
	s.mu.Lock()
	session, ok := s.forwards[id]
	s.mu.Unlock()
	if !ok {
		return ForwardResult{}, errors.New("tunnel is unknown or no longer tracked")
	}
	select {
	case <-ctx.Done():
		return s.StopForward(id)
	case <-session.process.Done():
		return s.ForwardStatus(id)
	}
}

func (s *Service) ForwardList() []ForwardResult {
	s.mu.Lock()
	ids := make([]string, 0, len(s.forwards))
	for id := range s.forwards {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	sort.Strings(ids)
	out := []ForwardResult{}
	for _, id := range ids {
		if result, err := s.ForwardStatus(id); err == nil {
			out = append(out, result)
		}
	}
	return out
}
