//go:build ignore

// Synthetic companion for the generic-provider browser acceptance test only.
// This file is never compiled into a Desk or gateway release.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	executable, _ := os.Executable()
	bundle := filepath.Dir(executable)
	if filepath.Base(executable) == "adapter-fixture" {
		var selection struct {
			ResourceID string `json:"resourceId"`
			Grant      string `json:"grant"`
		}
		if json.NewDecoder(io.LimitReader(os.Stdin, 4096)).Decode(&selection) != nil {
			os.Exit(2)
		}
		path := filepath.Join(os.Getenv("JPACK_CONNECTIONS_DIR"), "fixture-grant")
		granted, err := os.ReadFile(path)
		if err != nil || selection.ResourceID != "bucket/policy.txt" || selection.Grant != string(granted) {
			os.Exit(1)
		}
		if os.Remove(path) != nil {
			os.Exit(1)
		}
		data, err := os.ReadFile(filepath.Join(bundle, "resource.json"))
		if err != nil {
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(data)
		return
	}
	if len(os.Args) == 2 {
		name := ""
		switch os.Args[1] {
		case "--catalog-v3":
			name = "catalog.json"
		case "--local-plan":
			name = "plan.json"
		}
		if name != "" {
			data, err := os.ReadFile(filepath.Join(bundle, name))
			if err != nil {
				os.Exit(1)
			}
			_, _ = os.Stdout.Write(data)
			return
		}
	}
	dir := ""
	for i := 1; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--state-dir" {
			dir = os.Args[i+1]
		}
	}
	if dir == "" {
		os.Exit(2)
	}
	if os.MkdirAll(dir, 0700) != nil {
		os.Exit(1)
	}
	connected := false
	scan := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scan.Scan() {
		var request struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &request) != nil {
			os.Exit(2)
		}
		var result any
		switch request.Method {
		case "status":
			state := "not-connected"
			if connected {
				state = "connected"
			}
			result = map[string]any{"version": 1, "provider": "fixture-files", "state": state, "maxFiles": 4, "maxFileBytes": 4194304}
		case "configure":
			var values map[string]string
			if json.Unmarshal(request.Params, &values) != nil || values["folder"] != "policies" || values["key"] != "fixture-only-key" {
				os.Exit(2)
			}
			connected = true
			result = map[string]bool{"saved": true}
		case "search":
			if !connected {
				os.Exit(2)
			}
			result = map[string]any{"selectionContext": "fixture-epoch", "items": []any{map[string]string{"id": "bucket/policy.txt", "title": "Policy.txt", "url": "https://example.com/policy"}}, "more": false}
		case "select":
			var q struct {
				IDs     []string `json:"resourceIds"`
				Context string   `json:"selectionContext"`
			}
			if !connected || json.Unmarshal(request.Params, &q) != nil || len(q.IDs) != 1 || q.IDs[0] != "bucket/policy.txt" || q.Context != "fixture-epoch" {
				os.Exit(2)
			}
			bytes := make([]byte, 32)
			if _, err := rand.Read(bytes); err != nil {
				os.Exit(1)
			}
			grant := hex.EncodeToString(bytes)
			if os.WriteFile(filepath.Join(dir, "fixture-grant"), []byte(grant), 0600) != nil {
				os.Exit(1)
			}
			result = []any{map[string]string{"resourceId": q.IDs[0], "grant": grant}}
		case "disconnect":
			connected = false
			result = map[string]bool{"revoked": true}
		case "cancel":
			result = map[string]string{"state": "canceled"}
		default:
			fmt.Fprintln(os.Stderr, "unknown fixture method")
			os.Exit(2)
		}
		if enc.Encode(map[string]any{"id": request.ID, "result": result}) != nil {
			return
		}
	}
}
