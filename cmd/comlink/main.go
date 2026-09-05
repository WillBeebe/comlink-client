// comlink-client contains only endpoint operations; it cannot run an exchange.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"comlink/internal/admission"
	"comlink/internal/handset"
)

const version = "0.3.0-beta.1"
const endpointDefault = "https://api.comlink.fyi/mcp"
const rootsJSON = "{\"xqkio6o9\":\"rlSTpfAC2sR2AJLINTwTVchXuG+fynoIGPR/CvyAWBM=\"}\n"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func emit(v any) error { return json.NewEncoder(os.Stdout).Encode(v) }
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	s, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !s.IsDir() || s.Mode().Perm()&0077 != 0 {
		return errors.New("state directory must be private (0700), without symlinks")
	}
	return nil
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: comlink <version|setup|doctor|mcp|policy|revoke>")
	}
	if args[0] == "version" {
		return emit(map[string]any{"version": version, "events": 1, "client_only": true})
	}
	switch args[0] {
	case "setup", "doctor", "mcp", "policy", "revoke":
	default:
		return errors.New("unknown client command")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	state := filepath.Join(home, ".local", "share", "comlink")
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := fs.String("state", state, "private state directory")
	file := fs.String("file", "", "existing handset profile path")
	roots := fs.String("roots", "", "pinned public issuer roots path")
	endpoint := fs.String("endpoint", endpointDefault, "HTTPS MCP resource")
	ca := fs.String("ca", "", "optional trusted CA file")
	policyFile := fs.String("policy", "", "owner-approved caller policy JSON")
	fs.Bool("enroll", true, "enroll automatically when register is called")
	if err = fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if !filepath.IsAbs(*dir) {
		return errors.New("state path must be absolute")
	}
	if *file == "" {
		*file = filepath.Join(*dir, "handset.json")
	}
	if *roots == "" {
		*roots = filepath.Join(*dir, "roots.json")
	}
	if args[0] == "setup" {
		if *endpoint != endpointDefault {
			return errors.New("setup pins the production authority only")
		}
		if err = privateDir(*dir); err != nil {
			return err
		}
		raw, e := os.ReadFile(*roots)
		if e == nil {
			if string(raw) != rootsJSON {
				return errors.New("existing roots differ; independently verify before changing")
			}
		} else if errors.Is(e, os.ErrNotExist) {
			f, e := os.OpenFile(*roots, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, e = f.WriteString(rootsJSON)
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		} else {
			return e
		}
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		return emit(map[string]any{"mcpServers": map[string]any{"comlink": map[string]any{"command": executable, "args": []string{"mcp", "--enroll", "--file", *file, "--roots", *roots, "--endpoint", *endpoint}}}})
	}
	if args[0] == "doctor" {
		raw, e := os.ReadFile(*roots)
		if e != nil {
			return errors.New("roots missing; run comlink setup")
		}
		var pins map[string]string
		if json.Unmarshal(raw, &pins) != nil || len(pins) == 0 {
			return errors.New("invalid roots")
		}
		exists := false
		if s, e := os.Lstat(*file); e == nil {
			if !s.Mode().IsRegular() || s.Mode().Perm()&0077 != 0 {
				return errors.New("profile must be a private regular file")
			}
			exists = true
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		return emit(map[string]any{"version": version, "roots_present": true, "profile_present": exists, "live_event_host_required": true, "network_tested": false, "enrollment": "occurs on MCP register"})
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if args[0] == "mcp" {
		return handset.ServePublic(ctx, *file, *roots, *endpoint, *ca)
	}
	if _, err = os.Stat(*file); err != nil {
		return errors.New("enroll this endpoint first")
	}
	cfg, err := handset.PublicProfile(*file, *roots, *endpoint)
	if err != nil {
		return err
	}
	if cfg.SubscriberID == "" {
		return errors.New("enroll this endpoint first")
	}
	var policy *admission.CallPolicy
	if args[0] == "policy" {
		raw, e := os.ReadFile(*policyFile)
		var p admission.CallPolicy
		if e != nil || len(raw) > 40000 || json.Unmarshal(raw, &p) != nil || !p.Valid() {
			return errors.New("valid owner policy required")
		}
		policy = &p
	}
	out, err := handset.EnrollmentOperation(ctx, cfg, *endpoint, *ca, args[0], cfg.SubscriberID, policy)
	if err != nil {
		return err
	}
	return emit(map[string]any{"number": out.Credential.Number, "policy": out.Policy, "revoked": out.Revoked, "reconnect_required": true})
}
