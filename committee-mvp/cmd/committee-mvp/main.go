package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"committee-mvp/internal/committee"
	"committee-mvp/internal/config"
)

func main() {
	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "node":
			if err := runNode(os.Args[2:]); err != nil {
				log.Fatalf("node failed: %v", err)
			}
			return
		case "admin":
			if err := runAdmin(os.Args[2:]); err != nil {
				log.Fatalf("admin failed: %v", err)
			}
			return
		case "version":
			fmt.Println("committee-mvp v0.1.0")
			return
		case "help":
			printUsage()
			return
		default:
			printUsage()
			os.Exit(1)
		}
	}

	// geth-like default behavior: running without subcommand starts the node.
	if err := runNode(os.Args[1:]); err != nil {
		log.Fatalf("node failed: %v", err)
	}
}

func runNode(args []string) error {
	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	configPath := fs.String("config", "configs/devnet.json", "path to node config file")
	controlAddr := fs.String("control-addr", "", "optional control address for admin API, e.g. 127.0.0.1:4401")
	autoSession := fs.String("session", "", "optional session id for coordinator to trigger signing")
	autoMessage := fs.String("message", "", "optional message for coordinator to trigger signing")
	autoDelay := fs.Duration("auto-delay", 2*time.Second, "delay before coordinator sends sign request")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config failed: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if *controlAddr != "" {
		cfg.ControlAddr = *controlAddr
	}

	svc := committee.NewService(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("service start failed: %w", err)
	}
	if cfg.ControlAddr != "" {
		log.Printf("control server listening on %s", cfg.ControlAddr)
	}
	if cfg.NodeID == cfg.CoordinatorID && *autoSession != "" && *autoMessage != "" {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(*autoDelay):
			}
			for {
				if err := svc.SubmitSignRequest(*autoSession, []byte(*autoMessage)); err != nil {
					log.Printf("auto submit sign request retrying: %v", err)
					select {
					case <-ctx.Done():
						return
					case <-time.After(1 * time.Second):
					}
					continue
				}
				log.Printf("auto submit sign request sent session_id=%s", *autoSession)
				return
			}
		}()
	}
	<-ctx.Done()
	if err := svc.Stop(context.Background()); err != nil {
		log.Printf("service stop failed: %v", err)
	}
	return nil
}

func runAdmin(args []string) error {
	if len(args) < 1 {
		return errors.New("missing admin subcommand: submit-sign-request | set-auto-sign | sign-session | get-committee-pubkey | console | add-peer")
	}
	switch args[0] {
	case "submit-sign-request":
		return runSubmitSignRequest(args[1:])
	case "set-auto-sign":
		return runSetAutoSign(args[1:])
	case "sign-session":
		return runSignSession(args[1:])
	case "get-committee-pubkey":
		return runGetCommitteePubKey(args[1:])
	case "console":
		return runAdminConsole(args[1:])
	case "add-peer":
		return runAddPeer(args[1:])
	default:
		return fmt.Errorf("unknown admin subcommand: %s", args[0])
	}
}

type controlRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Enabled   bool   `json:"enabled"`
}

