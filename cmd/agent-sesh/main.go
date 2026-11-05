package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/mattgmak/agent-sesh/internal/config"
	"github.com/mattgmak/agent-sesh/internal/counts"
	"github.com/mattgmak/agent-sesh/internal/daemon"
	"github.com/mattgmak/agent-sesh/internal/debug"
	"github.com/mattgmak/agent-sesh/internal/notify"
	"github.com/mattgmak/agent-sesh/internal/picker"
	"github.com/mattgmak/agent-sesh/internal/version"
)

// firstNonEmpty returns the first value with content, or "".
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// daemonAddr returns the listen address from a flag, then config, then the default.
func daemonAddr(flagValue string) string {
	if addr := firstNonEmpty(flagValue); addr != "" {
		return addr
	}
	return net.JoinHostPort(daemon.DefaultHost, strconv.Itoa(config.Load().Port()))
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			if err := version.Write(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "list":
			opts := picker.ListOptions{}
			for i := 2; i < len(os.Args); i++ {
				if os.Args[i] != "--json" {
					fmt.Fprintln(os.Stderr, "usage: agent-sesh list [--json]")
					os.Exit(2)
				}
				opts.JSON = true
			}
			if err := picker.List(os.Stdout, opts); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "send":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: agent-sesh send <session-id|target> [text] [--no-enter] [--stdin]")
				os.Exit(2)
			}
			key := os.Args[2]
			text := ""
			enter := true
			fromStdin := false
			var positional []string
			for _, arg := range os.Args[3:] {
				switch arg {
				case "--no-enter":
					enter = false
				case "--stdin":
					fromStdin = true
				default:
					positional = append(positional, arg)
				}
			}
			if fromStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				text = string(data)
			} else if len(positional) > 0 {
				text = strings.Join(positional, " ")
			}
			if err := picker.Send(os.Stdout, key, text, enter); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "counts":
			opts := counts.Options{}
			for i := 2; i < len(os.Args); i++ {
				switch os.Args[i] {
				case "--json":
					opts.JSON = true
				case "--format":
					if i+1 >= len(os.Args) {
						fmt.Fprintln(os.Stderr, "usage: agent-sesh counts [--json] [--format <template>]")
						os.Exit(2)
					}
					opts.Format = os.Args[i+1]
					i++
				default:
					fmt.Fprintln(os.Stderr, "usage: agent-sesh counts [--json] [--format <template>]")
					os.Exit(2)
				}
			}
			if opts.JSON && opts.Format != "" {
				fmt.Fprintln(os.Stderr, "agent-sesh counts: --json and --format are mutually exclusive")
				os.Exit(2)
			}
			if err := counts.Run(os.Stdout, opts); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "daemon":
			sub := "start"
			rest := os.Args[2:]
			if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
				sub = rest[0]
				rest = rest[1:]
			}
			switch sub {
			case "start", "stop", "status", "serve":
			default:
				fmt.Fprintln(os.Stderr, "usage: agent-sesh daemon [start|stop|status|serve] [--addr A] [--registry P] [--notify]")
				os.Exit(2)
			}
			fs := flag.NewFlagSet("daemon", flag.ExitOnError)
			addr := fs.String("addr", "", "listen address, defaults to the config port on 127.0.0.1")
			registryFlag := fs.String("registry", "", "sessions.json path")
			notifyFlag := fs.Bool("notify", false, "raise notifications for configured status changes")
			if err := fs.Parse(rest); err != nil {
				os.Exit(2)
			}
			cfg := config.Load()
			opts := daemon.Options{
				Addr:     daemonAddr(*addr),
				Registry: *registryFlag,
				NotifyOn: *notifyFlag || cfg.Notify.Enabled,
				Notify: notify.Options{
					Backend: cfg.Notify.Backend,
					TTY:     cfg.Notify.TTY,
					Command: cfg.Notify.Command,
					Stdout:  os.Stderr,
				},
			}
			var err error
			switch sub {
			case "serve":
				ctx, stop := daemon.Context()
				defer stop()
				err = daemon.Serve(ctx, opts)
			case "start":
				err = daemon.Start(opts)
			case "stop":
				err = daemon.Stop(opts)
			case "status":
				err = daemon.Status(os.Stdout, opts)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "api":
			fs := flag.NewFlagSet("api", flag.ExitOnError)
			addr := fs.String("addr", "", "listen address, defaults to the config port on 127.0.0.1")
			if err := fs.Parse(os.Args[2:]); err != nil {
				os.Exit(2)
			}
			if fs.NArg() < 1 {
				fmt.Fprintln(os.Stderr, "usage: agent-sesh api <path> [--addr A]")
				os.Exit(2)
			}
			body, err := daemon.Get(daemonAddr(*addr), fs.Arg(0))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if _, err := os.Stdout.Write(body); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "notify":
			fs := flag.NewFlagSet("notify", flag.ExitOnError)
			backend := fs.String("backend", "", "auto, osc, notify-send, osascript, command, log")
			ttyFlag := fs.String("tty", "", "terminal device for the osc backend")
			commandFlag := fs.String("command", "", "shell command for the command backend")
			event := fs.String("event", "", "event name, for example tool_call")
			sessionFlag := fs.String("session", "", "session id or target to describe")
			title := fs.String("title", "", "notification title")
			body := fs.String("body", "", "notification body")
			if err := fs.Parse(os.Args[2:]); err != nil {
				os.Exit(2)
			}
			cfg := config.Load()
			e := notify.Event{Event: *event, Title: *title, Body: *body}
			if key := firstNonEmpty(*sessionFlag); key != "" {
				session, ok, err := picker.Lookup(key)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				if !ok {
					fmt.Fprintf(os.Stderr, "session %q not found\n", key)
					os.Exit(1)
				}
				e.SessionID = session.ID
				e.Target = session.TmuxTarget
				e.Agent = session.Agent
				e.Project = session.CWD
				e.Branch = session.Branch
				if e.Event == "" {
					e.Event = string(session.DisplayStatus())
				}
			}
			opts := notify.Options{
				Backend: firstNonEmpty(*backend, cfg.Notify.Backend),
				TTY:     firstNonEmpty(*ttyFlag, cfg.Notify.TTY),
				Command: firstNonEmpty(*commandFlag, cfg.Notify.Command),
				Stdout:  os.Stdout,
			}
			if err := notify.Send(e, opts); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "debug":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: agent-sesh debug <registry|validate|panes|discover|pane <target>>")
				os.Exit(2)
			}
			switch os.Args[2] {
			case "registry":
				if err := debug.Registry(os.Stdout); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			case "validate":
				if err := debug.Validate(os.Stdout); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			case "panes":
				if err := debug.Panes(os.Stdout); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			case "discover":
				if err := debug.Discover(os.Stdout); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			case "pane":
				if len(os.Args) < 4 {
					fmt.Fprintln(os.Stderr, "usage: agent-sesh debug pane <tmux-target>")
					os.Exit(2)
				}
				if err := debug.Pane(os.Stdout, os.Args[3]); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			default:
				fmt.Fprintln(os.Stderr, "usage: agent-sesh debug <registry|validate|panes|discover|pane <target>>")
				os.Exit(2)
			}
			return
		case "preview":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: agent-sesh preview <session-id>")
				os.Exit(2)
			}
			if err := picker.Preview(os.Stdout, os.Args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "fzf":
			if err := picker.RunFzf(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
			os.Exit(2)
		}
	}

	if os.Getenv("AGENT_SESH_FZF") == "1" {
		if err := picker.RunFzf(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := picker.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
