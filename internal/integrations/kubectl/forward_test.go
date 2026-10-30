package kubectl

import (
	"context"
	"fmt"
	"github.com/globlit/toolbox/internal/config"
	"github.com/globlit/toolbox/internal/testutil"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func forwardSettings() config.Kubectl {
	k := settings()
	k.AllowPortForward = true
	k.PortForwardMaxSeconds = 5
	return k
}
func TestForwardPolicy(t *testing.T) {
	for _, target := range []string{"api", "pod/api", "pods/api", "svc/api", "service/api", "deployment/api", "deploy/api"} {
		args, ns, d, err := forwardArgs(forwardSettings(), ForwardInput{Target: target, Ports: []string{"18080:80", ":443"}})
		if err != nil || ns != "apps" || d != 5*time.Second || args[0] != "port-forward" || args[2] != "--address=127.0.0.1" {
			t.Fatal(args, err)
		}
	}
	denied := []ForwardInput{{Target: "api", Ports: []string{"80"}}, {Target: "api", Ports: []string{"127.0.0.1:80:80"}}, {Target: "api", Ports: []string{"18080:0"}}, {Target: "api", Ports: []string{"18080:65536"}}, {Target: "api", Ports: []string{"18080:80", "18080:81"}}, {Target: "api", Ports: []string{":+80"}}, {Target: "api", Ports: []string{":http"}}, {Target: "api", Ports: []string{"--address=0.0.0.0"}}, {Target: "pods/api/exec", Ports: []string{":80"}}, {Target: "nodes/api", Ports: []string{":80"}}, {Target: "../api", Ports: []string{":80"}}, {Target: "api", Namespace: "other", Ports: []string{":80"}}, {Target: "api", Ports: []string{":80"}, DurationSeconds: 6}, {Target: "api"}}
	for _, in := range denied {
		if _, _, _, err := forwardArgs(forwardSettings(), in); err == nil {
			t.Fatal(in)
		}
	}
	if _, _, _, err := forwardArgs(settings(), ForwardInput{Target: "api", Ports: []string{":80"}}); err == nil {
		t.Fatal("disabled forwarding accepted")
	}
}
func forwardService(t *testing.T) *Service {
	t.Helper()
	c := testutil.Config(t)
	c.Kubectl.AllowPortForward = true
	c.Kubectl.PortForwardMaxSeconds = 5
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func echoTunnel(t *testing.T, r ForwardResult) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", r.Address, r.Ports[0].Local), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	_, err = conn.Write([]byte("echo"))
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	if _, err = io.ReadFull(conn, b); err != nil || string(b) != "echo" {
		t.Fatal(string(b), err)
	}
}
func assertTunnelClosed(t *testing.T, r ForwardResult) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", r.Address, r.Ports[0].Local), time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("tunnel still listening")
	}
}
func TestForwardLifecycle(t *testing.T) {
	s := forwardService(t)
	r, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080", ":8081"}})
	if err != nil || r.Status != "active" || r.ReadOnly || r.TrafficSanitized || len(r.Ports) != 2 {
		t.Fatal(r, err)
	}
	echoTunnel(t, r)
	if len(s.ForwardList()) != 1 {
		t.Fatal("tunnel not listed")
	}
	status, err := s.StopForward(r.ID)
	if err != nil || status.Status != "stopped" {
		t.Fatal(status, err)
	}
	assertTunnelClosed(t, r)
	if _, err := s.StopForward(r.ID); err != nil {
		t.Fatal("stop not idempotent", err)
	}
	r, err = s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}, DurationSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	status, err = s.WaitForward(context.Background(), r.ID)
	if err != nil || status.Status != "expired" {
		t.Fatal(status, err)
	}
	assertTunnelClosed(t, r)
	r, err = s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	assertTunnelClosed(t, r)
	if _, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}}); err == nil {
		t.Fatal("closed service started tunnel")
	}
}
func TestForwardFailureCancellationAndLimits(t *testing.T) {
	s := forwardService(t)
	for _, target := range []string{"failure", "overflow"} {
		_, err := s.StartForward(context.Background(), ForwardInput{Target: target, Ports: []string{":8080"}})
		if err == nil || strings.Contains(err.Error(), "canary") {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.StartForward(ctx, ForwardInput{Target: "slow", Ports: []string{":8080"}}); err == nil {
		t.Fatal("cancelled startup succeeded")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{fmt.Sprintf("%d:80", listener.Addr().(*net.TCPAddr).Port)}}); err == nil {
		t.Fatal("occupied port succeeded")
	}
	a, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}}); err == nil {
		t.Fatal("concurrency limit exceeded")
	}
	s.StopForward(a.ID)
	ctx, cancel2 := context.WithCancel(context.Background())
	cancel2()
	b, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.WaitForward(ctx, b.ID)
	if err != nil || result.Status != "stopped" {
		t.Fatal(result, err)
	}
	assertTunnelClosed(t, b)
}