type controlResponse struct {
	OK              bool   `json:"ok"`
	Error           string `json:"error,omitempty"`
	CommitteePubKey string `json:"committee_pub_key,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	Message         string `json:"message,omitempty"`
	Bitmap          string `json:"bitmap,omitempty"`
	AggregateSig    string `json:"aggregate_sig,omitempty"`
	AggregatePubKey string `json:"aggregate_pub_key,omitempty"`
}

func runSubmitSignRequest(args []string) error {
	fs := flag.NewFlagSet("admin submit-sign-request", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
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
	_, err := sendControlRequest(*controlAddr, controlRequest{
		Action:    "submit_sign_request",
		SessionID: *sessionID,
		Message:   *message,
	}, *timeout)
	return err
}

func runSetAutoSign(args []string) error {
	fs := flag.NewFlagSet("admin set-auto-sign", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	controlAddr := fs.String("control-addr", "127.0.0.1:4401", "node control address")
	enabled := fs.Bool("enabled", true, "set true to enable auto signing, false to disable")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := sendControlRequest(*controlAddr, controlRequest{
		Action:  "set_auto_sign",
		Enabled: *enabled,
	}, *timeout); err != nil {
		return err
	}
	fmt.Printf("set auto-sign=%t on %s\n", *enabled, *controlAddr)
	return nil
}

func runSignSession(args []string) error {
	fs := flag.NewFlagSet("admin sign-session", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	controlAddr := fs.String("control-addr", "127.0.0.1:4401", "node control address")
	sessionID := fs.String("session", "", "session id")
	message := fs.String("message", "", "optional message override if request is not queued locally")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" {
		return errors.New("session is required")
	}
	if _, err := sendControlRequest(*controlAddr, controlRequest{
		Action:    "sign_session",
		SessionID: *sessionID,
		Message:   *message,
	}, *timeout); err != nil {
		return err
	}
	fmt.Printf("manual sign triggered session=%s on %s\n", *sessionID, *controlAddr)
	return nil
}

func runGetCommitteePubKey(args []string) error {
	fs := flag.NewFlagSet("admin get-committee-pubkey", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	controlAddr := fs.String("control-addr", "127.0.0.1:4401", "node control address")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := sendControlRequest(*controlAddr, controlRequest{Action: "get_committee_pubkey"}, *timeout)
	if err != nil {
		return err
	}
	if resp.CommitteePubKey == "" {
		return errors.New("empty committee public key in response")
	}
	fmt.Printf("committee_pub_key_hex=%s\n", resp.CommitteePubKey)
	return nil
}

func runAdminConsole(args []string) error {
	fs := flag.NewFlagSet("admin console", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	controlAddr := fs.String("control-addr", "127.0.0.1:4401", "default node control address")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	currentAddr := *controlAddr
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("interactive admin console")
	fmt.Println("type 'help' for commands, 'exit' to quit")
	fmt.Printf("current target: %s\n", currentAddr)

	for {
		fmt.Printf("[%s] > ", currentAddr)
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "help":
			printConsoleHelp()
		case "exit", "quit":
			return nil
		case "target":
			if len(parts) < 2 {
				fmt.Println("usage: target <host:port>")
				continue
			}
			currentAddr = parts[1]
			fmt.Printf("target switched to %s\n", currentAddr)
		case "pubkey":
			resp, err := sendControlRequest(currentAddr, controlRequest{Action: "get_committee_pubkey"}, *timeout)
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("committee_pub_key_hex=%s\n", resp.CommitteePubKey)
		case "result":
			sessionID := ""
			if len(parts) >= 2 {
				sessionID = parts[1]
			}
			resp, err := sendControlRequest(currentAddr, controlRequest{Action: "get_agg_result", SessionID: sessionID}, *timeout)
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("session=%s\n", resp.SessionID)
			fmt.Printf("message=%s\n", resp.Message)
			fmt.Printf("bitmap_hex=%s\n", resp.Bitmap)
			fmt.Printf("aggregate_sig_hex=%s\n", resp.AggregateSig)
			fmt.Printf("aggregate_pub_key_hex=%s\n", resp.AggregatePubKey)
		case "submit":
			sessionID := fmt.Sprintf("session-%d", time.Now().Unix())
			fmt.Printf("session id (default %s): ", sessionID)
			sInput, _ := reader.ReadString('\n')
			sInput = strings.TrimSpace(sInput)
			if sInput != "" {
				sessionID = sInput
			}
			fmt.Print("message: ")
			msg, _ := reader.ReadString('\n')
			msg = strings.TrimSpace(msg)
			if msg == "" {
				fmt.Println("message is required")
				continue
			}
			if _, err := sendControlRequest(currentAddr, controlRequest{
				Action:    "submit_sign_request",
				SessionID: sessionID,
				Message:   msg,
			}, *timeout); err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("submitted session=%s\n", sessionID)
		case "autosign":
			if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
				fmt.Println("usage: autosign <on|off>")
				continue
			}
			enabled := parts[1] == "on"
			if _, err := sendControlRequest(currentAddr, controlRequest{
				Action:  "set_auto_sign",
				Enabled: enabled,
			}, *timeout); err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("auto-sign set to %v\n", enabled)
		case "sign":
			fmt.Print("session id: ")
			sessionID, _ := reader.ReadString('\n')
			sessionID = strings.TrimSpace(sessionID)
			if sessionID == "" {
				fmt.Println("session id is required")
				continue
			}
			fmt.Print("message override (optional): ")
			msg, _ := reader.ReadString('\n')
			msg = strings.TrimSpace(msg)
			if _, err := sendControlRequest(currentAddr, controlRequest{
				Action:    "sign_session",
				SessionID: sessionID,
				Message:   msg,
			}, *timeout); err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("manual sign triggered session=%s\n", sessionID)
		default:
			fmt.Println("unknown command, type 'help'")
		}
	}
}

func printConsoleHelp() {
	fmt.Println("commands:")
	fmt.Println("  help                    show this help")
	fmt.Println("  target <host:port>      switch target control address")
	fmt.Println("  submit                  create a new signing request (interactive prompt)")
	fmt.Println("  pubkey                  query committee public key hex")
	fmt.Println("  result [session]        query aggregate result; omit session to get latest")
	fmt.Println("  autosign <on|off>       set node auto signing mode")
	fmt.Println("  sign                    manually sign queued session (interactive prompt)")
	fmt.Println("  exit                    quit console")
}

func sendControlRequest(controlAddr string, req controlRequest, timeout time.Duration) (*controlResponse, error) {
	conn, err := net.DialTimeout("tcp", controlAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", controlAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	var resp controlResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "unknown error"
		}
		return nil, errors.New(resp.Error)
	}
	return &resp, nil
}

func runAddPeer(args []string) error {
	fs := flag.NewFlagSet("admin add-peer", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
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
	fmt.Println("committee-mvp usage (geth-like):")
	fmt.Println("  committee-mvp [global flags]                  # default to node")
	fmt.Println("  committee-mvp node --config configs/devnet.json")
	fmt.Println("  committee-mvp admin submit-sign-request --control-addr 127.0.0.1:4401 --session s1 --message hello")
	fmt.Println("  committee-mvp admin set-auto-sign --control-addr 127.0.0.1:4402 --enabled=false")
	fmt.Println("  committee-mvp admin sign-session --control-addr 127.0.0.1:4402 --session s1")
	fmt.Println("  committee-mvp admin get-committee-pubkey --control-addr 127.0.0.1:4401")
	fmt.Println("  committee-mvp admin console --control-addr 127.0.0.1:4401")
	fmt.Println("  committee-mvp admin add-peer --from node-1 --peer node-9 --addr 127.0.0.1:3409")
	fmt.Println("  committee-mvp version")
}
