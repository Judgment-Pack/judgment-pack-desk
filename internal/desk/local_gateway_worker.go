package desk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type localWorkerOptions struct{ Bundle, Dir, Seed, Public string }

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// RunLocalGatewayWorker owns a gateway for exactly as long as its parent pipe
// lives. EOF also happens when Desk crashes: this works without PID reuse checks,
// platform-specific parent-death signals, a shell, or a background daemon.
func RunLocalGatewayWorker(input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return errors.New("invalid local worker request")
	}
	var options localWorkerOptions
	if json.Unmarshal(line, &options) != nil || !filepath.IsAbs(options.Bundle) || !filepath.IsAbs(options.Dir) || !filepath.IsAbs(options.Seed) || !publicKeyHex.MatchString(options.Public) {
		return errors.New("invalid local worker options")
	}
	if err = verifyGatewayBundle(options.Bundle); err != nil {
		return err
	}
	root, err := openPrivateDataRoot(options.Dir, false)
	if err != nil {
		return err
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, reader); cancel() }()
	for attempt := 0; attempt < 5; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		// The current pinned gateway CLI cannot accept an inherited listener or
		// port 0. Release the candidate port, require this child's own bound-address
		// announcement and verify the pre-pinned identity; retry a bind collision.
		listener.Close()
		url := "http://127.0.0.1:" + strconv.Itoa(port)
		cmd := exec.Command(filepath.Join(options.Bundle, executableName("gateway")), "serve", filepath.Join(options.Dir, "store"), options.Seed, localAuthority, filepath.Join(options.Dir, "registry.jsonl"),
			"--port", strconv.Itoa(port), "--receipt-version", "3", "--max-request", "33554432", "--source-timeout", "documents=40", "--source", "documents="+executableName("adapter-document")+" --max-bytes 16777216 --max-output 8388608 --timeout 30s", "--source-max-output", "8388608")
		cmd.Dir = options.Dir
		// The CLI splits a source declaration into words. Resolve the adapter by its
		// fixed basename on a dedicated PATH so installation paths may contain spaces.
		cmd.Env = []string{"PATH=" + options.Bundle}
		for _, key := range []string{"SYSTEMROOT", "WINDIR", "TMPDIR", "TEMP", "TMP"} {
			if value := os.Getenv(key); value != "" {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		cmd.Stdout = io.Discard
		if err = cmd.Start(); err != nil {
			return err
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		announced := make(chan struct{}, 1)
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "gateway on "+url+" (") {
					select {
					case announced <- struct{}{}:
					default:
					}
				}
			}
		}()
		timer := time.NewTimer(4 * time.Second)
		ready := false
		select {
		case <-announced:
			ready = true
		case <-done:
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		if ready {
			err = checkLocalGateway(ctx, url, options.Public)
		} else {
			err = errors.New("gateway did not bind its requested port")
		}
		if err == nil {
			err = json.NewEncoder(output).Encode(localGatewayPin{URL: url, Authority: localAuthority, Signer: localSigner{Algorithm: "ed25519", Public: options.Public}})
		}
		if err == nil {
			select {
			case <-ctx.Done():
			case <-done:
			}
		}
		stopLocalProcess(cmd, done)
		if ready || ctx.Err() != nil {
			return err
		}
	}
	return errors.New("local gateway could not bind a port")
}
func stopLocalProcess(cmd *exec.Cmd, done <-chan struct{}) {
	select {
	case <-done:
		return
	default:
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}
func checkLocalGateway(ctx context.Context, url, public string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/publickey", nil)
	if err != nil {
		return err
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var identity struct {
		Algorithm string
		Authority string
		PublicKey string
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&identity) != nil || identity.Algorithm != "ed25519" || identity.Authority != localAuthority || identity.PublicKey != public {
		return fmt.Errorf("local gateway identity did not match its private signing key")
	}
	return nil
}
