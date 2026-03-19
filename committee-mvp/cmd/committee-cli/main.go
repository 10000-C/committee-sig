package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"committee-mvp/internal/config"
)

type controlRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type controlResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "submit-sign-request":
		if err := runSubmitSignRequest(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "submit-sign-request failed: %v\n", err)
			os.Exit(1)
		}
	case "add-peer":
		if err := runAddPeer(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "add-peer failed: %v\n", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func runSubmitSignRequest(args []string) error {
	fs := flag.NewFlagSet("submit-sign-request", flag.ContinueOnError)
	controlAddr := fs.String("control-addr", "127.0.0.1:4401", "coordinator control address")
	sessionID := fs.String("session", "", "session id")
	message := fs.String("message", "", "message to sign")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" || *message == "" {
		return errors.New("session and message are required")
	}

	conn, err := net.DialTimeout("tcp", *controlAddr, *timeout)
	if err != nil {
		return fmt.Errorf("dial %s: %w", *controlAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(*timeout))

	req := controlRequest{
		Action:    "submit_sign_request",
		SessionID: *sessionID,
		Message:   *message,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	var resp controlResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "unknown error"
		}
		return errors.New(resp.Error)
	}

	fmt.Printf("submitted sign request session=%s to %s\n", *sessionID, *controlAddr)
	return nil
}

func runAddPeer(args []string) error {
	fs := flag.NewFlagSet("add-peer", flag.ContinueOnError)
	from := fs.String("from", "", "source node id or all")
	peer := fs.String("peer", "", "peer node id")
	addr := fs.String("addr", "", "peer listen address")
	configDir := fs.String("config-dir", "configs/nodes", "directory holding node config json files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *peer == "" || *addr == "" {
		return errors.New("from, peer and addr are required")
	}

	targets, err := resolveTargetConfigs(*configDir, *from)
	if err != nil {
		return err
	}
	for _, path := range targets {
		cfg, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		cfg.StaticNodes = appendUnique(cfg.StaticNodes, *peer)
		if cfg.StaticNodeAddrs == nil {
			cfg.StaticNodeAddrs = make(map[string]string)
		}
		cfg.StaticNodeAddrs[*peer] = *addr
		if err := writeConfig(path, cfg); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("updated %s\n", path)
	}
	return nil
}

func resolveTargetConfigs(configDir, from string) ([]string, error) {
	if from == "all" {
		entries, err := os.ReadDir(configDir)
		if err != nil {
			return nil, err
		}
		res := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "node-") || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			res = append(res, filepath.Join(configDir, e.Name()))
		}
		sort.Strings(res)
		if len(res) == 0 {
			return nil, fmt.Errorf("no node config found under %s", configDir)
		}
		return res, nil
	}
	path := filepath.Join(configDir, from+".json")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("node config not found: %s", path)
	}
	return []string{path}, nil
}

func writeConfig(path string, cfg *config.NodeConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func appendUnique(in []string, s string) []string {
	for _, v := range in {
		if v == s {
			return in
		}
	}
	return append(in, s)
}

func printUsage() {
	fmt.Println("committee-cli usage:")
	fmt.Println("  committee-cli submit-sign-request --control-addr 127.0.0.1:4401 --session s1 --message hello")
	fmt.Println("  committee-cli add-peer --from node-1 --peer node-9 --addr 127.0.0.1:3409 [--config-dir configs/nodes]")
}
