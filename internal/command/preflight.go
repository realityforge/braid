package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
)

type Git interface {
	RequireVersion(context.Context, string) error
	IsInsideWorkTree(context.Context) (bool, error)
	RelativeWorkingDir(context.Context) (string, error)
	StatusPorcelain(context.Context) (string, error)
}

type RemoteGit interface {
	Git
	RemoteURL(context.Context, string) (string, bool, error)
	RemoteAdd(context.Context, string, string) error
	RemoteRemove(context.Context, string) error
}

type AddGit interface {
	RemoteGit
	Head(context.Context) (string, error)
	RevParse(context.Context, string) (string, error)
	LsRemote(context.Context, ...string) (string, error)
	Fetch(context.Context, ...string) error
	LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error)
	ReadTreePrefix(context.Context, string, string, bool) error
	UpdateIndexCacheInfo(context.Context, string, string, string) error
	CheckoutIndex(context.Context, string) error
	Add(context.Context, string) error
	CommitMessage(context.Context, string) (bool, error)
	ResetHard(context.Context, string) error
}

type DiffGit interface {
	AddGit
	MakeTreeWithItem(context.Context, string, gitexec.TreeItem) (string, error)
	Diff(context.Context, ...string) (string, error)
}

type Requirements struct {
	Git      bool
	Root     bool
	Config   bool
	Clean    bool
	MayWrite bool
}

type Options struct {
	Git        Git
	WorkDir    string
	ConfigRoot string
}

type Handler struct {
	Command cli.Command
	Options Options
}

var ErrNotImplemented = errors.New("command is not implemented yet")

func NewApp() cli.App {
	return NewAppWithOptions(Options{})
}

func NewAppWithOptions(options Options) cli.App {
	app := cli.New()
	app.Handler = map[cli.Command]cli.Handler{
		cli.CommandAdd:    AddHandler{Options: options},
		cli.CommandUpdate: Handler{Command: cli.CommandUpdate, Options: options},
		cli.CommandRemove: Handler{Command: cli.CommandRemove, Options: options},
		cli.CommandDiff:   DiffHandler{Options: options},
		cli.CommandPush:   Handler{Command: cli.CommandPush, Options: options},
		cli.CommandSetup:  SetupHandler{Options: options},
		cli.CommandStatus: Handler{Command: cli.CommandStatus, Options: options},
	}
	return app
}

func (h Handler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	if err := Preflight(context.Background(), h.Command, inv, h.Options, stderr); err != nil {
		return err
	}
	return fmt.Errorf("%s %w", h.Command, ErrNotImplemented)
}

func Preflight(ctx context.Context, command cli.Command, inv cli.Invocation, options Options, trace io.Writer) error {
	requirements := RequirementsFor(command)
	if !requirements.Git {
		return nil
	}

	git := options.Git
	if git == nil {
		git = gitexec.New(workDir(options.WorkDir), verbose(inv), trace)
	}

	if err := git.RequireVersion(ctx, gitexec.MinimumGitVersion); err != nil {
		return err
	}
	if requirements.Root {
		inside, err := git.IsInsideWorkTree(ctx)
		if err != nil {
			return err
		}
		if !inside {
			return errors.New("Braid must run inside a git working tree")
		}
		prefix, err := git.RelativeWorkingDir(ctx)
		if err != nil {
			return err
		}
		if prefix != "" {
			return errors.New("Braid v1 must run from the git working tree root")
		}
	}

	root := configRoot(options)
	if requirements.Config {
		if err := requireConfigFile(root); err != nil {
			return err
		}
	}
	if _, err := config.Load(root); err != nil {
		return err
	}

	if requirements.Clean {
		status, err := git.StatusPorcelain(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(status) != "" {
			return errors.New("local changes are present")
		}
	}

	return nil
}

func RequirementsFor(command cli.Command) Requirements {
	switch command {
	case cli.CommandVersion:
		return Requirements{}
	case cli.CommandSetup, cli.CommandStatus, cli.CommandDiff:
		return Requirements{Git: true, Root: true, Config: true}
	case cli.CommandAdd:
		return Requirements{Git: true, Root: true, Clean: true, MayWrite: true}
	case cli.CommandUpdate, cli.CommandRemove:
		return Requirements{Git: true, Root: true, Config: true, Clean: true, MayWrite: true}
	case cli.CommandPush:
		return Requirements{Git: true, Root: true, Config: true}
	default:
		return Requirements{}
	}
}

func requireConfigFile(root string) error {
	if _, err := os.Stat(filepath.Join(root, config.FileName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("missing %s", config.FileName)
		}
		return err
	}
	return nil
}

func configRoot(options Options) string {
	if options.ConfigRoot != "" {
		return options.ConfigRoot
	}
	return workDir(options.WorkDir)
}

func workDir(value string) string {
	if value == "" {
		return "."
	}
	return value
}

func verbose(inv cli.Invocation) bool {
	return inv.Add.Verbose || inv.Update.Verbose || inv.Remove.Verbose || inv.Diff.Verbose || inv.Push.Verbose || inv.Setup.Verbose || inv.Status.Verbose
}
