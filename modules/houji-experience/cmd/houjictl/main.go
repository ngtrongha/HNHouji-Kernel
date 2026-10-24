package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"houji-experience/internal/experience"
)

func readInput() ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(os.Stdin, 65537))
	if len(b) > 65536 {
		return nil, errors.New("request too large")
	}
	return b, e
}
func parseRequest(b []byte) (experience.Request, error) {
	var r experience.Request
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	e := d.Decode(&r)
	if e == nil {
		var extra any
		if d.Decode(&extra) != io.EOF {
			e = errors.New("invalid request")
		}
	}
	return r, e
}

func run(ctx context.Context, args []string) error {
	if runtime.GOOS == "android" && os.Geteuid() != 0 {
		return errors.New("run via KernelSU root")
	}
	a := experience.New(experience.DefaultPaths(), experience.AndroidRunner{})
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "watch":
		return a.Watch(ctx)
	case "install":
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		return a.Install(ctx, exe)
	case "recover":
		return a.Recover(ctx)
	case "cleanup-boot":
		return a.CleanupBoot(ctx)
	case "uninstall":
		return a.Uninstall(ctx)
	}
	r := experience.Request{Action: args[0]}
	switch args[0] {
	case "rpc":
		var b []byte
		var e error
		if v := os.Getenv("HOUJI_REQUEST_B64"); v != "" {
			if len(v) > 90000 {
				return errors.New("request too large")
			}
			b, e = base64.StdEncoding.DecodeString(v)
			_ = os.Unsetenv("HOUJI_REQUEST_B64")
		} else {
			b, e = readInput()
		}
		if e != nil {
			return errors.New("invalid request encoding")
		}
		r, e = parseRequest(b)
		if e != nil {
			return errors.New("invalid request JSON")
		}
	case "apps":
		if len(args) == 4 && args[1] == "assign" {
			r = experience.Request{Action: "app_set", Package: args[2], Profile: args[3]}
		} else if len(args) == 3 && args[1] == "remove" {
			r = experience.Request{Action: "app_remove", Package: args[2]}
		} else {
			return errors.New("apps assign PACKAGE PROFILE | apps remove PACKAGE")
		}
	case "protect":
		if len(args) != 3 || (args[1] != "add" && args[1] != "remove") {
			return errors.New("protect add|remove PACKAGE")
		}
		r = experience.Request{Action: "protect_set", Package: args[2], Protected: args[1] == "add"}
	case "ai":
		if len(args) < 2 {
			return errors.New("ai setup|test|status|preview|ask|disable|clear-config")
		}
		switch args[1] {
		case "setup":
			b, e := readInput()
			if e != nil {
				return e
			}
			var ai experience.AIConfig
			if json.Unmarshal(b, &ai) != nil {
				return errors.New("setup expects AI JSON on stdin")
			}
			r = experience.Request{Action: "ai_save", AI: &ai}
		case "test", "status", "preview", "disable":
			r.Action = "ai_" + args[1]
		case "clear-config":
			r.Action = "ai_clear"
		case "ask":
			b, e := readInput()
			if e != nil {
				return e
			}
			r.Action = "ai_ask"
			r.Prompt = string(b)
		default:
			return errors.New("unknown AI command")
		}
	}
	response := a.Dispatch(ctx, r)
	if e := json.NewEncoder(os.Stdout).Encode(response); e != nil {
		return e
	}
	if !response.OK {
		return errors.New("operation failed; see JSON response")
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := run(ctx, os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
