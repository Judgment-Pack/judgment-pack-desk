package codexbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The helper is a fresh test-binary process, never a user's installed client.
func TestProtocolHelper(t *testing.T) {
	if os.Getenv("JPS_CODEX_HELPER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(scan.Bytes(), &req)
		if req.Method == "initialized" {
			continue
		}
		switch req.Method {
		case "initialize":
			if os.Getenv("JPS_CODEX_WRONG_VERSION") == "1" {
				fmt.Printf(`{"id":%s,"result":{"userAgent":"jps_desk/0.146.0 (Linux)"}}`+"\n", req.ID)
				continue
			}
			fmt.Printf(`{"id":%s,"result":{"userAgent":"jps_desk/0.156.0 (Linux)"}}`+"\n", req.ID)
		case "answer":
			fmt.Printf(`{"id":%s,"result":{"value":42}}`+"\n", req.ID)
		case "fail":
			fmt.Printf(`{"id":%s,"error":{"code":-1,"message":"PRIVATE_TOKEN_SENTINEL"}}`+"\n", req.ID)
		case "malformed":
			fmt.Println("not json")
		case "oversized":
			fmt.Println(strings.Repeat("x", maxFrame+1))
		case "flood":
			for i := 0; i < 100; i++ {
				fmt.Println(`{"method":"unexpected","params":{}}`)
			}
		case "hang":
			time.Sleep(time.Minute)
		}
	}
	os.Exit(0)
}

func helperClient(t *testing.T) *client {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestProtocolHelper$")
	command.Env = []string{"JPS_CODEX_HELPER=1"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := startClient(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	return c
}

func TestRepliesAndRedactedErrors(t *testing.T) {
	c := helperClient(t)
	var result struct {
		Value int `json:"value"`
	}
	if err := c.call(context.Background(), "answer", map[string]any{}, &result); err != nil || result.Value != 42 {
		t.Fatalf("reply: %v %#v", err, result)
	}
	if err := c.call(context.Background(), "fail", nil, nil); !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "PRIVATE_TOKEN") {
		t.Fatalf("error was not sanitized: %v", err)
	}
}

func TestCancellationReapsProcess(t *testing.T) {
	c := helperClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := c.call(ctx, "hang", nil, nil); err == nil {
		t.Fatal("expected cancelled call")
	}
	select {
	case <-c.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("process was not reaped")
	}
	if c.cmd.ProcessState == nil {
		t.Fatal("missing process state")
	}
}

func TestBrokenProtocolFailsClosed(t *testing.T) {
	for _, method := range []string{"malformed", "oversized", "flood"} {
		t.Run(method, func(t *testing.T) {
			c := helperClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := c.call(ctx, method, nil, nil); err == nil {
				t.Fatal("broken protocol accepted")
			}
			select {
			case <-c.exited:
			case <-time.After(time.Second):
				t.Fatal("process not reaped")
			}
		})
	}
}

func TestConcurrentRPCReplies(t *testing.T) {
	c := helperClient(t)
	answers := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() {
			var got struct{ Value int }
			err := c.call(context.Background(), "answer", nil, &got)
			if err == nil && got.Value != 42 {
				err = errors.New("wrong reply")
			}
			answers <- err
		}()
	}
	for i := 0; i < 12; i++ {
		if err := <-answers; err != nil {
			t.Fatal(err)
		}
	}
}

// Opt-in, credential-free check against the installed native client. All state
// is created under t.TempDir; no login or inference method is called.
func TestNativeHandshake(t *testing.T) {
	binary := os.Getenv("JPS_CODEX_TEST_BINARY")
	if binary == "" {
		t.Skip("set JPS_CODEX_TEST_BINARY for the native handshake check")
	}
	base := t.TempDir()
	home := base + "/home"
	profile := home + "/.codex"
	work := base + "/work"
	for _, dir := range []string{home, profile, work} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := `forced_login_method = "chatgpt"
cli_auth_credentials_store = "file"
project_doc_max_bytes = 0
web_search = "disabled"
[analytics]
enabled = false
[features]
apps = false
hooks = false
plugins = false
remote_plugin = false
shell_snapshot = false
`
	if err := os.WriteFile(profile+"/config.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "CODEX_HOME=" + profile, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "RUST_LOG=off"}
	git := exec.Command("/usr/bin/git", "init", "-q", work)
	git.Env = env
	if err := git.Run(); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "app-server", "--stdio", "--strict-config")
	command.Dir = work
	command.Env = env
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := startClient(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var raw json.RawMessage
	if err := c.call(ctx, "account/read", map[string]any{"refreshToken": false}, &raw); err != nil {
		t.Fatal(err)
	}
	state, err := accountState(raw)
	if err != nil || state.Authenticated {
		t.Fatalf("empty profile was not an absent subscription: %#v %v", state, err)
	}
}

func TestDifferentVersionIsRejected(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestProtocolHelper$")
	command.Env = []string{"JPS_CODEX_HELPER=1", "JPS_CODEX_WRONG_VERSION=1"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if c, err := startClient(ctx, command); !errors.Is(err, ErrUnavailable) {
		if c != nil {
			c.close()
		}
		t.Fatalf("unexpected version accepted: %v", err)
	}
	if command.ProcessState == nil {
		t.Fatal("incompatible process was not reaped")
	}
}
